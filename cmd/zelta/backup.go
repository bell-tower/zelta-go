package main

import (
	"context"
	"fmt"
	"os"

	"git.belltower.it/djbell/zelta-go/internal/backup"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

func runBackup(args []string) int {
	p, err := opt.Parse("backup", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) // oracle stop() prefix
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		backupUsage()
		return 0
	}
	if len(p.Operands) != 2 {
		backupUsage()
		return 2
	}
	depth, code := depthFrom(p.Env, "backup")
	if code != 0 {
		return code
	}

	snapMode := backup.SnapIfNeeded
	switch p.Env.Get("SNAP_MODE") {
	case "0":
		snapMode = backup.SnapNever
	case "ALWAYS":
		snapMode = backup.SnapAlways
	}
	flags := opt.SendRecvFrom(p.Env)
	createParent := p.Env.Bool("CREATE_PARENT", true)

	res, err := backup.Run(context.Background(), &zfs.Real{}, backup.Request{
		Source:        p.Operands[0],
		Target:        p.Operands[1],
		TargetOrigin:  p.Env.Get("ORIGIN_ID"),
		DryRun:        p.Env.Bool("DRYRUN", false),
		Intermediate:  p.Env.Bool("SEND_INTR", true),
		SnapMode:      snapMode,
		SnapTime:      p.Env.Get("SNAP_TIME"),
		SnapSize:      p.Env.Get("SNAP_SIZE"),
		Depth:         depth,
		Include:       p.Env.List("INCLUDE"),
		Exclude:       p.Env.List("EXCLUDE"),
		SyncDirection: p.Env.Get("SYNC_DIRECTION"),
		Flags:         &flags,
		CreateParent:  &createParent,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta backup: %v\n", err)
		return 1
	}
	printWarns(res.Warnings)
	fmt.Print(res.Output)
	for _, msg := range res.Errors {
		fmt.Fprintf(os.Stderr, "zelta backup: %s\n", msg)
	}
	if len(res.Errors) > 0 {
		return 1
	}
	return 0
}

func backupUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta backup [-n] [-Ii] [--snapshot|--no-snapshot] [--target-origin ENDPOINT] [--push|--pull|--no-pull] [-d depth] [-X pat] SOURCE TARGET")
}
