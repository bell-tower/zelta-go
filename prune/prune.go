// Package prune selects snapshot prune candidates (read-only analysis).
// Destructive destroy is zprune (not implemented here).
package prune

import (
	"context"
	"fmt"
	"time"

	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/zfs"
)

// Request is a read-only prune analysis.
type Request struct {
	Source      string
	GuardTarget string // --match-endpoint; empty → no guard
	// PruneGuard: zero/GuardLatest (default when target), GuardUnsynced, GuardNone.
	// Use ParsePruneGuard for CLI/env/JSON strings.
	PruneGuard PruneGuard
	PruneNum   int // --prune-num: keep N snaps after match (-1 = unset)
	// PruneTime is age threshold. nil = unset; non-nil (including 0) = set.
	// Use ParsePruneTime for CLI/env/JSON strings.
	PruneTime *time.Duration
	PruneGrid string // --prune-grid spec (still a string this session)
	PruneSize string // --prune-size (still a string this session)
	Depth     int
	Include   []string
	Exclude   []string
	NoRanges  bool
	Visual    bool
	Now       int64 // unix seconds; 0 → time.Now()
}

// ListProps: source has clones; guard target does not (oracle).
var (
	SourceListProps = []string{"name", "guid", "written", "creation", "used", "referenced", "clones"}
	TargetListProps = []string{"name", "guid", "written", "creation", "used", "referenced"}
)

// Result carries analysis + rendered output.
type Result struct {
	Output   string
	Warnings []string
	Keeping  string // "keeping: …" (LOG_INFO; CLI prints on -v)
	datasets []*dsSnaps
}

// Candidates returns the snapshot names to destroy (dataset@snap), oldest first,
// for use by zprune. Returns nil when empty.
func (r *Result) Candidates() []string {
	var out []string
	for _, d := range r.datasets {
		if d.Filtered || len(d.pruneSnaps) == 0 {
			continue
		}
		for i := len(d.pruneSnaps) - 1; i >= 0; i-- {
			out = append(out, d.Name+d.pruneSnaps[i].Savepoint)
		}
	}
	return out
}

// Run lists source (+ guard target), selects prune candidates, renders output.
func Run(ctx context.Context, exec zfs.Executor, req Request) (*Result, error) {
	srcEp, err := endpoint.Parse(req.Source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	if req.Depth < 0 {
		return nil, fmt.Errorf("depth of '%d' invalid; must be positive", req.Depth)
	}
	guard := req.PruneGuard
	if guard == "" {
		guard = GuardLatest
	}
	switch guard {
	case GuardNone, GuardLatest, GuardUnsynced:
	default:
		return nil, fmt.Errorf("invalid prune-guard mode: %s", req.PruneGuard)
	}

	var tgtEp endpoint.Endpoint
	if req.GuardTarget != "" {
		tgtEp, err = endpoint.Parse(req.GuardTarget)
		if err != nil {
			return nil, fmt.Errorf("match-endpoint: %w", err)
		}
	} else {
		guard = GuardNone // oracle: no second operand → GUARD_NONE
	}

	sel, err := selectorFromRequest(req)
	if err != nil {
		return nil, err
	}

	srcLines, err := exec.List(ctx, req.Source, srcEp.Dataset, SourceListProps, req.Depth)
	if err != nil {
		return nil, fmt.Errorf("list source: %w", err)
	}
	srcRows, err := zfs.ParseListLines(srcLines, SourceListProps)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}

	var tgtRows []zfs.ListRow
	if req.GuardTarget != "" {
		tgtLines, err := exec.List(ctx, req.GuardTarget, tgtEp.Dataset, TargetListProps, req.Depth)
		if err != nil {
			return nil, fmt.Errorf("list match-endpoint: %w", err)
		}
		tgtRows, err = zfs.ParseListLines(tgtLines, TargetListProps)
		if err != nil {
			return nil, fmt.Errorf("parse match-endpoint: %w", err)
		}
	}

	now := req.Now
	if now == 0 {
		now = time.Now().Unix()
	}

	filt := match.ParseFilter(req.Include, req.Exclude)
	src := buildDatasets(srcEp.Dataset, srcRows, filt)
	tgt := buildGuardIndex(tgtEp.Dataset, tgtRows)
	analyze(src, tgt, sel, guard, now)

	return &Result{
		Output:   formatOutput(src, req),
		Keeping:  keptRangesString(src),
		Warnings: append([]string(nil), filt.Warnings...),
		datasets: src,
	}, nil
}
