// Package match compares source and target ZFS dataset trees.
//
//	res, err := match.Compare(ctx, exec, match.Request{
//	    Source: endpoint.MustParse("tank/src"),
//	    Target: endpoint.MustParse("backup/tgt"),
//	})
//
// Source and Target are endpoint.Endpoint values (build from memory or
// endpoint.Parse). No process env is read.
package match
