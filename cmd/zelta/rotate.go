package main

import (
	"context"
	"fmt"
	"os"

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
	request := rotate.TreeRequest{
		Source: p.Operands[0], Target: p.Operands[1], Pairs: m.Pairs,
		TargetRows: m.TgtRows, Intermediate: p.Env.Bool("SEND_INTR", true),
		Flags: opt.SendRecvFrom(p.Env),
	}
	steps, err := rotate.PlanTree(request)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	if len(steps) == 0 || len(steps[0].Argv) == 0 {
		fmt.Fprintln(os.Stderr, "zelta rotate: empty preservation plan")
		return 1
	}
	preserved := steps[0].Argv[len(steps[0].Argv)-1]
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
	fmt.Print(rotate.Format(steps))
	return 0
}

func rotateUsage() { fmt.Fprintln(os.Stderr, "usage: zelta rotate SOURCE TARGET") }
