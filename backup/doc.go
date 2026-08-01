// Package backup plans and runs ZFS send/recv replication.
//
// Build a Request from memory (integrator) or via public Parse helpers (CLI/JSON):
//
//	src := endpoint.Endpoint{Dataset: "tank/src"}
//	tgt := endpoint.Endpoint{Host: "backup", Dataset: "tank/tgt", Remote: true}
//	res, err := backup.Run(ctx, &zfs.Real{SSH: zfs.SSHConfig{…}}, backup.Request{
//	    Source: src, Target: tgt,
//	    SnapMode: backup.SnapIfNeeded,
//	    Flags:    &flags, // flags := backup.DefaultSendRecv(); customize
//	})
//
// Dry-run prints snap + send|recv; execute runs Snapshot + RunPipe.
// Action paths never read process env. String import: ParseSnapMode,
// ParseSnapTime, ParseSnapSize, ParseSyncDirection, endpoint.Parse.
package backup
