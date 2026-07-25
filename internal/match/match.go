package match

import (
	"context"
	"fmt"

	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

// Request is a match comparison.
type Request struct {
	Source string
	Target string
	// Props for zfs list; empty → default name,guid
	Props []string
}

// Result is a stub until the full engine lands.
type Result struct {
	Source  endpoint.Endpoint
	Target  endpoint.Endpoint
	SrcRows []zfs.ListRow
	TgtRows []zfs.ListRow
}

// DefaultListProps are enough for early GUID work.
var DefaultListProps = []string{"name", "guid"}

// Compare loads source and target lists via exec. Full pairing/render is TODO.
func Compare(ctx context.Context, exec zfs.Executor, req Request) (*Result, error) {
	srcEp, err := endpoint.Parse(req.Source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	tgtEp, err := endpoint.Parse(req.Target)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	props := req.Props
	if len(props) == 0 {
		props = DefaultListProps
	}
	srcLines, err := exec.List(ctx, req.Source, srcEp.Dataset, props)
	if err != nil {
		return nil, fmt.Errorf("list source: %w", err)
	}
	tgtLines, err := exec.List(ctx, req.Target, tgtEp.Dataset, props)
	if err != nil {
		return nil, fmt.Errorf("list target: %w", err)
	}
	srcRows, err := zfs.ParseListLines(srcLines, props)
	if err != nil {
		return nil, fmt.Errorf("parse source: %w", err)
	}
	tgtRows, err := zfs.ParseListLines(tgtLines, props)
	if err != nil {
		return nil, fmt.Errorf("parse target: %w", err)
	}
	return &Result{
		Source:  srcEp,
		Target:  tgtEp,
		SrcRows: srcRows,
		TgtRows: tgtRows,
	}, nil
}
