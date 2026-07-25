package main

import (
	"fmt"
	"os"
)

const version = "zelta-go 0.0.1-dev"

func main() {
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
		fmt.Fprintln(os.Stderr, "zelta match: not implemented yet (scaffold; see agents/03-match.md)")
		os.Exit(2)
	default:
		fmt.Fprintf(os.Stderr, "unrecognized command: %q\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintf(os.Stderr, `usage: zelta command [OPTIONS]

Commands:
  match       Compare dataset trees (WIP)
  version     Show version

Private experimental Go port. Docs: ~/Code/zelta/doc/ and AGENTS.md
`)
}
