package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/report"
)

func runMatch(args []string) int {
	p, err := opt.Parse("match", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) // oracle stop() prefix
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		matchUsage()
		return 0
	}

	dryRun := p.Env.Bool("DRYRUN", false)

	if dryRun {
		if len(p.Operands) < 1 || len(p.Operands) > 2 {
			matchUsage()
			return 2
		}
		depth, code := depthFrom(p.Env, "match")
		if code != 0 {
			return code
		}
		props := match.DefaultListProps
		noWritten := p.Env.Get("WRITTEN") == "0" && !p.Changed["LIST_WRITTEN"]
		if noWritten {
			props = match.MinimalListProps
		}
		for _, op := range p.Operands {
			fmt.Fprintln(os.Stdout, formatListCmd(op, props, depth))
		}
		return 0
	}

	if len(p.Operands) != 2 {
		matchUsage()
		return 2
	}
	depth, code := depthFrom(p.Env, "match")
	if code != 0 {
		return code
	}

	var cols []string
	if pl := p.Env.Get("PROPLIST"); pl != "" {
		cols, err = report.ExpandProplist(pl)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta match: %v\n", err)
			return 2
		}
	}

	// LIST_WRITTEN default on; --no-written (WRITTEN=0) disables unless
	// --written was explicitly given (oracle asymmetric-key quirk).
	noWritten := p.Env.Get("WRITTEN") == "0" && !p.Changed["LIST_WRITTEN"]
	res, err := match.Compare(context.Background(), newReal(), match.Request{
		Source:    p.Operands[0],
		Target:    p.Operands[1],
		Cols:      cols,
		Depth:     depth,
		Include:   p.Env.List("INCLUDE"),
		Exclude:   p.Env.List("EXCLUDE"),
		Scripting: p.Env.Bool("SCRIPTING_MODE", false),
		Parsable:  p.Env.Bool("PARSABLE", false),
		NoWritten: noWritten,
		CheckTime: p.Env.Bool("CHECK_TIME", false),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta match: %v\n", err)
		return 1
	}
	printWarns(res.Warnings)
	fmt.Print(res.Output)
	return 0
}

// formatListCmd builds the oracle dry-run "+ zfs list ..." line for an endpoint.
func formatListCmd(endpoint string, props []string, depth int) string {
	var b strings.Builder
	b.WriteString("+ zfs list -H -t snapshot -o ")
	b.WriteString(strings.Join(props, ","))
	if depth > 0 {
		fmt.Fprintf(&b, " -r -d %d", depth)
	}
	b.WriteString(" ")
	b.WriteString(endpoint)
	return b.String()
}

func matchUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta match [-Hp] [-d depth] [-o field[,...]] [-X pat] [--include pat] [--written|--no-written] [--time] SOURCE TARGET")
}
