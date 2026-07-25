// Package lineage plans the non-destructive dataset lineage operations.
package lineage

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

// Step is one local argv operation in a dry-run plan.
type Step struct {
	Kind string
	Argv []string
}

// Failure records an operation that could not be completed. DSSuffix is empty
// for root-wide operations such as preservation rename or final snapshot.
type Failure struct {
	Kind     string
	DSSuffix string
	Err      error
}

// ExecutionResult describes safe progress after a non-destructive operation.
// A preserved tree is never removed automatically during recovery.
type ExecutionResult struct {
	Preserved bool
	Completed []string
	Failures  []Failure
}

// Apply executes a planned revert. Root preservation is a prerequisite; once
// it succeeds, independent child clones are attempted even if one fails.
func Apply(ctx context.Context, exec zfs.Executor, endpointName string, steps []Step) (*ExecutionResult, error) {
	ep, err := endpoint.Parse(endpointName)
	if err != nil {
		return nil, err
	}
	ep.Snapshot = ""
	result := &ExecutionResult{}
	for _, step := range steps {
		if len(step.Argv) == 0 {
			return nil, fmt.Errorf("lineage: malformed %s step", step.Kind)
		}
		var opErr error
		switch step.Kind {
		case "rename":
			if len(step.Argv) < 4 {
				return nil, fmt.Errorf("lineage: malformed rename step")
			}
			opErr = exec.Rename(ctx, ep.String(), step.Argv[len(step.Argv)-2], step.Argv[len(step.Argv)-1])
			if opErr == nil {
				result.Preserved = true
			}
		case "clone":
			if len(step.Argv) < 2 {
				return nil, fmt.Errorf("lineage: malformed clone step")
			}
			opErr = exec.Clone(ctx, ep.String(), step.Argv[len(step.Argv)-2], step.Argv[len(step.Argv)-1])
		case "snapshot":
			opErr = exec.Snapshot(ctx, ep.String(), step.Argv[len(step.Argv)-1], true)
		default:
			return nil, fmt.Errorf("lineage: unknown step %q", step.Kind)
		}
		if opErr != nil {
			failure := Failure{Kind: step.Kind, Err: opErr}
			if step.Kind == "clone" {
				failure.DSSuffix = cloneSuffix(ep.Dataset, step.Argv[len(step.Argv)-1])
			}
			result.Failures = append(result.Failures, failure)
			if step.Kind == "rename" || step.Kind == "snapshot" {
				return result, nil
			}
			continue
		}
		if step.Kind == "clone" {
			result.Completed = append(result.Completed, cloneSuffix(ep.Dataset, step.Argv[len(step.Argv)-1]))
		}
	}
	return result, nil
}

func cloneSuffix(root, dataset string) string {
	suffix, err := endpoint.DSSuffix(root, dataset)
	if err != nil || suffix == "" {
		return ""
	}
	return suffix
}

// CloneRequest describes a recursive clone operation.
type CloneRequest struct {
	Source string
	Target string
	Depth  int
}

// Snapshot is one source dataset snapshot selected for cloning.
type Snapshot struct {
	Dataset  string
	Snapshot string
}

// ClonePlan builds clone commands from source list rows. Rows must contain
// name and type and be ordered newest-first, as produced by ListArgv.
func ClonePlan(req CloneRequest, rows []zfs.ListRow, targetExists bool) ([]Step, error) {
	src, err := endpoint.Parse(req.Source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	tgt, err := endpoint.Parse(req.Target)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	if tgt.Snapshot != "" {
		return nil, fmt.Errorf("target: clone target must not include a snapshot")
	}
	if !sameLocation(src, tgt) {
		return nil, fmt.Errorf("clone source and target must be on the same host and pool")
	}
	if targetExists {
		return nil, fmt.Errorf("clone target already exists: %s", tgt.Dataset)
	}
	selected := selectSnapshots(src, rows, req.Depth)
	if len(selected) == 0 {
		return nil, fmt.Errorf("clone source has no usable snapshots: %s", src.Dataset)
	}
	steps := make([]Step, 0, len(selected))
	for _, snap := range selected {
		suffix, err := endpoint.DSSuffix(src.Dataset, snap.Dataset)
		if err != nil {
			return nil, err
		}
		target := tgt.Dataset + suffix
		argv, err := cmdbuild.CloneArgv(snap.Dataset+"@"+snap.Snapshot, target)
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Kind: "clone", Argv: argv})
	}
	return steps, nil
}

// Clone retains the small root API for callers that already supply an
// explicit source snapshot.
func Clone(req CloneRequest) ([]Step, error) {
	src, err := endpoint.Parse(req.Source)
	if err != nil {
		return nil, err
	}
	if src.Snapshot == "" {
		return nil, fmt.Errorf("source: clone requires an explicit snapshot")
	}
	rows := []zfs.ListRow{{Name: src.Dataset + "@" + src.Snapshot, Props: map[string]string{"type": "snapshot"}}}
	return ClonePlan(req, rows, false)
}

func selectSnapshots(src endpoint.Endpoint, rows []zfs.ListRow, depth int) []Snapshot {
	seen := make(map[string]bool)
	var out []Snapshot
	for _, row := range rows {
		if row.Props["type"] != "snapshot" {
			continue
		}
		at := strings.LastIndex(row.Name, "@")
		if at <= 0 || at == len(row.Name)-1 {
			continue
		}
		dataset, snap := row.Name[:at], row.Name[at+1:]
		suffix, err := endpoint.DSSuffix(src.Dataset, dataset)
		if err != nil || !withinDepth(suffix, depth) || seen[suffix] {
			continue
		}
		if src.Snapshot != "" && snap != src.Snapshot {
			continue
		}
		seen[suffix] = true
		out = append(out, Snapshot{Dataset: dataset, Snapshot: snap})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return strings.Count(out[i].Dataset, "/") < strings.Count(out[j].Dataset, "/")
	})
	return out
}

func withinDepth(suffix string, depth int) bool {
	if depth <= 0 {
		return true
	}
	if suffix == "" {
		return 1 <= depth
	}
	return strings.Count(suffix, "/")+1 <= depth
}

// RevertRequest describes a root revert operation.
type RevertRequest struct {
	Endpoint      string
	Depth         int
	AfterSnapshot string
}

// RevertPlan preserves the current root and clones the selected snapshot tree
// back to its original names. It never uses zfs rollback -F or overwrites in
// place. Rows must contain name and type and be ordered newest-first.
func RevertPlan(req RevertRequest, rows []zfs.ListRow, preservationExists bool) ([]Step, error) {
	ep, err := endpoint.Parse(req.Endpoint)
	if err != nil {
		return nil, err
	}
	selected := selectSnapshots(ep, rows, req.Depth)
	if len(selected) == 0 {
		return nil, fmt.Errorf("revert source has no usable snapshots: %s", ep.Dataset)
	}
	rootSnapshot := ""
	for _, snap := range selected {
		if snap.Dataset == ep.Dataset {
			rootSnapshot = snap.Snapshot
			break
		}
	}
	if rootSnapshot == "" {
		return nil, fmt.Errorf("revert source has no root snapshot: %s", ep.Dataset)
	}
	preserved := ep.Dataset + "_" + rootSnapshot
	if preservationExists {
		return nil, fmt.Errorf("revert preservation target already exists: %s", preserved)
	}
	rename, err := cmdbuild.RenameArgv(ep.Dataset, preserved)
	if err != nil {
		return nil, err
	}
	steps := []Step{{Kind: "rename", Argv: rename}}
	for _, snap := range selected {
		suffix, err := endpoint.DSSuffix(ep.Dataset, snap.Dataset)
		if err != nil {
			return nil, err
		}
		cloneSource := preserved + suffix + "@" + snap.Snapshot
		cloneTarget := ep.Dataset + suffix
		clone, err := cmdbuild.CloneArgv(cloneSource, cloneTarget)
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Kind: "clone", Argv: clone})
	}
	if req.AfterSnapshot != "" {
		snap, err := cmdbuild.SnapArgv(ep.Dataset + "@" + strings.TrimPrefix(req.AfterSnapshot, "@"))
		if err != nil {
			return nil, err
		}
		steps = append(steps, Step{Kind: "snapshot", Argv: snap})
	}
	return steps, nil
}

// Revert retains the original root-only helper for callers that provide an
// explicit snapshot. New callers should use RevertPlan with list rows.
func Revert(req RevertRequest) ([]Step, error) {
	ep, err := endpoint.Parse(req.Endpoint)
	if err != nil {
		return nil, err
	}
	if ep.Snapshot == "" {
		return nil, fmt.Errorf("revert requires an explicit snapshot")
	}
	rows := []zfs.ListRow{{Name: ep.Dataset + "@" + ep.Snapshot, Props: map[string]string{"type": "snapshot"}}}
	return RevertPlan(req, rows, false)
}

func sameLocation(a, b endpoint.Endpoint) bool {
	aHost := a.Host
	bHost := b.Host
	if aHost == "localhost" {
		aHost = ""
	}
	if bHost == "localhost" {
		bHost = ""
	}
	aLocal := !a.Remote || aHost == ""
	bLocal := !b.Remote || bHost == ""
	if a.User != b.User || aHost != bHost || aLocal != bLocal {
		return false
	}
	return poolOf(a.Dataset) == poolOf(b.Dataset)
}

func poolOf(dataset string) string {
	if i := strings.IndexByte(dataset, '/'); i >= 0 {
		return dataset[:i]
	}
	return dataset
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

// FormatRemote renders each lineage command on its dataset endpoint. Clone
// and revert are local ZFS operations on one host, so no pipe direction is
// involved.
func FormatRemote(steps []Step, endpointName string) (string, error) {
	var b strings.Builder
	for _, step := range steps {
		if len(step.Argv) == 0 {
			continue
		}
		line, err := zfs.CommandShell(endpointName, step.Argv)
		if err != nil {
			return "", err
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}
