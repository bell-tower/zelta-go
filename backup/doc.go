// Package backup plans and runs ZFS send/recv replication.
//
// Lifecycle: Prepare (lazy match) → Plan.Commands / RunStep → Run.
// Results are typed (Plan, []zfs.Command, ErrCode, PipeStats); the CLI owns
// human/JSON presentation.
//
//	src := endpoint.Endpoint{Dataset: "tank/src"}
//	tgt := endpoint.Endpoint{Host: "backup", Dataset: "tank/tgt", Remote: true}
//	plan, err := backup.Prepare(ctx, exec, backup.Request{
//	    Source: src, Target: tgt, SnapMode: backup.SnapIfNeeded,
//	})
//	cmds := plan.Commands(src, tgt, backup.DirectionPull.PipeArg())
//	res, err := backup.Run(ctx, exec, req)
//
// Action paths never read process env. String import: ParseSnapMode,
// ParseSnapTime, ParseSnapSize, ParseSyncDirection, endpoint.Parse.
package backup
