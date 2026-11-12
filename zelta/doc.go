// Package zelta is the Go API for Zelta, the ZFS replication tool. It runs
// the same engine as the zelta command, in-process, with typed options and
// results instead of command-line flags and text output.
//
// Endpoints are strings in Zelta's scp-like syntax, as on the command line:
// "tank/home" is local, "backup@host:pool/backups/home" is reached over ssh,
// and an "@snapshot" suffix names a snapshot.
//
//	c, err := zelta.New(zelta.WithLogger(slog.Default()))
//	if err != nil {
//		return err
//	}
//	res, err := c.Backup(ctx, "tank/home", "backup.example.com:pool/home", &zelta.BackupOptions{
//		Snapshot: zelta.SnapshotPolicy{Mode: zelta.SnapshotAlways},
//		Parallel: 4,
//	})
//
// # Configuration
//
// A Client starts from Zelta's built-in defaults. WithEnvironment adds the
// ZELTA_* environment variables and zelta.env, as the zelta command reads
// them; WithSettings and each operation's Settings set any option by name
// (see zelta-options(7)). Typed option fields take precedence over Settings
// given for the same operation, which take precedence over the client's
// settings.
//
// # Errors and logging
//
// Failed operations return an *Error carrying the exit status the zelta
// command would have used and the messages it logged. Messages also go to
// the slog.Logger given with WithLogger; nothing is written to the
// process's standard streams.
//
// Every operation honors its context: canceling it stops running zfs and
// ssh commands, and the returned error then matches context.Canceled.
package zelta
