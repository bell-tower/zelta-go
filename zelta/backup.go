package zelta

import (
	"context"
	"errors"
	"time"

	"zelta.space/internal/backup"
)

// BackupResult describes a finished Backup or Rotate.
type BackupResult struct {
	// Snapshot is the source snapshot ("@name") the run created, or would
	// create in a dry run; empty if none.
	Snapshot string
	// Datasets reports each dataset of the tree in suffix order.
	Datasets []DatasetResult
	// Streams describes each stream received, as "pool/ds@from%to" for
	// incremental and "pool/ds@to" for full streams.
	Streams []string
	// StreamsSent and StreamsReceived count send and receive streams.
	StreamsSent, StreamsReceived int
	// Bytes is the total stream size reported by zfs send.
	Bytes int64
	// ReceiveTime is the summed receive time of every stream.
	ReceiveTime time.Duration
	// FailedProperties lists properties zfs recv was not allowed to set.
	FailedProperties []string
}

// DatasetResult is the outcome for one dataset.
type DatasetResult struct {
	// Suffix is relative to the endpoints ("" for the top dataset).
	Suffix string
	// Source and Target are the full dataset names.
	Source, Target string
	// Synced reports that at least one stream was received.
	Synced bool
	// UpToDate reports that the target has the newest source snapshot.
	UpToDate bool
	// Notice explains why the dataset is not up to date, such as "target
	// has diverged".
	Notice string
}

// UpToDate reports whether every dataset is up to date.
func (r *BackupResult) UpToDate() bool {
	for _, d := range r.Datasets {
		if !d.UpToDate {
			return false
		}
	}
	return len(r.Datasets) > 0
}

// Backup replicates a dataset tree from source to target, snapshotting the
// source first when needed. Both endpoints may be remote.
//
// When some datasets fail to sync, Backup returns the result together with
// an *Error whose Code is 2 (or 1 when the backup is merely incomplete).
func (c *Client) Backup(ctx context.Context, source, target string, opts *BackupOptions) (*BackupResult, error) {
	if opts == nil {
		opts = &BackupOptions{}
	}
	verb := "backup"
	if opts.Replicate {
		verb = "replicate"
	}
	return c.sync(ctx, verb, opts, source, target)
}

// Rotate renames a diverged replica out of the way and rebuilds it from the
// newest common snapshot (or the source's origin), destroying nothing.
func (c *Client) Rotate(ctx context.Context, source, target string, opts *BackupOptions) (*BackupResult, error) {
	if opts == nil {
		opts = &BackupOptions{}
	}
	if opts.Replicate {
		return nil, errors.New("zelta: Rotate does not support Replicate")
	}
	return c.sync(ctx, "rotate", opts, source, target)
}

func (c *Client) sync(ctx context.Context, verb string, opts *BackupOptions, args ...string) (*BackupResult, error) {
	if err := checkSnapName(opts.Snapshot.Name); err != nil {
		return nil, err
	}
	o, err := c.start(ctx, verb, opts.settings())
	if err != nil {
		return nil, err
	}
	res, err := backup.Execute(ctx, o.rt, operands(args...))
	return backupResult(res), o.finish(err)
}

func backupResult(r *backup.Result) *BackupResult {
	if r == nil {
		return nil
	}
	out := &BackupResult{
		Snapshot: r.Snapshot, Streams: r.Streams, StreamsSent: r.StreamsSent, StreamsReceived: r.StreamsReceived,
		Bytes: r.Bytes, ReceiveTime: time.Duration(r.Seconds * float64(time.Second)), FailedProperties: r.FailedProperties,
	}
	for _, d := range r.Datasets {
		out.Datasets = append(out.Datasets, DatasetResult(d))
	}
	return out
}

// Snapshot recursively snapshots a dataset tree and returns the snapshot
// created ("pool/ds@name"). It returns "" when MinInterval or MinWritten
// skipped the snapshot, or in a dry run.
func (c *Client) Snapshot(ctx context.Context, endpoint string, opts *SnapshotOptions) (string, error) {
	if opts == nil {
		opts = &SnapshotOptions{}
	}
	if err := checkSnapName(opts.Name); err != nil {
		return "", err
	}
	o, err := c.start(ctx, "snapshot", opts.settings())
	if err != nil {
		return "", err
	}
	name, err := backup.TakeSnapshot(ctx, o.rt, operands(endpoint))
	return name, o.finish(err)
}

// Clone recursively clones a dataset tree from its newest snapshot, or
// from the snapshot named in source, to target on the same host.
func (c *Client) Clone(ctx context.Context, source, target string, opts *CloneOptions) error {
	if opts == nil {
		opts = &CloneOptions{}
	}
	args := []string{source, target}
	switch {
	case opts.OriginBackup != "" && opts.TargetBackup != "":
		args = append(args, opts.OriginBackup, opts.TargetBackup)
	case opts.OriginBackup != "" || opts.TargetBackup != "":
		return errors.New("zelta: Clone needs both OriginBackup and TargetBackup")
	}
	o, err := c.start(ctx, "clone", opts.settings())
	if err != nil {
		return err
	}
	return o.finish(backup.Run(ctx, o.rt, operands(args...)))
}

// Revert rewinds a dataset tree to a snapshot by renaming it out of the way
// and cloning the snapshot in its place, preserving the current state.
func (c *Client) Revert(ctx context.Context, endpoint string, opts *RevertOptions) error {
	if opts == nil {
		opts = &RevertOptions{}
	}
	o, err := c.start(ctx, "revert", opts.settings())
	if err != nil {
		return err
	}
	return o.finish(backup.Run(ctx, o.rt, operands(endpoint)))
}

// Rebase replaces target with a dataset based on an upgraded upstream on
// the same host while preserving incremental backup continuity.
func (c *Client) Rebase(ctx context.Context, upstream, target string, opts *BasicOptions) error {
	if opts == nil {
		opts = &BasicOptions{}
	}
	o, err := c.start(ctx, "rebase", opts.settings())
	if err != nil {
		return err
	}
	return o.finish(backup.Rebase(ctx, o.rt, operands(upstream, target)))
}
