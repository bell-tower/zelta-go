package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/opt"
)

func runBackup(args []string) int {
	p, err := opt.Parse("backup", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) // oracle stop() prefix
		return 1
	}
	printWarns(p.Warnings)
	if p.Usage {
		backupUsage()
		return 0
	}
	if len(p.Operands) != 2 {
		backupUsage()
		return 2
	}
	depth, code := depthFrom(p.Env, "backup")
	if code != 0 {
		return code
	}
	if p.Env.Get("VERB") == "replicate" {
		depth = 1
	}

	snapTime, err := backup.ParseSnapTime(p.Env.Get("SNAP_TIME"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	snapSize, err := backup.ParseSnapSize(p.Env.Get("SNAP_SIZE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	jsonMode := p.Env.Get("LOG_MODE") == "json"
	snapMode, err := backup.ParseSnapMode(p.Env.Get("SNAP_MODE"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	syncDir, err := backup.ParseSyncDirection(p.Env.Get("SYNC_DIRECTION"))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	flags := opt.SendRecvFrom(p.Env)
	createParent := p.Env.Bool("CREATE_PARENT", true)

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
	var origin endpoint.Endpoint
	if o := p.Env.Get("ORIGIN_ID"); o != "" {
		origin, err = parseEndpoint(o)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: target-origin: %v\n", err)
			return 1
		}
	}

	res, err := backup.Run(context.Background(), newReal(), backup.Request{
		Source:        src,
		Target:        tgt,
		DryRun:        p.Env.Bool("DRYRUN", false),
		Intermediate:  p.Env.Bool("SEND_INTR", true),
		SnapMode:      snapMode,
		SnapName:      strings.TrimPrefix(p.Env.Get("SNAP_NAME"), "@"),
		SnapTime:      snapTime,
		SnapSize:      snapSize,
		Depth:         depth,
		Include:       p.Env.List("INCLUDE"),
		Exclude:       p.Env.List("EXCLUDE"),
		SyncDirection: syncDir,
		Flags:         &flags,
		CreateParent:  &createParent,
		TargetOrigin:  origin,
		JSON:          jsonMode,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta backup: %v\n", err)
		return 1
	}
	var filteredWarnings []string
	for _, w := range res.Warnings {
		if strings.Contains(w, "raw send unavailable") {
			continue
		}
		filteredWarnings = append(filteredWarnings, w)
	}
	printWarns(filteredWarnings)
	if p.Env.Bool("DRYRUN", false) {
		fmt.Fprint(os.Stderr, res.Output)
	}
	if jsonMode && res.JSONReport != nil {
		data, err := json.Marshal(res.JSONReport)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta backup: JSON marshal: %v\n", err)
			return 1
		}
		fmt.Println(string(data))
	} else if !p.Env.Bool("DRYRUN", false) {
		fmt.Print(res.Output)
	}
	for _, msg := range res.Errors {
		fmt.Fprintf(os.Stderr, "zelta backup: %s\n", msg)
	}
	if len(res.Errors) > 0 {
		return 1
	}
	return 0
}

func backupUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta backup [-n] [-Ii] [--snapshot|--no-snapshot] [--target-origin ENDPOINT] [--push|--pull|--no-pull] [-d depth] [-X pat] SOURCE TARGET")
}
