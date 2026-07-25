// Package rotate plans safe root dataset rotation.
package rotate

import (
	"context"
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/match"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

type Request struct {
	Source, Target string
	Match          string
	SourceLast     string
	SourceNext     string
	TargetLast     string
	SourceOrigin   string
	OriginVerified bool
	SourceType     string
	Intermediate   bool
	Flags          opt.SendRecv
}

type Step struct {
	Kind     string
	Argv     []string
	DSSuffix string
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

// TreeRequest plans a complete dataset tree. TargetRows are used to verify
// source clone origins before they are used as incremental bases.
type TreeRequest struct {
	Source, Target     string
	Pairs              []*match.Pair
	TargetRows         []zfs.ListRow
	PreservationExists bool
	Intermediate       bool
	SyncDirection      string
	Flags              opt.SendRecv
}

// PlanTree handles root direct-match and verified source-origin paths and
// plans full sends for source-only children. It remains planner-only.
func PlanTree(req TreeRequest) ([]Step, error) {
	src, err := endpoint.Parse(req.Source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	tgt, err := endpoint.Parse(req.Target)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}
	root := findRoot(req.Pairs)
	if root == nil || root.SrcName == "" {
		return nil, fmt.Errorf("rotate source root is missing")
	}
	if root.TgtName == "" {
		return nil, fmt.Errorf("rotate target root is missing")
	}
	matchName := root.Match
	if matchName == "" {
		_, originSnap, ok := splitOrigin(root.SrcOrigin)
		if !ok || !hasSnapshot(req.TargetRows, joinDataset(tgt.Dataset, root.DSSuffix)+originSnap) {
			return nil, fmt.Errorf("rotate has no verified common snapshot or source origin")
		}
		matchName = originSnap
	} else if root.TgtLast == "" {
		return nil, fmt.Errorf("rotate target has no usable snapshot")
	}
	if root.SrcLast == "" || root.SrcLast == matchName {
		return nil, fmt.Errorf("rotate source is up-to-date or has no new snapshot")
	}
	preserved := tgt.Dataset + "_" + strings.TrimPrefix(matchName, "@")
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
	if matchName != "" {
		sourceStart = sourceDataset + matchName
	} else if originDS, originSnap, ok := splitOrigin(pair.SrcOrigin); ok && hasSnapshot(req.TargetRows, targetDataset+originSnap) {
		matchName = originSnap
		sourceStart = originDS + originSnap
	}
	origin := ""
	if matchName != "" {
		origin = preserved + pair.DSSuffix + matchName
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
		"flags": recvFlags(req.Flags, pair.SrcType, origin, pair.DSSuffix == ""),
		"ds":    targetDataset,
	})
	if err != nil {
		return nil, err
	}
	return []Step{
		{Kind: "send", Argv: send, DSSuffix: pair.DSSuffix},
		{Kind: "recv", Argv: recv, DSSuffix: pair.DSSuffix},
	}, nil
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
		SrcName:   req.Source,
		TgtName:   req.Target,
		Match:     req.Match,
		SrcLast:   req.SourceLast,
		SrcNext:   req.SourceNext,
		TgtLast:   req.TargetLast,
		SrcOrigin: req.SourceOrigin,
		SrcType:   req.SourceType,
	}
	var targetRows []zfs.ListRow
	if req.OriginVerified {
		if _, originSnap, ok := splitOrigin(req.SourceOrigin); ok {
			tgt, err := endpoint.Parse(req.Target)
			if err != nil {
				return nil, err
			}
			targetRows = []zfs.ListRow{{Name: tgt.Dataset + originSnap}}
		}
	}
	return PlanTree(TreeRequest{
		Source: req.Source, Target: req.Target, Pairs: []*match.Pair{root},
		TargetRows: targetRows, Intermediate: req.Intermediate, Flags: req.Flags,
	})
}

func splitOrigin(origin string) (string, string, bool) {
	i := strings.LastIndex(origin, "@")
	if i <= 0 || i == len(origin)-1 {
		return "", "", false
	}
	return origin[:i], origin[i:], true
}

func incrFlag(intermediate bool) string {
	if intermediate {
		return "-I"
	}
	return "-i"
}

func recvFlags(f opt.SendRecv, sourceType, origin string, root bool) string {
	if f.RecvOverride != "" {
		return f.RecvOverride
	}
	var parts []string
	if f.RecvDefault != "" {
		parts = append(parts, f.RecvDefault)
	}
	if root && f.RecvTop != "" {
		parts = append(parts, f.RecvTop)
	}
	if sourceType == "volume" {
		if f.RecvVol != "" {
			parts = append(parts, f.RecvVol)
		}
	} else if f.RecvFS != "" {
		parts = append(parts, f.RecvFS)
	}
	for _, prop := range f.RecvPropsAdd {
		if prop != "" {
			parts = append(parts, "-o "+prop)
		}
	}
	for _, prop := range f.RecvPropsDel {
		if prop != "" {
			parts = append(parts, "-x "+prop)
		}
	}
	if f.Resume && f.RecvPartial != "" {
		parts = append(parts, f.RecvPartial)
	}
	if origin != "" {
		parts = append(parts, "-o origin="+origin)
	}
	return strings.Join(parts, " ")
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
	target, err := endpoint.Parse(req.Target)
	if err != nil {
		return fmt.Errorf("target: %w", err)
	}
	target.Snapshot = ""
	for i := 0; i < len(steps); i++ {
		step := steps[i]
		switch step.Kind {
		case "rename":
			if len(step.Argv) < 4 {
				return fmt.Errorf("rotate: malformed rename step")
			}
			if err := exec.Rename(ctx, target.String(), step.Argv[len(step.Argv)-2], step.Argv[len(step.Argv)-1]); err != nil {
				return fmt.Errorf("rename target: %w", err)
			}
		case "snapshot":
			if len(step.Argv) == 0 {
				return fmt.Errorf("rotate: malformed snapshot step")
			}
			if err := exec.Snapshot(ctx, req.Source, step.Argv[len(step.Argv)-1], true); err != nil {
				return fmt.Errorf("snapshot source: %w", err)
			}
		case "send":
			if i+1 >= len(steps) || steps[i+1].Kind != "recv" {
				return fmt.Errorf("rotate: send without receive for %q", step.DSSuffix)
			}
			recv := steps[i+1]
			if err := exec.RunPipeDirection(ctx, req.Source, step.Argv, req.Target, recv.Argv, req.SyncDirection); err != nil {
				return fmt.Errorf("sync %s: %w", step.DSSuffix, err)
			}
			i++
		case "recv":
			return fmt.Errorf("rotate: receive without send for %q", step.DSSuffix)
		}
	}
	return nil
}
