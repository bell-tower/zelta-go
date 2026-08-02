// Package prune selects snapshot prune candidates (read-only analysis).
// Destructive destroy is zprune (not implemented here).
package prune

import (
	"context"
	"fmt"
	"time"

	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/match"
	"github.com/bell-tower/zelta-go/zfs"
)

// Request is a read-only prune analysis.
type Request struct {
	Source endpoint.Endpoint
	// GuardTarget is the match-endpoint; zero → no guard.
	GuardTarget endpoint.Endpoint
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

// Result carries prune analysis. Presentation is CLI-owned (see Format).
type Result struct {
	Warnings []string
	// Keeping is the kept-ranges literal for LOG_INFO ("keeping: a@x,…").
	Keeping  string
	datasets []*dsSnaps
	// render hints captured from Request for Format.
	noRanges bool
	visual   bool
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
	srcEp := req.Source
	if srcEp.Dataset == "" {
		return nil, fmt.Errorf("source: empty endpoint")
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

	tgtEp := req.GuardTarget
	if tgtEp.IsZero() {
		guard = GuardNone // oracle: no second operand → GUARD_NONE
	} else if tgtEp.Dataset == "" {
		return nil, fmt.Errorf("match-endpoint: empty endpoint")
	}

	sel, err := selectorFromRequest(req)
	if err != nil {
		return nil, err
	}

	srcLines, err := exec.List(ctx, srcEp.String(), srcEp.Dataset, SourceListProps, req.Depth)
	if err != nil {
		return nil, fmt.Errorf("list source: %w", err)
	}
	srcRows, err := zfs.ParseListLines(srcLines, SourceListProps)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}

	var tgtRows []zfs.ListRow
	if !tgtEp.IsZero() {
		tgtLines, err := exec.List(ctx, tgtEp.String(), tgtEp.Dataset, TargetListProps, req.Depth)
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
		Keeping:  keptRangesString(src),
		Warnings: append([]string(nil), filt.Warnings...),
		datasets: src,
		noRanges: req.NoRanges,
		visual:   req.Visual,
	}, nil
}

// Format renders prune candidates (oldest-first per dataset). CLI presentation.
func (r *Result) Format() string {
	if r == nil {
		return ""
	}
	return formatOutput(r.datasets, Request{NoRanges: r.noRanges, Visual: r.visual})
}
