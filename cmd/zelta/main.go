package main

import (
	"fmt"
	"os"

	"git.belltower.it/djbell/zelta-go/internal/conf"
)

const version = "zelta-go 0.0.1-dev"

func main() {
	// Oracle := semantics: zelta.env sets only unset-or-empty ZELTA_* vars,
	// so process env always wins. Injection keeps every opt.Lookup path
	// (library defaults included) file-aware.
	for _, w := range injectEnvFile() {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}
	switch os.Args[1] {
	case "version", "--version", "-V":
		fmt.Println(version)
	case "help", "--help", "-h":
		usage()
	case "match":
		os.Exit(runMatch(os.Args[2:]))
	case "backup":
		os.Exit(runBackup(os.Args[2:]))
	case "prune":
		os.Exit(runPrune(os.Args[2:]))
	case "clone":
		os.Exit(runClone(os.Args[2:]))
	case "revert":
		os.Exit(runRevert(os.Args[2:]))
	case "rotate":
		os.Exit(runRotate(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "unrecognized command: %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

// injectEnvFile loads zelta.env (conf.EnvPath) into the process environment.
func injectEnvFile() []string {
	vals, warns, err := conf.LoadEnvFile(conf.EnvPath())
	if err != nil {
		return append(warns, err.Error())
	}
	for k, v := range vals {
		if os.Getenv("ZELTA_"+k) == "" { // empty export counts as unset
			os.Setenv("ZELTA_"+k, v)
		}
	}
	return warns
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: zelta command [OPTIONS]

Commands:
  match       Compare dataset trees
  backup      Sync dataset trees (snap-if-needed; -n dry-run)
  prune       Report snapshot prune candidates (read-only)
  clone       Clone an explicit snapshot (use -n to print commands)
  revert      Preserve current state and clone an explicit snapshot back
  rotate      Preserve a divergent target and print the safe receive plan
  version     Show version

Private experimental Go port. Docs: ~/Code/zelta/doc/ and AGENTS.md
`)
}
