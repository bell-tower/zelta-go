package main

import (
	"context"
	"fmt"
	"os"

	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/internal/opt"
	"github.com/bell-tower/zelta-go/internal/report"
	"github.com/bell-tower/zelta-go/internal/zlog"
	"github.com/bell-tower/zelta-go/match"
)

func runMatch(args []string) int {
	p, err := opt.Parse("match", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) // oracle stop() prefix
		return 1
	}
	sink := newLogSink(p)
	defer sink.Close()
	printWarns(sink, p.Warnings)
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
		req := match.Request{Depth: depth}
		noWritten := p.Env.Get("WRITTEN") == "0" && !p.Changed["LIST_WRITTEN"]
		if noWritten {
			req.Props = match.MinimalListProps
		}
		for _, op := range p.Operands {
			ep, err := endpoint.Parse(op)
			if err != nil {
				fmt.Fprintf(os.Stderr, "zelta match: %v\n", err)
				return 2
			}
			if req.Source.Dataset == "" {
				req.Source = ep
			} else {
				req.Target = ep
			}
		}
		cmds, err := match.Commands(req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta match: %v\n", err)
			return 2
		}
		if sink.Enabled(zlog.Notice) {
			for _, c := range cmds {
				line, err := c.ShellLine()
				if err != nil {
					fmt.Fprintf(os.Stderr, "zelta match: %v\n", err)
					return 2
				}
				fmt.Fprintln(os.Stdout, "+ "+line)
			}
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
	src, err := parseEndpoint(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: source: %v\n", err)
		return 1
	}
	tgt, err := parseEndpoint(p.Operands[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: target: %v\n", err)
		return 1
	}
	exec := newReal()
	wireCommandEcho(exec, sink)
	scripting := p.Env.Bool("SCRIPTING_MODE", false)
	parsable := p.Env.Bool("PARSABLE", false)
	checkTime := p.Env.Bool("CHECK_TIME", false)
	res, err := match.Compare(context.Background(), exec, match.Request{
		Source:    src,
		Target:    tgt,
		Cols:      cols,
		Depth:     depth,
		Include:   p.Env.List("INCLUDE"),
		Exclude:   p.Env.List("EXCLUDE"),
		NoWritten: noWritten,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta match: %v\n", err)
		return 1
	}
	printWarns(sink, res.Warnings)
	// Oracle: the whole table is LOG_NOTICE — `-q` silences it.
	out := report.FormatMatch(res.Pairs, report.MatchFormatOpts{
		Cols:        cols,
		SrcLeaf:     report.DatasetLeaf(src.Dataset),
		Scripting:   scripting,
		Parsable:    parsable,
		CheckTime:   checkTime,
		SrcListTime: res.SrcListTime,
		TgtListTime: res.TgtListTime,
	})
	emitBlob(sink, out)
	return 0
}

func matchUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta match [-Hp] [-d depth] [-o field[,...]] [-X pat] [--include pat] [--written|--no-written] [--time] SOURCE TARGET")
}
