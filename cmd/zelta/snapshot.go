package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/bell-tower/zelta-go/backup"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/internal/opt"
	"github.com/bell-tower/zelta-go/internal/zlog"
)

func runSnapshot(args []string) int {
	p, err := opt.Parse("snapshot", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := newLogSink(p)
	defer sink.Close()
	printWarns(sink, p.Warnings)
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
		// Oracle: dry-run "+ …" line is LOG_NOTICE.
		if sink.Enabled(zlog.Notice) {
			fmt.Printf("+ zfs snapshot -r %s\n", dsSnap)
		}
		return 0
	}
	if err := exec.Snapshot(context.Background(), p.Operands[0], dsSnap, true); err != nil {
		fmt.Fprintf(os.Stderr, "zelta snapshot: error creating '%s': %v\n", dsSnap, err)
		return 1
	}
	// Oracle LOG_NOTICE (snapshotting feedback).
	sink.Notice(fmt.Sprintf("snapshot created '%s'", dsSnap))
	return 0
}

func snapshotUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta snapshot [OPTIONS] ENDPOINT")
	fmt.Fprintln(os.Stderr, "Create a recursive snapshot on ENDPOINT.")
}
