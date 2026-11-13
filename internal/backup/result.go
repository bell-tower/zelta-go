package backup

import (
	"context"
	"errors"
	"slices"

	"github.com/bell-tower/zelta-go/internal/app"
)

// Result describes a finished replication run.
type Result struct {
	// Snapshot is the source snapshot ("@name") this run created, or would
	// create in a dry run; empty if none.
	Snapshot string
	// StreamsSent and StreamsReceived count send/receive streams.
	StreamsSent, StreamsReceived int
	// Bytes is the total size reported by zfs send.
	Bytes int64
	// Seconds is the summed receive time of every stream.
	Seconds float64
	// Streams describes each stream as "pool/ds@from%to" or "pool/ds@to".
	Streams []string
	// FailedProperties lists properties zfs recv was not allowed to set.
	FailedProperties []string
	// ErrorCode is the exit status: 0, 1 (incomplete), or 2 (sync errors).
	ErrorCode int
	// Datasets reports each dataset of the tree in suffix order.
	Datasets []DatasetResult
}

// DatasetResult is the outcome for one dataset.
type DatasetResult struct {
	// Suffix is relative to the endpoints ("" for the top dataset).
	Suffix string
	// Source and Target are the full dataset names.
	Source, Target string
	// Synced reports that at least one stream was received.
	Synced bool
	// UpToDate reports that the target matches the source after the run.
	UpToDate bool
	// Notice explains why the dataset is not up-to-date, such as "target
	// has diverged".
	Notice string
}

// Execute runs like Run and also returns the run's result. The result is
// nil only when the run failed before planning.
func Execute(ctx context.Context, rt *app.Runtime, args []string) (*Result, error) {
	r := newRun(ctx, rt, args)
	defer func() { r.report() }()
	err := r.main()
	if errors.Is(err, errStale) {
		rt.Log.Infof("%s; checking %s", errStale, r.tgt.ID)
		rt.Cache.Delete(r.memoryKey())
		created := r.created
		r = newRun(ctx, rt, args)
		r.noMemory, r.created, r.snapshotTaken = true, created, created != ""
		err = r.main()
	}
	if err == nil && r.sum.errorCode != 0 {
		err = &app.ExitError{Code: r.sum.errorCode}
	}
	if len(r.list) == 0 && r.created == "" {
		return nil, err
	}
	return r.result(), err
}

func (r *run) result() *Result {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := &r.sum
	res := &Result{
		Snapshot: r.created, StreamsSent: s.streamsSent, StreamsReceived: s.streamsReceived,
		Bytes: s.replicationSize, Seconds: s.replicationTime, ErrorCode: s.errorCode,
		Streams: slices.Clone(s.sentStreams), FailedProperties: slices.Clone(s.failedProps),
	}
	for _, suffix := range r.list {
		a := r.action(suffix)
		notice := a.statusNotice
		if notice == "" {
			notice = a.statusInfo
		}
		upToDate := a.upToDate
		if r.synced[suffix] {
			// The final eligibility pass predates the receive (a new target
			// still looks missing), so judge by the received snapshot.
			m := r.pair(suffix).match
			upToDate = m != "" && (m == r.dataset(src, suffix).latest || m == r.tree.finalSnapshot)
			if upToDate {
				notice = ""
			}
		}
		res.Datasets = append(res.Datasets, DatasetResult{
			Suffix: suffix, Source: r.src.DS + suffix, Target: r.tgt.DS + suffix,
			Synced: r.synced[suffix], UpToDate: upToDate, Notice: notice,
		})
	}
	return res
}
