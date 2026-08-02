package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/cmdbuild"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/rotate"
)

func runRotate(args []string) int {
	p, err := opt.Parse("rotate", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := newLogSink(p)
	defer sink.Close()
	printWarns(sink, p.Warnings)
	if p.Usage {
		rotateUsage()
		return 0
	}
	if len(p.Operands) != 2 {
		rotateUsage()
		return 2
	}
	exec := newReal()
	src, err := parseEndpoint(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: source: %v\n", err)
		return 1
	}
	tgt, err := parseEndpoint(p.Operands[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: target: %v\n", err)
		return 1
	}
	m, err := match.Compare(context.Background(), exec, match.Request{
		Source: src, Target: tgt, Props: match.RotateListProps,
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
	syncDir, err := rotateDirection(p.Env.Get("SYNC_DIRECTION"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	request := rotate.TreeRequest{
		Source: src, Target: tgt, Pairs: m.Pairs,
		TargetRows: m.TgtRows, Intermediate: p.Env.Bool("SEND_INTR", true),
		SyncDirection: syncDir,
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
	exists, err := exec.Exists(context.Background(), tgt.String(), preserved)
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
	// Re-plan above drops the snapshot prefix; re-apply when needed.
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
	if shouldSnapshot {
		fmt.Fprintf(os.Stdout, "source is written; snapshotting: %s\n", savepoint)
	}
	tgtRoot := p.Operands[1]
	if ep, err := endpoint.Parse(p.Operands[1]); err == nil {
		tgtRoot = ep.Dataset
	}
	if fullCount := rotate.FullSendCount(m.Pairs, tgtRoot, m.TgtRows); fullCount > 0 {
		sink.Warning(fmt.Sprintf("insufficient snapshots; performing full backup for %d datasets", fullCount))
	}
	if p.Env.Bool("DRYRUN", false) {
		out, err := rotate.FormatRemote(steps, src.String(), tgt.String(), request.SyncDirection.PipeArg())
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
			return 1
		}
		// Oracle: dry-run "+ …" lines are LOG_NOTICE.
		emitBlob(sink, out)
		return 0
	}
	execStart := time.Now()
	execution, err := rotate.ExecuteResult(context.Background(), exec, request, steps)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	secs := time.Since(execStart).Seconds()
	if len(execution.Failures) > 0 && !execution.Preserved {
		printRotateFailures(execution.Failures)
		return 1
	}
	if execution.Preserved {
		fmt.Fprintf(os.Stdout, "renaming '%s' to '%s'\n", p.Operands[1], preserved)
	}
	_, err = match.Compare(context.Background(), exec, match.Request{
		Source: src, Target: tgt, Props: match.RotateListProps,
		Scripting: true, Parsable: true,
	})
	if err != nil {
		sink.Warning(fmt.Sprintf("rotate confirmation: %v", err))
	} else {
		// Oracle LOG_NOTICE.
		sink.Notice(fmt.Sprintf("to ensure target is up-to-date, run: zelta backup %s %s", p.Operands[0], p.Operands[1]))
	}
	if streams := rotate.StreamCount(steps); streams > 0 && len(execution.Failures) == 0 {
		s := execution.Stats
		streams := s.Streams
		if streams == 0 {
			streams = rotate.StreamCount(steps)
		}
		if s.Secs > 0 {
			secs = s.Secs
		}
		// Awk parity: size/stream counts come from zfs send -P and zfs recv -v.
		fmt.Fprintf(os.Stdout, "%s sent, %d streams received in %g seconds\n", backup.HumanBytes(s.Bytes), streams, secs)
	}
	if len(execution.Failures) > 0 {
		printRotateFailures(execution.Failures)
		fmt.Fprintf(os.Stderr, "zelta rotate: preserved target remains at %s; incomplete children require manual recovery\n", preserved)
		return 1
	}
	return 0
}

func rotateUsage() { fmt.Fprintln(os.Stderr, "usage: zelta rotate SOURCE TARGET") }

func rotateDirection(value string) (backup.SyncDirection, error) {
	return backup.ParseSyncDirection(value)
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
	snapMode, err := backup.ParseSnapMode(mode)
	if err != nil {
		return "", false, err
	}
	force := snapMode == backup.SnapAlways
	disabled := snapMode == backup.SnapNever
	need := force
	st, err := backup.ParseSnapTime(snapTime)
	if err != nil {
		return "", false, err
	}
	ss, err := backup.ParseSnapSize(snapSize)
	if err != nil {
		return "", false, err
	}
	if !force && !disabled && (st > 0 || ss > 0) {
		need = backup.ShouldSnapshotWithThresholds(backup.SnapIfNeeded, backup.ViewsFromMatch(pairs), st, ss) != ""
	}
	for _, pair := range pairs {
		if pair == nil || pair.SrcName == "" {
			continue
		}
		if st > 0 || ss > 0 {
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
