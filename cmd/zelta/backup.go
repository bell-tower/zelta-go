package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/bell-tower/zelta-go/backup"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/internal/opt"
	"github.com/bell-tower/zelta-go/internal/report"
)

func runBackup(args []string) int {
	p, err := opt.Parse("backup", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err) // oracle stop() prefix
		return 1
	}
	sink := newLogSink(p)
	defer sink.Close()
	printWarns(sink, p.Warnings)
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

	exec := newReal()
	wireCommandEcho(exec, sink)
	dryRun := p.Env.Bool("DRYRUN", false)
	startTime := time.Now()
	req := backup.Request{
		Source:        src,
		Target:        tgt,
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
	}
	if dryRun {
		// Oracle -n: recon + plan, then render "+ …" commands without
		// executing anything (Prepare may still create a missing parent).
		plan, err := backup.Prepare(context.Background(), exec, req)
		if err != nil {
			// Oracle stop(): still emit JSON with errorMessages when --json.
			fmt.Fprintf(os.Stderr, "zelta backup: %v\n", err)
			if jsonMode {
				emitBackupJSON(report.NewBackupResult(
					src, tgt, 0, nil, []string{err.Error()}, nil, startTime, time.Now(), 0, 0, 0,
				))
			}
			return 1
		}
		out, err := backup.FormatDryRunDirection(plan, src.String(), tgt.String(), syncDir.PipeArg())
		if err != nil {
			fmt.Fprintf(os.Stderr, "zelta backup: %v\n", err)
			if jsonMode {
				emitBackupJSON(report.NewBackupResult(
					src, tgt, 0, nil, []string{err.Error()}, nil, startTime, time.Now(), 0, 0, 0,
				))
			}
			return 1
		}
		if plan.Full+plan.Incr == 0 {
			if sum := plan.Summary(); sum != "" {
				out += sum + "\n"
			}
		}
		emitBlob(sink, out)
		if jsonMode {
			streams, sent := plan.StreamCount()
			var messages []string
			for _, w := range plan.Warnings {
				messages = append(messages, "warning: "+w)
			}
			emitBackupJSON(report.NewBackupResult(
				src, tgt, streams, sent, nil, messages, startTime, time.Time{}, 0, 0, 0,
			))
		}
		return 0
	}
	res, err := backup.Run(context.Background(), exec, req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta backup: %v\n", err)
		if jsonMode {
			emitBackupJSON(report.NewBackupResult(
				src, tgt, 0, nil, []string{err.Error()}, nil, startTime, time.Now(), 0, 0, 0,
			))
		}
		return 1
	}
	var filteredWarnings []string
	for _, w := range res.Warnings {
		if strings.Contains(w, "raw send unavailable") {
			continue
		}
		filteredWarnings = append(filteredWarnings, w)
	}
	printWarns(sink, filteredWarnings)
	// Oracle: syncing/up-to-date/+ command lines are LOG_NOTICE — `-q`
	// silences them; json mode prefixes them to stderr; terminal mode
	// prints them to stdout (dry-run and execute alike).
	emitBlob(sink, res.Output)
	if jsonMode {
		streams, sent := 0, []string(nil)
		if res.Plan != nil {
			streams, sent = res.Plan.StreamCount()
		}
		var messages []string
		for _, w := range filteredWarnings {
			messages = append(messages, "warning: "+w)
		}
		st, et := res.StartTime, res.EndTime
		if st.IsZero() {
			st = startTime
		}
		if et.IsZero() {
			et = time.Now()
		}
		emitBackupJSON(report.NewBackupResult(
			src, tgt, streams, sent, res.Errors, messages,
			st, et, res.Stats.Bytes, res.Stats.Streams, res.Stats.Secs,
		))
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

func emitBackupJSON(r *report.BackupResult) {
	data, err := r.Marshal()
	if err != nil {
		fmt.Fprintf(os.Stderr, "zelta backup: JSON marshal: %v\n", err)
		return
	}
	fmt.Println(string(data))
}
