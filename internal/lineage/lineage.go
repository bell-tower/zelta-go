// Package lineage plans the non-destructive dataset lineage operations.
package lineage

import (
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/endpoint"
)

// Step is one local argv operation in a dry-run plan.
type Step struct {
	Kind string
	Argv []string
}

// CloneRequest describes a root clone operation. Snapshot must be explicit in
// the first slice; recursive latest-snapshot selection comes later.
type CloneRequest struct {
	Source string
	Target string
}

// Clone builds a non-overwriting root clone plan.
func Clone(req CloneRequest) ([]Step, error) {
	src, err := endpoint.Parse(req.Source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	tgt, err := endpoint.Parse(req.Target)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	if src.Snapshot == "" {
		return nil, fmt.Errorf("source: clone requires an explicit snapshot")
	}
	if tgt.Snapshot != "" {
		return nil, fmt.Errorf("target: clone target must not include a snapshot")
	}
	if !sameHost(src, tgt) {
		return nil, fmt.Errorf("clone source and target must be on the same host")
	}
	argv, err := cmdbuild.CloneArgv(src.Dataset+"@"+src.Snapshot, tgt.Dataset)
	if err != nil {
		return nil, err
	}
	return []Step{{Kind: "clone", Argv: argv}}, nil
}

// RevertRequest describes a root revert operation.
type RevertRequest struct {
	Endpoint string
}

// Revert preserves the current root and clones the selected snapshot back to
// its original name. It never uses zfs rollback -F or overwrites in place.
func Revert(req RevertRequest) ([]Step, error) {
	ep, err := endpoint.Parse(req.Endpoint)
	if err != nil {
		return nil, err
	}
	if ep.Snapshot == "" {
		return nil, fmt.Errorf("revert requires an explicit snapshot")
	}
	preserved := ep.Dataset + "_" + ep.Snapshot
	rename, err := cmdbuild.RenameArgv(ep.Dataset, preserved)
	if err != nil {
		return nil, err
	}
	clone, err := cmdbuild.CloneArgv(ep.Dataset+"@"+ep.Snapshot, ep.Dataset)
	if err != nil {
		return nil, err
	}
	return []Step{{Kind: "rename", Argv: rename}, {Kind: "clone", Argv: clone}}, nil
}

func sameHost(a, b endpoint.Endpoint) bool {
	return a.User == b.User && a.Host == b.Host && a.Remote == b.Remote
}

// Format renders a plan in the same one-command-per-line style as dry-run
// output elsewhere in the Go port.
func Format(steps []Step) string {
	var lines []string
	for _, step := range steps {
		if len(step.Argv) > 0 {
			lines = append(lines, strings.Join(step.Argv, " "))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
