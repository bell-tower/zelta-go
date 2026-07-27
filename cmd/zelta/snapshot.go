package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/opt"
)

func runSnapshot(args []string) int {
	p, err := opt.Parse("snapshot", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		snapshotUsage()
		return 0
	}
	if len(p.Operands) != 1 {
		snapshotUsage()
		return 2
	}

	ep, err := endpoint.Parse(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta snapshot: %v\n", err)
		return 1
	}
	name := strings.TrimPrefix(p.Env.Get("SNAP_NAME"), "@")
	if name == "" {
		name = backup.DefaultSnapName()
	}
	dsSnap := ep.Dataset + "@" + name
	exec := newReal()
	if p.Env.Bool("DRYRUN", false) {
		fmt.Printf("+ zfs snapshot -r %s\n", dsSnap)
		return 0
	}
	if err := exec.Snapshot(context.Background(), p.Operands[0], dsSnap, true); err != nil {
		fmt.Fprintf(os.Stderr, "zelta snapshot: error creating '%s': %v\n", dsSnap, err)
		return 1
	}
	fmt.Printf("snapshot created '%s'\n", dsSnap)
	return 0
}

func snapshotUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta snapshot [OPTIONS] ENDPOINT")
	fmt.Fprintln(os.Stderr, "Create a recursive snapshot on ENDPOINT.")
}
