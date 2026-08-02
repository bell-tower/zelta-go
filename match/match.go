package match

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/zfs"
)

// Request is a match comparison (analysis only — no presentation dials).
type Request struct {
	Source                  endpoint.Endpoint
	Target                  endpoint.Endpoint
	Props                   []string // empty → resolveListProps
	// Cols hints which display columns the caller cares about (affects list
	// prop selection for written/ivset). Presentation is CLI-owned.
	Cols                    []string
	Depth                   int // 0 = unlimited; zfs -d + pair filter
	Include                 []string
	Exclude                 []string
	NoWritten               bool // list name,guid only when props unset
	PreserveSourceSnapshots bool // retain source history for filtered backup planning
	// SrcContext / TgtContext, when set, merge dataset props after list (backup path).
	SrcContext *zfs.DatasetContext
	TgtContext *zfs.DatasetContext
}

// Result is a completed match comparison (typed data only).
type Result struct {
	Source      endpoint.Endpoint
	Target      endpoint.Endpoint
	SrcRows     []zfs.ListRow
	TgtRows     []zfs.ListRow
	Pairs       []*Pair
	Warnings    []string // filter parse warnings
	SrcListTime float64  // seconds; 0 if not measured / negligible
	TgtListTime float64
}

// DefaultListProps match production dry-run list columns (LIST_WRITTEN default on).
var DefaultListProps = []string{"name", "guid", "written", "creation", "used"}

// MinimalListProps when --no-written (or -p without written/size cols).
var MinimalListProps = []string{"name", "guid"}

// RotateListProps includes origin for clone-lineage classification.
var RotateListProps = []string{"name", "guid", "origin", "written", "snapshots_changed", "creation", "used", "type"}

// Commands returns structured dry-run list operations without contacting any pool.
// Props default to DefaultListProps (no feature probing). Source and Target
// lines appear in request order, skipping endpoints with an empty dataset.
func Commands(req Request) ([]zfs.Command, error) {
	if req.Depth < 0 {
		return nil, fmt.Errorf("depth of '%d' invalid; must be positive", req.Depth)
	}
	props := req.Props
	if len(props) == 0 {
		props = DefaultListProps
	}
	var out []zfs.Command
	for _, ep := range []endpoint.Endpoint{req.Source, req.Target} {
		if ep.Dataset == "" {
			continue
		}
		argv := []string{"zfs", "list", "-H", "-t", "snapshot", "-o", strings.Join(props, ",")}
		if req.Depth > 0 {
			argv = append(argv, "-r", "-d", fmt.Sprintf("%d", req.Depth))
		}
		// Oracle dry-run embeds the full endpoint string as the list target.
		argv = append(argv, ep.String())
		out = append(out, zfs.Command{
			Kind:     zfs.CmdList,
			Endpoint: ep,
			Argv:     argv,
		})
	}
	return out, nil
}

// Compare loads lists and pairs by ds_suffix/GUID. Callers render presentation.
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
	props, err := resolveListProps(ctx, exec, req)
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

	return &Result{
		Source:      srcEp,
		Target:      tgtEp,
		SrcRows:     srcRows,
		TgtRows:     tgtRows,
		Pairs:       pairs,
		Warnings:    append([]string(nil), filt.Warnings...),
		SrcListTime: srcDur,
		TgtListTime: tgtDur,
	}, nil
}

// resolveListProps picks zfs list -o columns (oracle add_written).
// When cols need ivset and Props is empty, probes top-level source features once.
func resolveListProps(ctx context.Context, exec zfs.Executor, req Request) ([]string, error) {
	if len(req.Props) > 0 {
		return req.Props, nil
	}
	wantWritten := !req.NoWritten
	// LIST_WRITTEN + proplist without written/size → skip slow props.
	if wantWritten && len(req.Cols) > 0 && !colsNeedWritten(req.Cols) {
		wantWritten = false
	}
	if !colsNeedIVSet(req.Cols) {
		if wantWritten {
			return DefaultListProps, nil
		}
		return MinimalListProps, nil
	}
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
