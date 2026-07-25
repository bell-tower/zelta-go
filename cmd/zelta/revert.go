package main

import (
	"context"
	"fmt"
	"os"

	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/lineage"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

func runRevert(args []string) int {
	p, err := opt.Parse("revert", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		revertUsage()
		return 0
	}
	if len(p.Operands) != 1 {
		revertUsage()
		return 2
	}
	steps, err := lineage.Revert(lineage.RevertRequest{Endpoint: p.Operands[0]})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: %v\n", err)
		return 1
	}
	if p.Env.Bool("DRYRUN", false) {
		fmt.Print(lineage.Format(steps))
		return 0
	}
	ep, _ := endpoint.Parse(p.Operands[0])
	exec := &zfs.Real{}
	if err := exec.Rename(context.Background(), ep.String(), ep.Dataset, ep.Dataset+"_"+ep.Snapshot); err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: %v\n", err)
		return 1
	}
	if err := exec.Clone(context.Background(), ep.String(), ep.Dataset+"@"+ep.Snapshot, ep.Dataset); err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: %v\n", err)
		return 1
	}
	return 0
}

func revertUsage() { fmt.Fprintln(os.Stderr, "usage: zelta revert [-n] DATASET@SNAPSHOT") }
