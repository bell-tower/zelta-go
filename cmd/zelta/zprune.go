package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/prune"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func runZprune(args []string) int {
	p, err := opt.Parse("prune", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		zpruneUsage()
		return 0
	}
	if len(p.Operands) != 1 {
		zpruneUsage()
		return 2
	}
	depth, code := depthFrom(p.Env, "zprune")
	if code != 0 {
		return code
	}

	dryRun := p.Env.Bool("DRYRUN", false)
	force := p.Env.Bool("PRUNE_FORCE", false)

	pruneNum := -1
	if s := p.Env.Get("PRUNE_NUM"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 0 {
			fmt.Fprintf(os.Stderr, "zprune: prune-num of '%s' invalid; must be non-negative\n", s)
			return 1
		}
		pruneNum = n
	}

	pruneGuard, err := prune.ParsePruneGuard(p.Env.Get("PRUNE_GUARD"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "zprune: %v\n", err)
		return 1
	}
	pruneTime, err := prune.ParsePruneTime(p.Env.Get("PRUNE_TIME"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "zprune: %v\n", err)
		return 1
	}
	src, err := parseEndpoint(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: source: %v\n", err)
		return 1
	}
	guard, err := parseEndpoint(p.Env.Get("MATCH_ENDPOINT"))
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
		fmt.Fprintf(os.Stderr, "zprune: %v\n", err)
		return 1
	}
	printWarns(res.Warnings)
	fmt.Print(res.Output)

	candidates := res.Candidates()
	if len(candidates) == 0 {
		return 0
	}

	if dryRun {
		return 0
	}

	// Preview and confirm
	if !confirmDestruction(candidates, force) {
		return 1
	}

	srcEp, err := endpoint.Parse(p.Operands[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "zprune: source: %v\n", err)
		return 1
	}
	return destroyCandidates(srcEp, candidates)
}

func confirmDestruction(candidates []string, force bool) bool {
	fmt.Fprintf(os.Stderr, "\nzprune: will destroy %d snapshot(s)\n", len(candidates))
	if force {
		return true
	}
	fmt.Fprintf(os.Stderr, "Destroy now? [y/N] ")
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return false
	}
	answer := strings.ToLower(strings.TrimSpace(scanner.Text()))
	if answer != "y" && answer != "yes" {
		fmt.Fprintln(os.Stderr, "Aborted.")
		return false
	}
	return true
}

// destroyCandidates runs zfs destroy grouped by dataset. srcEp supplies the
// transport host (list names are bare dataset paths, not user@host:ds).
func destroyCandidates(srcEp endpoint.Endpoint, candidates []string) int {
	groups := groupByDataset(candidates)
	failed := false
	for ds, snaps := range groups {
		// zfs destroy ds@snap1,snap2 (comma form; space-separated full names fail)
		target := destroyTarget(ds, snaps)
		if target == "" {
			continue
		}
		if runDestroyCmd(srcEp, []string{"destroy", target}) != 0 {
			failed = true
		}
	}
	if failed {
		return 1
	}
	return 0
}

func groupByDataset(candidates []string) map[string][]string {
	groups := make(map[string][]string)
	for _, c := range candidates {
		i := strings.IndexByte(c, '@')
		if i < 0 {
			continue
		}
		ds := c[:i]
		groups[ds] = append(groups[ds], c[i:])
	}
	return groups
}

// destroyTarget builds "ds@snap1,snap2" for one zfs destroy invocation.
func destroyTarget(ds string, snaps []string) string {
	if len(snaps) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(ds)
	b.WriteString(snaps[0])
	for _, s := range snaps[1:] {
		b.WriteByte(',')
		b.WriteString(strings.TrimPrefix(s, "@"))
	}
	return b.String()
}

func runDestroyCmd(ep endpoint.Endpoint, args []string) int {
	if !ep.Remote || ep.Host == "" || ep.Host == "localhost" {
		fmt.Fprintf(os.Stderr, "+ zfs %s\n", strings.Join(args, " "))
		cmd := exec.Command("zfs", args...)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "zprune: destroy command failed: zfs %s\n", strings.Join(args, " "))
			return 1
		}
		return 0
	}
	host := ep.Host
	if ep.User != "" {
		host = ep.User + "@" + ep.Host
	}
	remoteCmd := "zfs " + strings.Join(args, " ")
	argv, err := remoteFromEnv().Argv(host, remoteCmd, zfs.RoleDefault)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zprune: remote wrap: %v\n", err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "+ %s\n", strings.Join(argv, " "))
	cmd := exec.Command(argv[0], argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zprune: destroy command failed: %s\n", strings.Join(argv, " "))
		return 1
	}
	return 0
}

func zpruneUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta zprune [OPTIONS] ENDPOINT")
	fmt.Fprintln(os.Stderr, "  or:  zprune [OPTIONS] ENDPOINT")
	fmt.Fprintln(os.Stderr, "Analyzes and destroys prune candidates on ENDPOINT.")
	fmt.Fprintln(os.Stderr, "Options: --dryrun, --force, --prune-num, --prune-time, --prune-grid, --prune-guard, --match-endpoint")
}
