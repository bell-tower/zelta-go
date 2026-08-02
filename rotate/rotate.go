// Package rotate plans safe root dataset rotation.
package rotate

import (
	"context"
	"fmt"
	"git.belltower.it/djbell/zelta-go/backup"
	"strings"

	"git.belltower.it/djbell/zelta-go/cmdbuild"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/zfs"
)

type Request struct {
	Source, Target endpoint.Endpoint
	Match          string
	SourceLast     string
	SourceNext     string
	TargetLast     string
	SourceOrigin   string
	OriginVerified bool
	SourceType     string
	Intermediate   bool
	Flags          backup.SendRecv
}

type Step struct {
	Kind     string
	Argv     []string
	DSSuffix string
}

// Failure records one failed operation after Rotate began changing state.
type Failure struct {
	Kind     string
	DSSuffix string
	Err      error
}

// ExecutionResult records progress without attempting destructive rollback.
type ExecutionResult struct {
	Preserved bool
	Completed []string
	Failures  []Failure
	// Stats carries send/recv replication telemetry from the executor when it
	// reports it (zero when the executor does not).
	Stats zfs.PipeStats
}

// NeedsPreservation reports whether any source dataset still has source
// snapshots newer than the confirmed target match after a rotation step.
func NeedsPreservation(result *match.Result) bool {
	if result == nil {
		return false
	}
	for _, pair := range result.Pairs {
		if pair.SrcName != "" && pair.SrcLast != "" && pair.Match != pair.SrcLast {
			return true
		}
	}
	return false
}

// FullSendCount returns how many source pairs will be planned as true full
// sends (no direct match and no verified clone-origin base). Used for the
// "insufficient snapshots; performing full backup" warning — origin-backed
// pairs must not be counted.
func FullSendCount(pairs []*match.Pair, targetRoot string, targetRows []zfs.ListRow) int {
	n := 0
	for _, pair := range pairs {
		if pair == nil || pair.SrcName == "" || pair.SrcLast == "" {
			continue
		}
		if pair.Match != "" {
			continue
		}
		if _, originSnap, ok := endpoint.SplitOrigin(pair.SrcOrigin); ok {
			tgtDS := joinDataset(targetRoot, pair.DSSuffix)
			if hasSnapshot(targetRows, tgtDS+originSnap) {
				continue
			}
		}
		n++
	}
	return n
}

// StreamCount returns how many send/recv pairs are in a plan (excludes rename
// and snapshot steps).
func StreamCount(steps []Step) int {
	n := 0
	for _, step := range steps {
		if step.Kind == "send" {
			n++
		}
	}
	return n
}

// TreeRequest plans a complete dataset tree. TargetRows are used to verify
// source clone origins before they are used as incremental bases.
type TreeRequest struct {
	Source, Target     endpoint.Endpoint
	Pairs              []*match.Pair
	TargetRows         []zfs.ListRow
	PreservationExists bool
	Intermediate       bool
	SyncDirection      backup.SyncDirection
	Flags              backup.SendRecv
}

// PlanTree handles root direct-match and verified source-origin paths and
// plans full sends for source-only children. It remains planner-only.
func PlanTree(req TreeRequest) ([]Step, error) {
	src, tgt := req.Source, req.Target
	root := findRoot(req.Pairs)
	if root == nil || root.SrcName == "" {
		return nil, fmt.Errorf("rotate source root is missing")
	}
	if root.TgtName == "" {
		return nil, fmt.Errorf("rotate target root is missing")
	}
	// Lineage base for send planning (direct GUID match or verified clone origin).
	lineageSnap := root.Match
	if lineageSnap == "" {
		_, originSnap, ok := endpoint.SplitOrigin(root.SrcOrigin)
		if !ok || !hasSnapshot(req.TargetRows, joinDataset(tgt.Dataset, root.DSSuffix)+originSnap) {
			return nil, fmt.Errorf("rotate has no verified common snapshot or source origin")
		}
		lineageSnap = originSnap
	}
	if root.SrcLast == "" || root.SrcLast == lineageSnap {
		return nil, fmt.Errorf("rotate source is up-to-date or has no new snapshot")
	}
	// Awk rename_dataset: preserve name from target latest snapshot (not match/origin).
	preserveSnap := root.TgtLast
	if preserveSnap == "" {
		preserveSnap = lineageSnap
	}
	preserved := tgt.Dataset + "_" + strings.TrimPrefix(preserveSnap, "@")
	if req.PreservationExists {
		return nil, fmt.Errorf("rotate preservation target already exists: %s", preserved)
	}
	rename, err := cmdbuild.RenameArgv(tgt.Dataset, preserved)
	if err != nil {
		return nil, err
	}
	steps := []Step{{Kind: "rename", Argv: rename}}
	for _, pair := range req.Pairs {
		action, err := planPair(src.Dataset, tgt.Dataset, preserved, pair, req)
		if err != nil {
			return nil, err
		}
		steps = append(steps, action...)
	}
	return steps, nil
}

func findRoot(pairs []*match.Pair) *match.Pair {
	for _, pair := range pairs {
		if pair != nil && pair.DSSuffix == "" {
			return pair
		}
	}
	return nil
}

func planPair(sourceRoot, targetRoot, preserved string, pair *match.Pair, req TreeRequest) ([]Step, error) {
	if pair == nil || pair.SrcName == "" || pair.SrcLast == "" {
		return nil, nil
	}
	sourceDataset := joinDataset(sourceRoot, pair.DSSuffix)
	targetDataset := joinDataset(targetRoot, pair.DSSuffix)
	matchName := pair.Match
	sourceEnd := pair.SrcNext
	if sourceEnd == "" {
		sourceEnd = pair.SrcLast
	}
	if sourceEnd == "" || sourceEnd == matchName {
		return nil, nil
	}
	sourceStart := ""
	fromOrigin := false
	if matchName != "" {
		sourceStart = sourceDataset + matchName
	} else if originDS, originSnap, ok := endpoint.SplitOrigin(pair.SrcOrigin); ok && hasSnapshot(req.TargetRows, targetDataset+originSnap) {
		matchName = originSnap
		sourceStart = originDS + originSnap
		fromOrigin = true
	}
	origin := ""
	if fromOrigin {
		origin = preserved + pair.DSSuffix + matchName
	}
	steps := make([]Step, 0, 4)
	if matchName != "" && !fromOrigin {
		seed, err := cmdbuild.Build("SEND", map[string]string{
			"flags":   req.Flags.SendFlags(),
			"ds_snap": sourceDataset + matchName,
		})
		if err != nil {
			return nil, err
		}
		seedRecv, err := cmdbuild.Build("RECV", map[string]string{
			"flags": req.Flags.RecvFlags(pair.SrcType, pair.DSSuffix == "", true, ""),
			"ds":    targetDataset,
		})
		if err != nil {
			return nil, err
		}
		steps = append(steps,
			Step{Kind: "send", Argv: seed, DSSuffix: pair.DSSuffix},
			Step{Kind: "recv", Argv: seedRecv, DSSuffix: pair.DSSuffix},
		)
	}
	vars := map[string]string{
		"flags":   req.Flags.SendFlags(),
		"ds_snap": sourceDataset + sourceEnd,
	}
	if sourceStart != "" {
		vars["intr_snap"] = incrFlag(req.Intermediate) + " " + sourceStart
	}
	send, err := cmdbuild.Build("SEND", vars)
	if err != nil {
		return nil, err
	}
	recv, err := cmdbuild.Build("RECV", map[string]string{
		"flags": req.Flags.RecvFlags(pair.SrcType, pair.DSSuffix == "", true, origin),
		"ds":    targetDataset,
	})
	if err != nil {
		return nil, err
	}
	return append(steps,
		Step{Kind: "send", Argv: send, DSSuffix: pair.DSSuffix},
		Step{Kind: "recv", Argv: recv, DSSuffix: pair.DSSuffix},
	), nil
}

func joinDataset(root, suffix string) string {
	if suffix == "" {
		return root
	}
	return root + suffix
}

func hasSnapshot(rows []zfs.ListRow, name string) bool {
	for _, row := range rows {
		if row.Name == name {
			return true
		}
	}
	return false
}

// Plan handles one root pair for callers that do not yet have a full match
// tree. PlanTree is preferred for CLI and recursive callers.
func Plan(req Request) ([]Step, error) {
	root := &match.Pair{
		DSSuffix:  "",
		SrcName:   req.Source.Dataset,
		TgtName:   req.Target.Dataset,
		Match:     req.Match,
		SrcLast:   req.SourceLast,
		SrcNext:   req.SourceNext,
		TgtLast:   req.TargetLast,
		SrcOrigin: req.SourceOrigin,
		SrcType:   req.SourceType,
	}
	var targetRows []zfs.ListRow
	if req.OriginVerified {
		if _, originSnap, ok := endpoint.SplitOrigin(req.SourceOrigin); ok {
			targetRows = []zfs.ListRow{{Name: req.Target.Dataset + originSnap}}
		}
	}
	return PlanTree(TreeRequest{
		Source: req.Source, Target: req.Target, Pairs: []*match.Pair{root},
		TargetRows: targetRows, Intermediate: req.Intermediate, Flags: req.Flags,
	})
}

func incrFlag(intermediate bool) string {
	if intermediate {
		return "-I"
	}
	return "-i"
}

func Format(steps []Step) string {
	var b strings.Builder
	for _, step := range steps {
		if len(step.Argv) == 0 {
			continue
		}
		b.WriteString(strings.Join(step.Argv, " "))
		b.WriteByte('\n')
	}
	return b.String()
}

// FormatRemote renders a dry-run plan with endpoint-aware command wrappers.
// Send/receive pairs use the same direction rules as backup dry-runs.
func FormatRemote(steps []Step, source, target, direction string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		if len(step.Argv) == 0 {
			continue
		}
		switch step.Kind {
		case "rename":
			line, err := zfs.CommandShell(target, step.Argv)
			if err != nil {
				return "", err
			}
			b.WriteString(line)
		case "snapshot":
			line, err := zfs.CommandShell(source, step.Argv)
			if err != nil {
				return "", err
			}
			b.WriteString(line)
		case "send":
			if i+1 >= len(steps) || steps[i+1].Kind != "recv" {
				return "", fmt.Errorf("rotate: send without receive for %q", step.DSSuffix)
			}
			line, err := zfs.PipeShellDirection(source, target, step.Argv, steps[i+1].Argv, direction)
			if err != nil {
				return "", err
			}
			b.WriteString(line)
			i++
		case "recv":
			return "", fmt.Errorf("rotate: receive without send for %q", step.DSSuffix)
		default:
			b.WriteString(zfs.SoftJoin(step.Argv))
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// Execute applies a previously validated plan. Send and receive steps are
// paired in order and run through the executor so remote stdin semantics stay
// centralized in zfs.Real.
func Execute(ctx context.Context, exec zfs.Executor, req TreeRequest, steps []Step) error {
	result, err := ExecuteResult(ctx, exec, req, steps)
	if err != nil {
		return err
	}
	if len(result.Failures) > 0 {
		return result.Failures[0].Err
	}
	return nil
}

// ExecuteResult applies a validated plan. Preservation and source snapshot
// failures stop the operation; independent child streams continue so callers
// receive a complete partial-progress report.
func ExecuteResult(ctx context.Context, exec zfs.Executor, req TreeRequest, steps []Step) (*ExecutionResult, error) {
	target := req.Target
	target.Snapshot = ""
	result := &ExecutionResult{}
	if reporter, ok := exec.(zfs.PipeStatsReporter); ok {
		reporter.TakeStats() // discard telemetry from earlier runs on this exec
	}
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		switch step.Kind {
		case "rename":
			if len(step.Argv) < 4 {
				return nil, fmt.Errorf("rotate: malformed rename step")
			}
			if err := exec.Rename(ctx, target.String(), step.Argv[len(step.Argv)-2], step.Argv[len(step.Argv)-1]); err != nil {
				result.Failures = append(result.Failures, Failure{Kind: step.Kind, Err: fmt.Errorf("rename target: %w", err)})
				return result, nil
			}
			result.Preserved = true
		case "snapshot":
			if len(step.Argv) == 0 {
				return nil, fmt.Errorf("rotate: malformed snapshot step")
			}
			if err := exec.Snapshot(ctx, req.Source.String(), step.Argv[len(step.Argv)-1], true); err != nil {
				result.Failures = append(result.Failures, Failure{Kind: step.Kind, Err: fmt.Errorf("snapshot source: %w", err)})
				return result, nil
			}
		case "send":
			if i+1 >= len(steps) || steps[i+1].Kind != "recv" {
				return nil, fmt.Errorf("rotate: send without receive for %q", step.DSSuffix)
			}
			recv := steps[i+1]
			if err := exec.RunPipeDirection(ctx, req.Source.String(), step.Argv, req.Target.String(), recv.Argv, req.SyncDirection.PipeArg()); err != nil {
				result.Failures = append(result.Failures, Failure{Kind: step.Kind, DSSuffix: step.DSSuffix, Err: fmt.Errorf("sync %s: %w", step.DSSuffix, err)})
				if i+2 < len(steps) && steps[i+2].Kind == "send" && steps[i+2].DSSuffix == step.DSSuffix {
					i += 3
				} else {
					i++
				}
				continue
			}
			if i+2 >= len(steps) || steps[i+2].Kind != "send" || steps[i+2].DSSuffix != step.DSSuffix {
				result.Completed = append(result.Completed, step.DSSuffix)
			}
			i++
		case "recv":
			return nil, fmt.Errorf("rotate: receive without send for %q", step.DSSuffix)
		default:
			return nil, fmt.Errorf("rotate: unknown step %q", step.Kind)
		}
	}
	if reporter, ok := exec.(zfs.PipeStatsReporter); ok {
		result.Stats = reporter.TakeStats()
	}
	return result, nil
}
