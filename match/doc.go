// Package match compares source and target ZFS dataset trees (analysis only).
//
//	res, err := match.Compare(ctx, exec, match.Request{
//	    Source: endpoint.MustParse("tank/src"),
//	    Target: endpoint.MustParse("backup/tgt"),
//	})
//	// res.Pairs, timings, warnings — CLI renders tables via internal/report
//
// Source and Target are endpoint.Endpoint values (build from memory or
// endpoint.Parse). No process env is read. Presentation (tables, -H/-p) is
// CLI-owned.
package match
