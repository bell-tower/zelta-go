// Package prune selects snapshot prune candidates (read-only analysis).
//
//	res, err := prune.Run(ctx, exec, prune.Request{
//	    Source:     endpoint.MustParse("tank/data"),
//	    PruneGuard: prune.GuardLatest,
//	    PruneNum:   30,
//	})
//	cands := res.Candidates() // dataset@snap names for destroy
//
// Destructive destroy is zprune / a future gated destroy API — not this package.
// String import: ParsePruneGuard, ParsePruneTime, endpoint.Parse.
package prune
