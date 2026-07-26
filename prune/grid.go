package prune

import (
	"fmt"
	"strconv"
	"strings"
)

// gridTerm is one --prune-grid term: count x interval (count -1 = keep-all tail).
type gridTerm struct {
	Count    int64
	Interval int64
}

// parseGrid parses "30x1 day, 52x1 week, 1 year" (oracle parse_prune_grid).
func parseGrid(spec string) ([]gridTerm, error) {
	var out []gridTerm
	spec = strings.ReplaceAll(spec, " x ", "x")
	parts := strings.FieldsFunc(spec, func(r rune) bool { return r == ',' || r == '|' })
	for _, term := range parts {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		var count int64 = -1
		intervalStr := term
		if x := strings.Index(term, "x"); x >= 0 {
			c, err := strconv.ParseInt(term[:x], 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid --prune-grid term: %s", term)
			}
			count = c
			intervalStr = term[x+1:]
		}
		interval, err := ParseDuration(intervalStr)
		if err != nil || interval == 0 {
			return nil, fmt.Errorf("invalid --prune-grid term: %s", term)
		}
		out = append(out, gridTerm{Count: count, Interval: interval})
	}
	return out, nil
}

// gridKeeps is oracle grid_keeps_snapshot with per-dataset bucket state.
// Anchor = match snap creation (or newest snap when no match).
type gridState struct {
	terms   []gridTerm
	buckets map[string]bool
}

func newGrid(terms []gridTerm) *gridState {
	return &gridState{terms: terms, buckets: make(map[string]bool)}
}

// keeps reports whether creation should be kept under the GFS grid.
func (g *gridState) keeps(creation, anchor int64) bool {
	if len(g.terms) == 0 {
		return false
	}
	age := anchor - creation
	if age < 0 {
		return false
	}
	var start int64
	for i, t := range g.terms {
		if t.Count == -1 {
			if age < start {
				return false
			}
			bucket := fmt.Sprintf("%d/%d", i, (age-start)/t.Interval)
			if !g.buckets[bucket] {
				g.buckets[bucket] = true
				return true
			}
			return false
		}
		end := start + t.Count*t.Interval
		if age >= start && age < end {
			bucket := fmt.Sprintf("%d/%d", i, (age-start)/t.Interval)
			if !g.buckets[bucket] {
				g.buckets[bucket] = true
				return true
			}
			return false
		}
		start = end
	}
	return false
}
