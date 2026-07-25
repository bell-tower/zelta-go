package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/backup"
	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/match"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/rotate"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

func runRotate(args []string) int {
	p, err := opt.Parse("rotate", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		rotateUsage()
		return 0
	}
	if len(p.Operands) != 2 {
		rotateUsage()
		return 2
	}
	exec := &zfs.Real{}
	m, err := match.Compare(context.Background(), exec, match.Request{
		Source: p.Operands[0], Target: p.Operands[1], Props: match.RotateListProps,
		Scripting: true, Parsable: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	var root *match.Pair
	for _, pair := range m.Pairs {
		if pair.DSSuffix == "" {
			root = pair
			break
		}
	}
	if root == nil {
		fmt.Fprintln(os.Stderr, "zelta rotate: root dataset is missing")
		return 1
	}
	savepoint, shouldSnapshot, err := prepareRotateSnapshot(p.Env.Get("SNAP_MODE"), p.Env.Get("SNAP_NAME"), p.Env.Get("SNAP_TIME"), p.Env.Get("SNAP_SIZE"), m.Pairs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	request := rotate.TreeRequest{
		Source: p.Operands[0], Target: p.Operands[1], Pairs: m.Pairs,
		TargetRows: m.TgtRows, Intermediate: p.Env.Bool("SEND_INTR", true),
		SyncDirection: rotateDirection(p.Env.Get("SYNC_DIRECTION")),
		Flags:         opt.SendRecvFrom(p.Env),
	}
	steps, err := rotate.PlanTree(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	if shouldSnapshot {
		source, err := endpoint.Parse(p.Operands[0])
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta rotate: source: %v\n", err)
			return 1
		}
		argv, err := cmdbuild.SnapArgv(source.Dataset + savepoint)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
			return 1
		}
		steps = append([]rotate.Step{{Kind: "snapshot", Argv: argv}}, steps...)
	}
	if len(steps) == 0 || len(steps[0].Argv) == 0 {
		fmt.Fprintln(os.Stderr, "zelta rotate: empty preservation plan")
		return 1
	}
	preserved := preservationFromSteps(steps)
	if preserved == "" {
		fmt.Fprintln(os.Stderr, "zelta rotate: preservation rename is missing")
		return 1
	}
	exists, err := exec.Exists(context.Background(), p.Operands[1], preserved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: preservation check: %v\n", err)
		return 1
	}
	request.PreservationExists = exists
	steps, err = rotate.PlanTree(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	if p.Env.Bool("DRYRUN", false) {
		out, err := rotate.FormatRemote(steps, p.Operands[0], p.Operands[1], request.SyncDirection)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
			return 1
		}
		fmt.Print(out)
		return 0
	}
	execution, err := rotate.ExecuteResult(context.Background(), exec, request, steps)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	if len(execution.Failures) > 0 && !execution.Preserved {
		printRotateFailures(execution.Failures)
		return 1
	}
	post, err := match.Compare(context.Background(), exec, match.Request{
		Source: p.Operands[0], Target: p.Operands[1], Props: match.RotateListProps,
		Scripting: true, Parsable: true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: rotate confirmation: %v\n", err)
	} else {
		if rotate.NeedsPreservation(post) {
			preservedEP := preservationEndpoint(p.Operands[1], preserved)
			fmt.Fprintf(os.Stdout, "ensure preservation of diverged replica with: zelta backup %s %s\n", p.Operands[0], preservedEP)
		}
		fmt.Fprintf(os.Stdout, "to ensure target is up-to-date, run: zelta backup %s %s\n", p.Operands[0], p.Operands[1])
	}
	if len(execution.Failures) > 0 {
		printRotateFailures(execution.Failures)
		fmt.Fprintf(os.Stderr, "zelta rotate: preserved target remains at %s; incomplete children require manual recovery\n", preserved)
		return 1
	}
	return 0
}

func rotateUsage() { fmt.Fprintln(os.Stderr, "usage: zelta rotate SOURCE TARGET") }

func rotateDirection(value string) string {
	switch value {
	case "0", "no", "false", "off", "NO", "FALSE", "OFF":
		return ""
	case "", "pull", "PULL":
		return "PULL"
	default:
		return value
	}
}

func preservationEndpoint(target, dataset string) string {
	ep, err := endpoint.Parse(target)
	if err != nil {
		return dataset
	}
	ep.Dataset = dataset
	ep.Snapshot = ""
	return ep.String()
}

func preservationFromSteps(steps []rotate.Step) string {
	for _, step := range steps {
		if step.Kind == "rename" && len(step.Argv) >= 2 {
			return step.Argv[len(step.Argv)-1]
		}
	}
	return ""
}

func printRotateFailures(failures []rotate.Failure) {
	for _, failure := range failures {
		if failure.DSSuffix == "" {
			fmt.Fprintf(os.Stderr, "zelta rotate: %s: %v\n", failure.Kind, failure.Err)
		} else {
			fmt.Fprintf(os.Stderr, "zelta rotate: %s %s: %v\n", failure.Kind, failure.DSSuffix, failure.Err)
		}
	}
}

func prepareRotateSnapshot(mode, requested, snapTime, snapSize string, pairs []*match.Pair) (string, bool, error) {
	mode = strings.ToUpper(strings.TrimSpace(mode))
	if mode == "" {
		mode = "IF_NEEDED"
	}
	force := mode == "ALWAYS" || mode == "1" || mode == "YES" || mode == "TRUE"
	disabled := mode == "0" || mode == "OFF" || mode == "NO" || mode == "FALSE" || mode == "SKIP"
	need := force
	if !force && !disabled && (strings.TrimSpace(snapTime) != "" || strings.TrimSpace(snapSize) != "") {
		views := make([]backup.PairView, 0, len(pairs))
		for _, pair := range pairs {
			views = append(views, backup.PairView{
				SrcName: pair.SrcName, SrcLast: pair.SrcLast, SrcWritten: pair.SrcWritten,
				SrcSnapshotsChanged: pair.SrcSnapshotsChanged,
			})
		}
		need = backup.ShouldSnapshotWithThresholds(backup.SnapIfNeeded, views, snapTime, snapSize) != ""
	}
	for _, pair := range pairs {
		if pair == nil || pair.SrcName == "" {
			continue
		}
		if strings.TrimSpace(snapTime) != "" || strings.TrimSpace(snapSize) != "" {
			continue
		}
		if pair.SrcLast == "" || (pair.Match != "" && pair.Match == pair.SrcLast) || written(pair.SrcWritten) {
			need = true
		}
	}
	if !need {
		return "", false, nil
	}
	if disabled {
		return "", false, fmt.Errorf("source snapshot required for rotation")
	}
	name := strings.TrimPrefix(strings.TrimSpace(requested), "@")
	if name == "" {
		name = backup.DefaultSnapName()
	}
	savepoint := "@" + name
	for _, pair := range pairs {
		if pair != nil && pair.SrcName != "" {
			pair.SrcLast = savepoint
		}
	}
	return savepoint, true, nil
}

func written(value string) bool {
	value = strings.TrimSpace(value)
	return value != "" && value != "-" && value != "0" && value != "0B"
}
