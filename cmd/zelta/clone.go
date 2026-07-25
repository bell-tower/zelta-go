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
	if len(p.Operands) != 2 {
		cloneUsage()
		return 2
	}
	steps, err := lineage.Clone(lineage.CloneRequest{Source: p.Operands[0], Target: p.Operands[1]})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: %v\n", err)
		return 1
	}
	if p.Env.Bool("DRYRUN", false) {
		fmt.Print(lineage.Format(steps))
		return 0
	}
	src, _ := endpoint.Parse(p.Operands[0])
	tgt, _ := endpoint.Parse(p.Operands[1])
	if err := (&zfs.Real{}).Clone(context.Background(), tgt.String(), src.Dataset+"@"+src.Snapshot, tgt.Dataset); err != nil {
		fmt.Fprintf(os.Stderr, "zelta clone: %v\n", err)
		return 1
	}
	return 0
}

func cloneUsage() { fmt.Fprintln(os.Stderr, "usage: zelta clone [-n] SOURCE@SNAPSHOT TARGET") }
