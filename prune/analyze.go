package prune

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/bell-tower/zelta-go/match"
	"github.com/bell-tower/zelta-go/zfs"
)

// snap is one source snapshot row (source snaps only; bookmarks skipped).
type snap struct {
	Savepoint  string
	GUID       string
	Creation   int64
	Written    int64
	Referenced int64
	Clones     string
	Filtered   bool // prune_filtered via -X/--include
	Idx        int  // oracle snap_idx (all source snaps, incl. filtered)
}

// dsSnaps is one source dataset's snapshot list (newest-first) + match index.
type dsSnaps struct {
	Name       string
	Suffix     string
	Filtered   bool
	Snaps      []*snap
	MatchIdx   int // 0 = no match; else snaps[MatchIdx-1] is the match
	pruneSnaps []*snap
	keptSnaps  []*snap
	idxCounter int
}

// buildDatasets groups source rows by ds_suffix (newest-first, oracle process_row).
func buildDatasets(root string, rows []zfs.ListRow, filt *match.Filter) []*dsSnaps {
	bySuf := make(map[string]*dsSnaps)
	var order []*dsSnaps
	for _, row := range rows {
		base, savepoint, isSnap := splitRowName(row.Name)
		suffix := suffixOf(root, base)
		d := bySuf[suffix]
		if d == nil {
			d = &dsSnaps{Name: base, Suffix: suffix}
			bySuf[suffix] = d
			order = append(order, d)
		}
		if !isSnap {
			if filt != nil && !filt.KeepDatasetForPrune(row.Name, suffix) {
				d.Filtered = true
			}
			continue
		}
		if savepoint[0] != '@' { // bookmarks: oracle skips in prune analysis
			continue
		}
		d.idxCounter++
		sp := &snap{
			Savepoint:  savepoint,
			GUID:       row.Props["guid"],
			Creation:   atoi(row.Props["creation"]),
			Written:    atoi(row.Props["written"]),
			Referenced: atoi(row.Props["referenced"]),
			Clones:     row.Props["clones"],
			Idx:        d.idxCounter,
		}
		if filt != nil && filt.Active && !filt.KeepSourceSnap(savepoint, base, suffix) {
			sp.Filtered = true
		}
		d.Snaps = append(d.Snaps, sp)
	}
	return order
}

// guardDS maps guard target rows by ds_suffix → guid/snap-name presence.
type guardDS struct {
	GUIDs map[string]bool
	Names map[string]bool
}

func buildGuardIndex(root string, rows []zfs.ListRow) map[string]*guardDS {
	out := make(map[string]*guardDS)
	for _, row := range rows {
		base, savepoint, isSnap := splitRowName(row.Name)
		if !isSnap || savepoint[0] != '@' {
			continue
		}
		suffix := suffixOf(root, base)
		g := out[suffix]
		if g == nil {
			g = &guardDS{GUIDs: make(map[string]bool), Names: make(map[string]bool)}
			out[suffix] = g
		}
		g.GUIDs[row.Props["guid"]] = true
		g.Names[savepoint] = true
	}
	return out
}

func suffixOf(root, base string) string {
	if base == root {
		return ""
	}
	return strings.TrimPrefix(base, root)
}

func splitRowName(name string) (base, savepoint string, isSnap bool) {
	if i := strings.IndexAny(name, "@#"); i >= 0 {
		return name[:i], name[i:], true
	}
	return name, "", false
}

func atoi(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// selector is parsed prune criteria (oracle prune_init + Opt values).
type selector struct {
	num      int
	timeSecs int64
	hasTime  bool
	grid     []gridTerm
	sizeByte int64
}

func selectorFromRequest(req Request) (*selector, error) {
	s := &selector{num: req.PruneNum}
	// Oracle default when all retention opts unset: num=30 time=30days.
	if req.PruneNum < 0 && req.PruneTime == nil && req.PruneGrid == "" && req.PruneSize == "" {
		s.num = 30
		d, _ := ParseDuration("30days")
		s.timeSecs, s.hasTime = d, true
		return s, nil
	}
	if req.PruneTime != nil {
		s.timeSecs = int64(*req.PruneTime / time.Second)
		s.hasTime = true
	}
	if req.PruneGrid != "" {
		g, err := parseGrid(req.PruneGrid)
		if err != nil {
			return nil, err
		}
		s.grid = g
	}
	if req.PruneSize != "" {
		n, err := ParseSize(req.PruneSize)
		if err != nil {
			return nil, fmt.Errorf("invalid --prune-size: %s", req.PruneSize)
		}
		s.sizeByte = n
	}
	if req.PruneNum < 0 {
		s.num = 0 // unset → no num rule (num>0 required)
	}
	return s, nil
}

// analyze selects prune candidates per dataset (oracle analyze_prune_candidates).
func analyze(dss []*dsSnaps, tgt map[string]*guardDS, sel *selector, guard PruneGuard, now int64) {
	for _, d := range dss {
		if d.Filtered {
			continue
		}
		matchIdx := 0
		gd := tgt[d.Suffix]
		if guard != GuardNone && gd != nil {
			for i, sp := range d.Snaps {
				if !sp.Filtered && gd.GUIDs[sp.GUID] {
					matchIdx = i + 1
					break
				}
			}
		}
		d.MatchIdx = matchIdx

		var anchor int64
		if matchIdx > 0 && matchIdx <= len(d.Snaps) {
			anchor = d.Snaps[matchIdx-1].Creation
		} else if len(d.Snaps) > 0 {
			anchor = d.Snaps[0].Creation
		}
		g := newGrid(sel.grid)
		minAge := now - sel.timeSecs
		seenAfterMatch := 0
		var eligible []*snap

		for i := matchIdx; i < len(d.Snaps); i++ {
			sp := d.Snaps[i]
			if sp.Filtered {
				d.keptSnaps = append(d.keptSnaps, sp)
				continue
			}
			seenAfterMatch++
			if sp.Clones != "" && sp.Clones != "-" {
				d.keptSnaps = append(d.keptSnaps, sp)
				continue
			}
			// Unsynced protection requires both the snapshot GUID and its
			// savepoint name to exist on the guard endpoint.
			if guard == GuardUnsynced {
				if gd == nil || !gd.GUIDs[sp.GUID] || !gd.Names[sp.Savepoint] {
					d.keptSnaps = append(d.keptSnaps, sp)
					continue
				}
			}
			gridKeep := len(sel.grid) > 0 &&
				(i == 0 || i == len(d.Snaps)-1 || g.keeps(sp.Creation, anchor))
			if gridKeep ||
				(sel.num > 0 && seenAfterMatch <= sel.num) ||
				(sel.hasTime && sp.Creation >= minAge) {
				d.keptSnaps = append(d.keptSnaps, sp)
				continue
			}
			if sel.sizeByte > 0 {
				eligible = append(eligible, sp)
			} else {
				d.pruneSnaps = append(d.pruneSnaps, sp)
			}
		}
		if sel.sizeByte > 0 {
			var total int64
			for p := len(eligible) - 1; p >= 0; p-- {
				d.pruneSnaps = append([]*snap{eligible[p]}, d.pruneSnaps...)
				total += eligible[p].Written
				if total-eligible[p].Referenced >= sel.sizeByte {
					break
				}
			}
		}
	}
}
