package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/lineage"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func runClone(args []string) int {
	p, err := opt.Parse("clone", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		cloneUsage()
		return 0
	}
	if len(p.Operands) == 4 {
		return runCloneAndBackup(p)
	}
	if len(p.Operands) != 2 {
		cloneUsage()
		return 2
	}
	return runCloneParsed(p)
}

func runCloneParsed(p *opt.Parsed) int {
	depth, code := depthFrom(p.Env, "clone")
	if code != 0 {
		return code
	}
	exec := newReal()
	src, err := endpoint.Parse(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: source: %v\n", err)
		return 1
	}
	sourceArg := p.Operands[0]
	if src.Snapshot == "" && p.Env.Get("SNAP_NAME") != "" {
		sourceArg += "@" + strings.TrimPrefix(p.Env.Get("SNAP_NAME"), "@")
		src, err = endpoint.Parse(sourceArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta clone: source: %v\n", err)
			return 1
		}
	}
	tgt, err := endpoint.Parse(p.Operands[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: target: %v\n", err)
		return 1
	}
	rows, err := exec.List(context.Background(), sourceArg, src.Dataset, []string{"name", "type"}, depth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: list source: %v\n", err)
		return 1
	}
	parsedRows, err := zfs.ParseListLines(rows, []string{"name", "type"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: parse source: %v\n", err)
		return 1
	}
	exists, err := exec.Exists(context.Background(), p.Operands[1], tgt.Dataset)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: target check: %v\n", err)
		return 1
	}
	steps, err := lineage.ClonePlan(lineage.CloneRequest{Source: sourceArg, Target: p.Operands[1], Depth: depth}, parsedRows, exists)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: %v\n", err)
		return 1
	}
	if p.Env.Bool("DRYRUN", false) {
		out, err := lineage.FormatRemote(steps, p.Operands[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta clone: %v\n", err)
			return 1
		}
		fmt.Print(out)
		return 0
	}
	for _, step := range steps {
		if len(step.Argv) < 2 {
			continue
		}
		if err := exec.Clone(context.Background(), tgt.String(), step.Argv[len(step.Argv)-2], step.Argv[len(step.Argv)-1]); err != nil {
			fmt.Fprintf(os.Stderr, "zelta clone: %v\n", err)
			return 1
		}
	}
	return 0
}

func runCloneAndBackup(p *opt.Parsed) int {
	depth, code := depthFrom(p.Env, "clone")
	if code != 0 {
		return code
	}
	cloneParsed := *p
	cloneParsed.Operands = p.Operands[:2]
	if code := runCloneParsed(&cloneParsed); code != 0 {
		return code
	}

	snapTime, err := backup.ParseSnapTime(p.Env.Get("SNAP_TIME"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	snapSize, err := backup.ParseSnapSize(p.Env.Get("SNAP_SIZE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	flags := opt.SendRecvFrom(p.Env)
	createParent := p.Env.Bool("CREATE_PARENT", true)
	src, err := parseEndpoint(p.Operands[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: source: %v\n", err)
		return 1
	}
	origin, err := parseEndpoint(p.Operands[2])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: target-origin: %v\n", err)
		return 1
	}
	tgt, err := parseEndpoint(p.Operands[3])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: target: %v\n", err)
		return 1
	}
	res, err := backup.Run(context.Background(), newReal(), backup.Request{
		Source:        src,
		Target:        tgt,
		TargetOrigin:  origin,
		DryRun:        p.Env.Bool("DRYRUN", false),
		Intermediate:  p.Env.Bool("SEND_INTR", true),
		SnapMode:      backup.ParseSnapMode(p.Env.Get("SNAP_MODE")),
		SnapTime:      snapTime,
		SnapSize:      snapSize,
		Depth:         depth,
		Include:       p.Env.List("INCLUDE"),
		Exclude:       p.Env.List("EXCLUDE"),
		SyncDirection: backup.ParseSyncDirection(p.Env.Get("SYNC_DIRECTION")),
		Flags:         &flags,
		CreateParent:  &createParent,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: backup: %v\n", err)
		return 1
	}
	printWarns(res.Warnings)
	fmt.Print(res.Output)
	for _, msg := range res.Errors {
		fmt.Fprintf(os.Stderr, "zelta clone: backup: %s\n", msg)
	}
	if len(res.Errors) > 0 {
		return 1
	}
	return 0
}

func cloneUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta clone [-n] SOURCE[@SNAPSHOT] TARGET [ORIGIN-BACKUP TARGET-BACKUP]")
}
