package prune

import (
	"context"
	"slices"

	"zelta.space/internal/app"
	"zelta.space/internal/endpoint"
	"zelta.space/internal/match"
)

// chain returns every snapshot oldest first, then the live dataset.
func (d *Dataset) chain() []*match.Row {
	var chain []*match.Row
	for _, r := range slices.Backward(d.src.Snaps) {
		if r.Kind == match.KindSnapshot {
			chain = append(chain, r)
		}
	}
	return append(chain, d.src.Row)
}

// runs returns the named snapshots ("@name") as runs [a, b] of consecutive
// snapshots in chain. Names chain doesn't list are ignored.
func runs(chain []*match.Row, names []string) [][2]int {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var out [][2]int
	for i := 0; i < len(chain)-1; i++ {
		if !want[chain[i].Savepoint] {
			continue
		}
		j := i
		for j+1 < len(chain)-1 && want[chain[j+1].Savepoint] {
			j++
		}
		out = append(out, [2]int{i, j})
		i = j
	}
	return out
}

// bounds bounds what destroying chain[a..b] frees from the listing alone.
// They are equal when the run is one snapshot, which frees its used, or
// starts at the oldest snapshot of a dataset that isn't a clone. See
// selectSize.
func bounds(chain []*match.Row, a, b int) (lo, hi int64) {
	var used, written int64
	for _, s := range chain[a : b+1] {
		used += s.Used
		written += s.Written
	}
	switch {
	case a == b:
		return used, used
	case a == 0 && chain[0].Written == chain[0].Referenced:
		next := chain[b+1]
		freed := written + next.Written - next.Referenced
		return freed, freed
	}
	return used, written
}

// ReclaimBounds bounds what destroying the named snapshots ("@name") frees,
// from the listing alone, with no zfs commands. Lo equals hi when that is
// exact.
func (d *Dataset) ReclaimBounds(names []string) (lo, hi int64) {
	if d.src == nil {
		return 0, 0
	}
	chain := d.chain()
	for _, r := range runs(chain, names) {
		l, h := bounds(chain, r[0], r[1])
		lo += l
		hi += h
	}
	return lo, hi
}

// Reclaim returns what destroying the named snapshots ("@name") frees, as
// "zfs destroy -nvp" reports it. Each run whose bounds differ takes one
// "zfs get written@" on ep.
func (d *Dataset) Reclaim(ctx context.Context, rt *app.Runtime, ep endpoint.Endpoint, names []string) (int64, error) {
	if d.src == nil {
		return 0, nil
	}
	chain := d.chain()
	var total int64
	for _, r := range runs(chain, names) {
		if lo, hi := bounds(chain, r[0], r[1]); lo == hi {
			total += lo
			continue
		}
		sr := &sizeRun{ctx: ctx, rt: rt, ep: ep, ds: d.src.Name, chain: chain, a: r[0], e: r[1]}
		n, err := sr.freed(r[1])
		if err != nil {
			return 0, err
		}
		total += n
	}
	return total, nil
}
