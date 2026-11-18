package zelta

import (
	"context"

	"zelta.space/internal/app"
	"zelta.space/internal/match"
)

// SyncState is whether a dataset's replica can be brought up to date.
type SyncState string

// Sync states.
const (
	// UpToDate means the newest source snapshot is on the target.
	UpToDate SyncState = "up-to-date"
	// Syncable means a backup can update the target.
	Syncable SyncState = "syncable"
	// Blocked means the target has diverged, has local writes, or has no
	// source.
	Blocked SyncState = "blocked"
)

// Comparison is the result of Match.
type Comparison struct {
	// Datasets are sorted by suffix.
	Datasets []DatasetComparison
	// UpToDate, Syncable, and Blocked count the datasets in each state.
	UpToDate, Syncable, Blocked int
	// Summary reads like "5 up-to-date, 2 syncable".
	Summary string
}

// DatasetComparison compares one dataset with its replica.
type DatasetComparison struct {
	// Suffix is relative to the endpoints ("" for the top dataset).
	Suffix string
	State  SyncState
	// Info explains the state, such as "syncable (incremental)" or
	// "blocked sync: target diverged".
	Info string
	// Match is the newest common snapshot ("@name"), if any, and Matches
	// the number of common snapshots.
	Match   string
	Matches int
	// TransferBytes estimates the size of the source snapshots newer than
	// the match.
	TransferBytes int64
	// Source and Target describe each side; nil when the dataset does not
	// exist there.
	Source, Target *DatasetSnapshots
}

// DatasetSnapshots describes one side of a comparison.
type DatasetSnapshots struct {
	// Name is the full dataset name.
	Name string
	// Snapshots counts snapshots and bookmarks.
	Snapshots int
	// First and Last are the oldest and newest snapshot or bookmark
	// ("@name" or "#name").
	First, Last string
	// Next is the oldest snapshot newer than the match, and Newer the
	// number of snapshots newer than the match. On the source these are
	// what a backup sends; on the target they block one.
	Next  string
	Newer int
	// Written is the dataset's written property (zero with NoSizes).
	Written int64
}

// Match compares a dataset tree with its replica by snapshot GUID. With an
// empty target it lists the source tree alone.
func (c *Client) Match(ctx context.Context, source, target string, opts *MatchOptions) (*Comparison, error) {
	if opts == nil {
		opts = &MatchOptions{}
	}
	o, err := c.start(ctx, "match", opts.settings())
	if err != nil {
		return nil, err
	}
	res, err := compare(ctx, o.rt, source, target)
	return res, o.finish(err)
}

func compare(ctx context.Context, rt *app.Runtime, source, target string) (*Comparison, error) {
	if source == "" {
		return nil, app.Usage("missing source endpoint")
	}
	args := []string{source}
	if target != "" {
		args = append(args, target)
	}
	src, tgt, err := match.Endpoints(rt, args)
	if err != nil {
		return nil, err
	}
	if err := match.CheckDepth(rt); err != nil {
		return nil, err
	}
	req := match.RequestFromOptions(rt, src, tgt)
	req.DryRun = false // listing changes nothing
	res, err := match.Compare(ctx, rt, req)
	if err != nil {
		return nil, err
	}
	if len(res.Pairs) == 0 {
		return nil, app.Fail("datasets inaccessible or do not exist")
	}
	out := &Comparison{UpToDate: res.UpToDate, Syncable: res.Syncable, Blocked: res.Blocked, Summary: res.Summary}
	for _, p := range res.Pairs {
		d := DatasetComparison{
			Suffix: p.Suffix, State: syncState(p.State), Info: p.Info,
			Match: p.Match, Matches: p.NumMatches, TransferBytes: p.XferSize,
		}
		if sd := res.Source.Dataset(p.Suffix); sd != nil {
			d.Source = &DatasetSnapshots{Name: sd.Name, Snapshots: p.SrcSnaps, First: p.SrcFirst, Last: p.SrcLast,
				Next: p.SrcNext, Newer: p.XferNum, Written: sd.Written}
		}
		if td := res.Target.Dataset(p.Suffix); td != nil {
			d.Target = &DatasetSnapshots{Name: td.Name, Snapshots: p.TgtSnaps, First: p.TgtFirst, Last: p.TgtLast,
				Next: p.TgtNext, Newer: p.NumBlocked, Written: td.Written}
		}
		out.Datasets = append(out.Datasets, d)
	}
	return out, nil
}

func syncState(s match.SyncState) SyncState {
	switch s {
	case match.SyncUpToDate:
		return UpToDate
	case match.SyncSyncable:
		return Syncable
	}
	return Blocked
}
