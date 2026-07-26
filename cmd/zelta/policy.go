package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/conf"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/policy"
)

func runPolicy(args []string) int {
	p, err := opt.Parse("policy", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		policyUsage()
		return 0
	}

	cfg := p.Env.Get("CONFIG")
	if cfg == "" {
		cfg = conf.ConfigPath()
	}

	// Conf must not clobber CLI. Process ZELTA_* also wins over conf
	// (00-contracts). Built-in defaults and zelta.env stay below conf.
	override := map[string]string{}
	for k := range p.Changed {
		override[k] = p.Env.Get(k)
	}
	for _, kv := range os.Environ() {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" {
			continue
		}
		bare, ok := strings.CutPrefix(k, "ZELTA_")
		if !ok {
			continue
		}
		if _, cli := p.Changed[bare]; cli {
			continue
		}
		// Skip keys injectEnvFile may have filled from zelta.env so conf
		// still beats env-file (contracts). Heuristic: only honor process
		// env for keys that are not solely built-in defaults and that look
		// like explicit operator exports — we treat any non-default-shaped
		// presence as override. Practically, honor all process env except
		// the built-in default keys when their value equals the default.
		if def, isDef := builtInDefaults[bare]; isDef && v == def {
			continue
		}
		override[bare] = v
	}

	jobs, warns, err := policy.Load(cfg, override)
	printWarns(warns)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}

	filtered := policy.Filter(jobs, p.Operands)
	if len(filtered) == 0 {
		if len(p.Operands) > 0 {
			fmt.Fprintf(os.Stderr, "error: policy object(s) not found: %s\n", strings.Join(p.Operands, ", "))
			return 1
		}
		fmt.Fprintf(os.Stderr, "error: no datasets defined in %s\n", cfg)
		return 1
	}

	if !p.Env.Bool("DRYRUN", false) {
		fmt.Fprintf(os.Stderr, "error: policy execution not implemented; use -n/--dryrun\n")
		return 2
	}

	logLevel, _ := strconv.Atoi(p.Env.Get("LOG_LEVEL"))
	if logLevel >= 3 {
		fmt.Print(policy.FormatCommands(filtered))
	} else {
		fmt.Print(policy.FormatTable(filtered, p.Env.Bool("NO_HEADER", false)))
	}
	return 0
}

var builtInDefaults = map[string]string{
	"LOG_LEVEL":       "2",
	"RESUME":          "1",
	"SNAP_MODE":       "IF_NEEDED",
	"SYNC_DIRECTION":  "PULL",
	"SEND_INTR":       "1",
	"SEND_DEFAULT":    "-L -c -e",
	"SEND_DECRYPTED":  "-L -c",
	"SEND_RAW":        "--raw",
	"SEND_NEW":        "-p",
	"RECV_TOP":        "-o readonly=on",
	"RECV_FS":         "-u -x mountpoint -o canmount=noauto",
	"RECV_PARTIAL":    "-s",
	"BOOKMARK_MODE":   "0",
	"BOOKMARK_PREFIX": "{targethost}_",
	"CREATE_PARENT":   "1",
	"LIST_WRITTEN":    "1",
	"REMOTE_COMMAND":  "ssh",
}

func policyUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta policy [-n] [-H] [-C file] [site|host|dataset] ...")
}
