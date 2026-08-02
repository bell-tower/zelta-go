package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/internal/opt"
	"github.com/bell-tower/zelta-go/internal/zeport"
	"github.com/bell-tower/zelta-go/internal/zlog"
	"github.com/bell-tower/zelta-go/zfs"
)

func runReport(args []string) int {
	p, err := opt.Parse("report", args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		return 1
	}
	sink := newLogSink(p)
	defer sink.Close()
	printWarns(sink, p.Warnings)
	if p.Usage {
		reportUsage()
		return 0
	}

	var eps []string
	if br := p.Env.Get("BACKUP_ROOT"); br != "" {
		eps = []string{br}
	} else if len(p.Operands) >= 1 {
		eps = p.Operands
	} else {
		sink.Error("BACKUP_ROOT not set and no endpoints provided")
		return 1
	}

	get := func(k string) string { return p.Env.Get(k) }
	hostFallback := resolveHostname(p)
	execr := newReal()
	tooOld := time.Now().Unix() - int64(zeport.MaxAge/time.Second)
	ctx := context.Background()

	for _, raw := range eps {
		if err := processReportEndpoint(ctx, execr, sink, raw, hostFallback, tooOld, get); err != nil {
			fmt.Fprintf(os.Stderr, "zelta report: %v\n", err)
			return 1
		}
	}
	return 0
}

func processReportEndpoint(ctx context.Context, execr *zfs.Real, sink *zlog.Sink, raw, hostFallback string, tooOld int64, get zeport.Lookup) error {
	ep, err := endpoint.Parse(raw)
	if err != nil {
		return err
	}
	meta := zeport.Endpoint{
		ID:      raw,
		Host:    ep.Host,
		Dataset: ep.Dataset,
		Leaf:    zeport.LeafOf(ep.Dataset),
	}
	if meta.Host == "" || meta.Host == "localhost" {
		meta.Host = hostFallback
	}

	out, err := execr.Output(ctx, raw, zeport.ListArgv(ep.Dataset))
	if err != nil {
		return fmt.Errorf("%s: %w", raw, err)
	}
	rows := zeport.ParseListLines(splitOutputLines(string(out)))
	hasSnaps := func(ds string) bool {
		o, err := execr.Output(ctx, raw, zeport.SnapListArgv(ds))
		if err != nil {
			return false
		}
		return len(splitOutputLines(string(o))) > 0
	}
	res := zeport.Classify(meta, tooOld, rows, hasSnaps)
	msg := zeport.Message(res, get)
	sink.Notice(msg)
	if cmd := zeport.Command(res, msg, get); cmd != "" {
		runReportHook(cmd)
	}
	return nil
}

func runReportHook(cmdline string) {
	// Oracle: `_cmd | getline; close(_cmd)` — run via shell, discard stdout.
	c := exec.Command("sh", "-c", cmdline)
	c.Stdout = io.Discard
	c.Stderr = io.Discard
	_ = c.Run()
}

func resolveHostname(p *opt.Parsed) string {
	if h := p.Env.Get("HOSTNAME"); h != "" {
		return h
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "localhost"
}

func splitOutputLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func reportUsage() {
	fmt.Fprintln(os.Stderr, "usage: zelta report [OPTIONS] [ENDPOINT...]")
	fmt.Fprintln(os.Stderr, "Report backup datasets with out-of-date snapshots (experimental).")
	fmt.Fprintln(os.Stderr, "Uses BACKUP_ROOT when no endpoints are given.")
}
