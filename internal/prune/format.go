package prune

import (
	"fmt"
	"strings"
)

// formatOutput renders prune output (candidates oldest-first per dataset).
func formatOutput(dss []*dsSnaps, req Request) string {
	var b strings.Builder
	for _, d := range dss {
		if d.Filtered || len(d.pruneSnaps) == 0 {
			if req.Visual && !d.Filtered && len(d.Snaps) > 0 {
				fmt.Fprintf(&b, "%s\n%s\n", d.Name, d.visualString())
			}
			continue
		}
		if req.Visual {
			fmt.Fprintf(&b, "%s\n%s\n", d.Name, d.visualString())
			continue
		}
		if req.NoRanges {
			for i := len(d.pruneSnaps) - 1; i >= 0; i-- {
				fmt.Fprintf(&b, "%s%s\n", d.Name, d.pruneSnaps[i].Savepoint)
			}
			continue
		}
		for _, r := range compressRanges(d.pruneSnaps) {
			fmt.Fprintf(&b, "%s%s\n", d.Name, r)
		}
	}
	return b.String()
}

// compressRanges compresses contiguous prune snaps (oracle compress_snapshot_ranges).
// pruneSnaps is newest-first; output is oldest-first.
func compressRanges(snaps []*snap) []string {
	var ranges []string
	var start, end *snap
	var prevIdx int
	// iterate oldest → newest (reverse of newest-first list)
	for p := len(snaps) - 1; p >= 0; p-- {
		sp := snaps[p]
		if start == nil {
			start, end, prevIdx = sp, sp, sp.Idx
			continue
		}
		// oldest→newest: contiguous when idx decreases by 1 (oracle _prev_idx - 1)
		if sp.Idx == prevIdx-1 {
			end, prevIdx = sp, sp.Idx
			continue
		}
		ranges = append(ranges, rangeString(start, end))
		start, end, prevIdx = sp, sp, sp.Idx
	}
	if start != nil {
		ranges = append(ranges, rangeString(start, end))
	}
	return ranges
}

func rangeString(start, end *snap) string {
	if start == end {
		return start.Savepoint
	}
	// oracle: "@first%last" where last loses its '@'
	return start.Savepoint + "%" + strings.TrimPrefix(end.Savepoint, "@")
}

// visualString is ❌/🔹 oldest→newest (oracle output_prune_visual).
func (d *dsSnaps) visualString() string {
	kill := make(map[int]bool, len(d.pruneSnaps))
	for _, sp := range d.pruneSnaps {
		kill[sp.Idx] = true
	}
	var b strings.Builder
	for i := len(d.Snaps) - 1; i >= 0; i-- {
		if kill[d.Snaps[i].Idx] {
			b.WriteString("❌")
		} else {
			b.WriteString("🔹")
		}
	}
	return b.String()
}

// keptRangesString is the LOG_INFO "keeping: a@x,b@y%z,…" line (oracle build_kept_string).
func keptRangesString(dss []*dsSnaps) string {
	var parts []string
	for _, d := range dss {
		if d.Filtered {
			continue
		}
		for _, r := range compressRanges(d.keptSnaps) {
			parts = append(parts, d.Name+r)
		}
	}
	return strings.Join(parts, ",")
}
