// Package report implements "zelta report" (zeport), which flags backup
// datasets whose snapshots have not changed in the last 24 hours and can run
// a notification command with the result.
package report

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/zfs"
)

// maxAge is how old a dataset's latest snapshot change may be.
const maxAge = 86400

// Report statuses.
const (
	StatusNone = "none"
	StatusAll  = "all"
	StatusSome = "some"
	StatusOK   = "ok"
)

var defaultMessages = map[string]string{
	StatusNone: "{endpoint} no snapshots found",
	StatusAll:  "{endpoint} all snapshots are out of date",
	StatusSome: "{endpoint} some snapshots are out of date: {dslist}",
	StatusOK:   "{endpoint} snapshots are up to date",
}

// result is the snapshot freshness of one endpoint.
type result struct {
	ep  endpoint.Endpoint
	old []string
	ok  int
}

func (r *result) status() string {
	switch {
	case r.ok == 0 && len(r.old) == 0:
		return StatusNone
	case r.ok == 0:
		return StatusAll
	case len(r.old) > 0:
		return StatusSome
	}
	return StatusOK
}

// expand substitutes report symbols in a message or command template.
func (r *result) expand(template, message string) string {
	return strings.NewReplacer(
		"{message}", message,
		"{endpoint}", r.ep.ID,
		"{host}", r.ep.Host,
		"{dataset}", r.ep.DS,
		"{dslist}", strings.Join(r.old, " "),
		"{oldcount}", strconv.Itoa(len(r.old)),
		"{okcount}", strconv.Itoa(r.ok),
		"{status}", r.status(),
	).Replace(template)
}

// Run implements "zelta report [ENDPOINT...]". BACKUP_ROOT, when set, is the
// only endpoint checked.
func Run(ctx context.Context, rt *app.Runtime, args []string) error {
	eps := args
	if root := rt.Opts.Get("BACKUP_ROOT"); root != "" {
		eps = []string{root}
	}
	if len(eps) == 0 {
		return app.Fail("BACKUP_ROOT not set and no endpoints provided")
	}
	cutoff := rt.Unix() - maxAge
	for _, id := range eps {
		r := check(ctx, rt, rt.Endpoint(id), cutoff)
		status := r.status()
		msg := rt.Opts.Get("REPORT_MESSAGE_" + strings.ToUpper(status))
		if msg == "" {
			msg = rt.Opts.Get("REPORT_MESSAGE_DEFAULT")
		}
		if msg == "" {
			msg = defaultMessages[status]
		}
		msg = r.expand(msg, "")
		rt.Log.Noticef("%s", msg)
		cmd := rt.Opts.Get("REPORT_COMMAND_" + strings.ToUpper(status))
		if cmd == "" {
			cmd = rt.Opts.Get("REPORT_COMMAND_DEFAULT")
		}
		if cmd != "" {
			forwardStderr(rt, rt.Run(ctx, command.Script(r.expand(cmd, msg))))
		}
	}
	return nil
}

// check classifies the datasets below ep by snapshots_changed.
func check(ctx context.Context, rt *app.Runtime, ep endpoint.Endpoint, cutoff int64) *result {
	r := &result{ep: ep}
	list := zfs.Command(rt, ep, zfs.RemoteDefault, append(command.Words("list", "-t", "filesystem,volume", "-Hpr",
		"-o", "name,snapshots_changed,usedbysnapshots", "-S", "snapshots_changed"), command.Name(ep.DS))...)
	res := rt.Run(ctx, list)
	forwardStderr(rt, res)
	seen := map[string]bool{}
	for _, f := range rows(res) {
		if len(f) < 3 || seen[f[0]] {
			continue
		}
		ds, changed, usedBySnaps := f[0], f[1], f[2]
		seen[ds] = true
		if changed == "-" {
			continue
		}
		rel := strings.TrimPrefix(strings.TrimPrefix(ds, ep.DS), "/")
		if rel == "" {
			rel = ep.Leaf
		}
		when, _ := strconv.ParseInt(changed, 10, 64)
		if when >= cutoff {
			r.ok++
			continue
		}
		if usedBySnaps == "0" && !hasSnapshots(ctx, rt, ep, ds) {
			r.ok++
			continue
		}
		r.old = append(r.old, rel)
	}
	return r
}

// hasSnapshots reports whether ds itself has any snapshots.
func hasSnapshots(ctx context.Context, rt *app.Runtime, ep endpoint.Endpoint, ds string) bool {
	res := rt.Run(ctx, zfs.Command(rt, ep, zfs.RemoteDefault, append(command.Words("list", "-Hro", "name", "-t", "snapshot", "-d", "1"), command.Name(ds))...))
	return len(res.Stdout()) > 0
}

func rows(res execx.Result) [][]string {
	var out [][]string
	for _, l := range res.Stdout() {
		var f []string
		for s := range strings.SplitSeq(app.SanitizeLine(l), "\t") {
			if s != "" {
				f = append(f, s)
			}
		}
		out = append(out, f)
	}
	return out
}

func forwardStderr(rt *app.Runtime, res execx.Result) {
	for _, l := range res.Stderr() {
		fmt.Fprintln(rt.Stderr, app.SanitizeLine(l))
	}
}
