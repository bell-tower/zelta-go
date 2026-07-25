package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/backup"
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
	depth, code := depthFrom(p.Env, "revert")
	if code != 0 {
		return code
	}
	exec := &zfs.Real{}
	ep, err := endpoint.Parse(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: %v\n", err)
		return 1
	}
	endpointArg := p.Operands[0]
	if ep.Snapshot == "" && p.Env.Get("SNAP_NAME") != "" {
		endpointArg += "@" + strings.TrimPrefix(p.Env.Get("SNAP_NAME"), "@")
		ep, err = endpoint.Parse(endpointArg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta revert: %v\n", err)
			return 1
		}
	}
	current := ep
	current.Snapshot = ""
	rows, err := exec.List(context.Background(), endpointArg, ep.Dataset, []string{"name", "type"}, depth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: list source: %v\n", err)
		return 1
	}
	parsedRows, err := zfs.ParseListLines(rows, []string{"name", "type"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: parse source: %v\n", err)
		return 1
	}
	request := lineage.RevertRequest{Endpoint: endpointArg, Depth: depth, AfterSnapshot: "@" + backup.DefaultSnapName()}
	steps, err := lineage.RevertPlan(request, parsedRows, false)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: %v\n", err)
		return 1
	}
	preserved := steps[0].Argv[len(steps[0].Argv)-1]
	exists, err := exec.Exists(context.Background(), current.String(), preserved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: preservation check: %v\n", err)
		return 1
	}
	if exists {
		fmt.Fprintf(os.Stderr, "zelta revert: preservation target already exists: %s\n", preserved)
		return 1
	}
	if p.Env.Bool("DRYRUN", false) {
		fmt.Print(lineage.Format(steps))
		return 0
	}
	result, err := lineage.Apply(context.Background(), exec, current.String(), steps)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta revert: %v\n", err)
		return 1
	}
	for _, failure := range result.Failures {
		if failure.DSSuffix == "" {
			fmt.Fprintf(os.Stderr, "zelta revert: %s: %v\n", failure.Kind, failure.Err)
		} else {
			fmt.Fprintf(os.Stderr, "zelta revert: %s %s: %v\n", failure.Kind, failure.DSSuffix, failure.Err)
		}
	}
	if len(result.Failures) > 0 {
		if result.Preserved {
			fmt.Fprintf(os.Stderr, "zelta revert: preserved tree remains at %s; incomplete children require manual recovery\n", preserved)
		}
		return 1
	}
	fmt.Fprintf(os.Stdout, "to retain replica history, run: zelta rotate '%s' 'TARGET'\n", current.String())
	return 0
}

func revertUsage() { fmt.Fprintln(os.Stderr, "usage: zelta revert [-n] DATASET[@SNAPSHOT]") }
