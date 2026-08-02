package match

import (
	"context"
	"fmt"
	"strings"
	"time"

	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/report"
	"git.belltower.it/djbell/zelta-go/zfs"
)

// Request is a match comparison.
type Request struct {
	Source                  endpoint.Endpoint
	Target                  endpoint.Endpoint
	Props                   []string // empty → resolveListProps (+ optional feature probe)
	Cols                    []string // empty → expand default proplist
	Depth                   int      // 0 = unlimited; zfs -d + pair filter
	Include                 []string // --include patterns (comma lists / repeated)
	Exclude                 []string // -X / --exclude patterns
	Scripting               bool     // -H
	Parsable                bool     // -p
	NoWritten               bool     // --no-written: list name,guid only
	CheckTime               bool     // --time: append SOURCE/TARGET_LIST_TIME
	PreserveSourceSnapshots bool     // retain source history for filtered backup planning
	// SrcContext / TgtContext, when set, merge dataset props after list (backup path).
	SrcContext *zfs.DatasetContext
	TgtContext *zfs.DatasetContext
}

// Result is a completed match comparison.
type Result struct {
	Source      endpoint.Endpoint
	Target      endpoint.Endpoint
	SrcRows     []zfs.ListRow
	TgtRows     []zfs.ListRow
	Pairs       []*Pair
	Summary     string
	Output      string
	Warnings    []string // filter parse warnings (oracle stderr)
	SrcListTime float64  // seconds; 0 if not measured / negligible
	TgtListTime float64
}

// DefaultListProps match production dry-run list columns (LIST_WRITTEN default on).
var DefaultListProps = []string{"name", "guid", "written", "creation", "used"}

// MinimalListProps when --no-written (or -p without written/size cols).
var MinimalListProps = []string{"name", "guid"}

// RotateListProps includes origin for clone-lineage classification.
var RotateListProps = []string{"name", "guid", "origin", "written", "snapshots_changed", "creation", "used", "type"}

// Compare loads lists, pairs by ds_suffix/GUID, and renders columns.
func Compare(ctx context.Context, exec zfs.Executor, req Request) (*Result, error) {
	srcEp := req.Source
	tgtEp := req.Target
	if srcEp.Dataset == "" {
		return nil, fmt.Errorf("source: empty endpoint")
	}
	if tgtEp.Dataset == "" {
		return nil, fmt.Errorf("target: empty endpoint")
	}
	if req.Depth < 0 {
		return nil, fmt.Errorf("depth of '%d' invalid; must be positive", req.Depth)
	}
	cols := req.Cols
	var err error
	if len(cols) == 0 {
		cols, err = report.ExpandProplist("")
		if err != nil {
			return nil, err
		}
	}
	props, err := resolveListProps(ctx, exec, req, cols)
	if err != nil {
		return nil, err
	}

	t0 := time.Now()
	srcLines, err := exec.List(ctx, srcEp.String(), srcEp.Dataset, props, req.Depth)
	if err != nil {
		return nil, fmt.Errorf("list source: %w", err)
	}
	srcDur := time.Since(t0).Seconds()

	t1 := time.Now()
	tgtLines, err := exec.List(ctx, tgtEp.String(), tgtEp.Dataset, props, req.Depth)
	if err != nil {
		return nil, fmt.Errorf("list target: %w", err)
	}
	tgtDur := time.Since(t1).Seconds()

	srcRows, err := zfs.ParseListLines(srcLines, props)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}
	tgtRows, err := zfs.ParseListLines(tgtLines, props)
	if err != nil {
		return nil, fmt.Errorf("parse target: %w", err)
	}

	filt := ParseFilter(req.Include, req.Exclude)
	srcTree, err := buildTree(srcEp.Dataset, srcRows, filt, true, req.PreserveSourceSnapshots)
	if err != nil {
		return nil, fmt.Errorf("source tree: %w", err)
	}
	tgtTree, err := buildTree(tgtEp.Dataset, tgtRows, filt, false, false)
	if err != nil {
		return nil, fmt.Errorf("target tree: %w", err)
	}

	pairs := pairTrees(srcTree, tgtTree)
	if req.Depth > 0 {
		pairs = filterDepth(pairs, req.Depth)
	}
	if req.SrcContext != nil || req.TgtContext != nil {
		ApplyDatasetContext(pairs, req.SrcContext, req.TgtContext)
	}
	sum := summaryOf(pairs)
	out := Format(pairs, FormatOpts{
		Cols:      cols,
		SrcLeaf:   leafOf(srcEp.Dataset),
		Scripting: req.Scripting,
		Parsable:  req.Parsable,
	})
	if req.CheckTime {
		out += formatListTimes(srcDur, tgtDur)
	}

	return &Result{
		Source:      srcEp,
		Target:      tgtEp,
		SrcRows:     srcRows,
		TgtRows:     tgtRows,
		Pairs:       pairs,
		Summary:     sum,
		Output:      out,
		Warnings:    append([]string(nil), filt.Warnings...),
		SrcListTime: srcDur,
		TgtListTime: tgtDur,
	}, nil
}

// resolveListProps picks zfs list -o columns (oracle add_written).
// When cols need ivset and Props is empty, probes top-level source features once.
func resolveListProps(ctx context.Context, exec zfs.Executor, req Request, cols []string) ([]string, error) {
	if len(req.Props) > 0 {
		return req.Props, nil
	}
	wantWritten := !req.NoWritten
	// LIST_WRITTEN + -p + proplist without written/size → skip slow props.
	if wantWritten && req.Parsable && len(cols) > 0 && !colsNeedWritten(cols) {
		wantWritten = false
	}
	if !colsNeedIVSet(cols) {
		if wantWritten {
			return DefaultListProps, nil
		}
		return MinimalListProps, nil
	}
	// Optional feature requested: probe top-level only.
	feat, err := zfs.ProbeFeatures(ctx, exec, req.Source.String(), req.Source.Dataset)
	if err != nil {
		return nil, fmt.Errorf("probe source features: %w", err)
	}
	return SnapListProps(feat, SnapListOpts{Written: wantWritten, IVSet: true}), nil
}

func colsNeedWritten(cols []string) bool {
	for _, c := range cols {
		switch c {
		case "xfer_size", "src_written", "tgt_written", "written", "size":
			return true
		}
	}
	return false
}

func formatListTimes(src, tgt float64) string {
	var b strings.Builder
	// Oracle: if (list_time) print — skip exact zero.
	if src > 0 {
		fmt.Fprintf(&b, "SOURCE_LIST_TIME:\t%s\n", formatSeconds(src))
	}
	if tgt > 0 {
		fmt.Fprintf(&b, "TARGET_LIST_TIME:\t%s\n", formatSeconds(tgt))
	}
	return b.String()
}

func formatSeconds(s float64) string {
	// time -p style: trim trailing zeros but keep at least one decimal when needed.
	t := fmt.Sprintf("%.2f", s)
	t = strings.TrimRight(t, "0")
	t = strings.TrimRight(t, ".")
	if t == "" {
		return "0"
	}
	return t
}

// dsDepth: root "" → 1; "/a" → 2; "/a/b" → 3.
func dsDepth(suffix string) int {
	if suffix == "" {
		return 1
	}
	return strings.Count(suffix, "/") + 1
}

func filterDepth(pairs []*Pair, depth int) []*Pair {
	var out []*Pair
	for _, p := range pairs {
		if dsDepth(p.DSSuffix) <= depth {
			out = append(out, p)
		}
	}
	return out
}
