package main

import (
	"context"
	"fmt"
	"os"
	"strconv"

	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/prune"
)

func runPrune(args []string) int {
	p, err := opt.Parse("prune", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) // oracle stop() prefix
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		pruneUsage()
		return 0
	}
	if len(p.Operands) < 1 || len(p.Operands) > 2 {
		pruneUsage()
		return 2
	}
	depth, code := depthFrom(p.Env, "prune")
	if code != 0 {
		return code
	}

	pruneNum := -1
	if s := p.Env.Get("PRUNE_NUM"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			fmt.Fprintf(os.Stderr, "zelta prune: prune-num of '%s' invalid; must be non-negative\n", s)
			return 1
		}
		pruneNum = n
	}

	// Optional second operand is the guard/match endpoint (oracle CLI form).
	guardStr := p.Env.Get("MATCH_ENDPOINT")
	if len(p.Operands) == 2 && guardStr == "" {
		guardStr = p.Operands[1]
	}

	pruneGuard, err := prune.ParsePruneGuard(p.Env.Get("PRUNE_GUARD"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta prune: %v\n", err)
		return 1
	}
	pruneTime, err := prune.ParsePruneTime(p.Env.Get("PRUNE_TIME"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta prune: %v\n", err)
		return 1
	}
	src, err := parseEndpoint(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: source: %v\n", err)
		return 1
	}
	guard, err := parseEndpoint(guardStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: match-endpoint: %v\n", err)
		return 1
	}
	res, err := prune.Run(context.Background(), newReal(), prune.Request{
		Source:      src,
		GuardTarget: guard,
		PruneGuard:  pruneGuard,
		PruneNum:    pruneNum,
		PruneTime:   pruneTime,
		PruneGrid:   p.Env.Get("PRUNE_GRID"),
		PruneSize:   p.Env.Get("PRUNE_SIZE"),
		Depth:       depth,
		Include:     p.Env.List("INCLUDE"),
		Exclude:     p.Env.List("EXCLUDE"),
		NoRanges:    p.Env.Bool("NO_RANGES", false),
		Visual:      p.Env.Bool("PRUNE_VISUAL", false),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta prune: %v\n", err)
		return 1
	}
	printWarns(res.Warnings)
	if n, _ := strconv.Atoi(p.Env.Get("LOG_LEVEL")); n >= 3 && res.Keeping != "" {
		fmt.Fprintf(os.Stderr, "keeping: %s\n", res.Keeping)
	}
	fmt.Print(res.Output)
	return 0
}

func pruneUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta prune [OPTIONS] ENDPOINT [MATCH_ENDPOINT]")
	fmt.Fprintln(os.Stderr, "Reports snapshot prune candidates on ENDPOINT.")
	fmt.Fprintln(os.Stderr, "Optional MATCH_ENDPOINT guards against unsynced snapshots.")
}
