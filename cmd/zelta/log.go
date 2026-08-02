package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/zlog"
	"git.belltower.it/djbell/zelta-go/zfs"
)

// newLogSink builds the leveled log sink from parsed options (oracle
// LOG_LEVEL / LOG_MODE / LOG_FILE, plus ZELTA_LOG_PREFIX for policy children).
func newLogSink(p *opt.Parsed) *zlog.Sink {
	level, err := strconv.Atoi(p.Env.Get("LOG_LEVEL"))
	if err != nil {
		level = 2 // opt default; tolerate bogus env values
	}
	mode := p.Env.Get("LOG_MODE")
	file := p.Env.Get("LOG_FILE")
	prefix := os.Getenv("ZELTA_LOG_PREFIX")
	s, err := zlog.New(zlog.Level(level), mode, prefix, file)
	if err != nil {
		// Oracle `exec 3>>file` failure is fatal; Go warns and falls back
		// to stderr so the verb can still report.
		fmt.Fprintf(os.Stderr, "warning: log file: %v\n", err)
		s, _ = zlog.New(zlog.Level(level), mode, prefix, "")
	}
	return s
}

// emitBlob routes a notice-level text blob through the sink line by line
// (oracle report(LOG_NOTICE) per line). At default level on a terminal this
// reproduces the blob exactly on stdout.
func emitBlob(s *zlog.Sink, out string) {
	if s == nil {
		return
	}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		s.Notice(line)
	}
}

// wireCommandEcho hooks the executor's per-command callback to the sink
// (oracle LOG_DEBUG "`command`" echoes). Consumer-side: the library reports
// nothing; the CLI decides what chatter reaches the user.
func wireCommandEcho(real *zfs.Real, sink *zlog.Sink) {
	if real == nil || sink == nil {
		return
	}
	real.LogCmd = func(ep endpoint.Endpoint, argv []string) {
		sink.Debug(zfs.CommandDebug(ep, argv))
	}
}
