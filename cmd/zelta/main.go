package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bell-tower/zelta-go/internal/conf"
)

const version = "Zelta 1.2.0 (Go)"

func main() {
	// argv[0] dispatch: zprune binary/symlink acts as "zelta zprune"
	if len(os.Args) > 0 {
		switch filepath.Base(os.Args[0]) {
		case "zprune":
			args := []string{os.Args[0], "zprune"}
			args = append(args, os.Args[1:]...)
			os.Args = args
		default:
			// Accept "zprune-*" variants (zprune-freebsd, zprune-linux, …)
			if bn := filepath.Base(os.Args[0]); strings.HasPrefix(bn, "zprune") && bn != "zprune" {
				args := []string{os.Args[0], "zprune"}
				args = append(args, os.Args[1:]...)
				os.Args = args
			}
		}
	}

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
	case "-h", "-?":
		usage()
	case "help", "--help":
		os.Exit(commandHelp(os.Args[2:]))
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
	case "snapshot":
		os.Exit(runSnapshot(os.Args[2:]))
	case "policy", "zp":
		os.Exit(runPolicy(os.Args[2:]))
	case "zprune":
		os.Exit(runZprune(os.Args[2:]))
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

// commandHelp routes zelta help to man pages.
// topicless → zelta(8); "options" → zelta-options(7); any verb → zelta-VERB(8).
func commandHelp(args []string) int {
	section := "8"
	page := "zelta"
	if len(args) > 0 {
		switch args[0] {
		case "options":
			page = "zelta-options"
			section = "7"
		case "zprune":
			page = "zprune"
		default:
			page = "zelta-" + args[0]
		}
	}

	// Embedded man pages — always available, no filesystem dependency.
	if content, err := manPages.ReadFile("doc/man" + section + "/" + page + "." + section); err == nil {
		tmpDir, err := os.MkdirTemp("", "zelta-man-*")
		if err == nil {
			defer os.RemoveAll(tmpDir)
			manDir := filepath.Join(tmpDir, "man"+section)
			if err := os.MkdirAll(manDir, 0755); err == nil {
				if err := os.WriteFile(filepath.Join(manDir, page+"."+section), content, 0644); err == nil {
					cmd := exec.Command("man", "-M", tmpDir, section, page)
					cmd.Stdout = os.Stdout
					cmd.Stderr = os.Stderr
					if cmd.Run() == nil {
						return 0
					}
				}
			}
		}
	}

	// System man fallback
	docDir := conf.DocDir()
	var cmd *exec.Cmd
	if docDir != "" {
		if st, err := os.Stat(docDir); err == nil && st.IsDir() {
			cmd = exec.Command("man", "-M", docDir, section, page)
		}
	}
	if cmd == nil {
		cmd = exec.Command("man", section, page)
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if len(args) > 0 {
			fmt.Fprintf(os.Stderr, "zelta %s — see https://zelta.space\n", args[0])
			return 1
		}
		usage()
	}
	return 0
}

func usage() {
	exec := os.Stderr
	fmt.Fprintf(exec, "usage: zelta command [OPTIONS]\n\n")
	fmt.Fprintf(exec, "Where 'command' is one of the following:\n\n")
	for _, c := range []struct {
		name, desc string
	}{
		{"match", "Compare dataset trees"},
		{"backup", "Sync ZFS datasets"},
		{"policy", "Run configured replication jobs"},
		{"clone", "Clone ZFS datasets"},
		{"revert", "Rename and clone a dataset in-place"},
		{"rotate", "Recover sync continuity"},
		{"snapshot", "Create a recursive snapshot"},
		{"prune", "Report snapshot prune candidates (read-only)"},
		{"lock", "Make datasets read-only and unmount them"},
		{"unlock", "Inherit readonly and mount datasets"},
		{"failover", "Promote a target dataset tree"},
		{"propsync", "Sync local ZFS properties between trees"},
		{"", ""},
		{"version", "Show version information"},
	} {
		if c.name == "" {
			fmt.Fprintln(exec)
			continue
		}
		fmt.Fprintf(exec, "  %-8s   %s\n", c.name, c.desc)
	}
	fmt.Fprintf(exec, "\nEach endpoint is in the form: [user@host:]pool[/dataset/][@snap]\n")
	fmt.Fprintf(exec, "\nFor complete documentation:  zelta help\n")
	fmt.Fprintf(exec, "                             zelta help [<topic>]\n")
	fmt.Fprintf(exec, "                             zelta help options\n")
}
