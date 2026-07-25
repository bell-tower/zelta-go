// Package backup plans and runs replication from match results (phase 2).
// Dry-run (-n) prints snap + send|recv; execute runs Snapshot + RunPipe.
// Missing target parent: CREATE (default) even on dry-run, matching oracle.
// SEND/RECV flag fragments come from opt.Resolve() (defaults + ZELTA_* env).
package backup
