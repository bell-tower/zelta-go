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
	m, err := match.Compare(context.Background(), &zfs.Real{}, match.Request{
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
	originVerified := false
	if root.Match == "" && root.SrcOrigin != "" {
		if _, snap, ok := splitOriginForCLI(root.SrcOrigin); ok {
			originVerified = hasSnapshot(m.TgtRows, m.Target.Dataset+snap)
		}
	}
	steps, err := rotate.Plan(rotate.Request{
		Source: p.Operands[0], Target: p.Operands[1], Match: root.Match,
		SourceLast: root.SrcLast, TargetLast: root.TgtLast,
		SourceOrigin: root.SrcOrigin, OriginVerified: originVerified,
		SourceType: root.SrcType, Intermediate: p.Env.Bool("SEND_INTR", true),
		Flags: opt.SendRecvFrom(p.Env),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta rotate: %v\n", err)
		return 1
	}
	fmt.Print(rotate.Format(steps))
	return 0
}

func rotateUsage() { fmt.Fprintln(os.Stderr, "usage: zelta rotate SOURCE TARGET") }

func splitOriginForCLI(origin string) (string, string, bool) {
	for i := len(origin) - 1; i > 0; i-- {
		if origin[i] == '@' {
			return origin[:i], origin[i:], i < len(origin)-1
		}
	}
	return "", "", false
}

func hasSnapshot(rows []zfs.ListRow, name string) bool {
	for _, row := range rows {
		if row.Name == name {
			return true
		}
	}
	return false
}
