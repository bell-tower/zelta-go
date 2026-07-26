// Package backup plans and runs ZFS send/recv replication.
//
// Typical use:
//
//	res, err := backup.Run(ctx, &zfs.Real{SSH: zfs.SSHConfig{…}}, backup.Request{
//	    Source: "tank/src", Target: "backup/tgt",
//	})
//
// Dry-run (-n) prints snap + send|recv; execute runs Snapshot + RunPipe.
// Missing target parent: CREATE (default) even on dry-run, matching oracle.
// SEND/RECV flag fragments come from Request.Flags or opt.Resolve().
package backup
