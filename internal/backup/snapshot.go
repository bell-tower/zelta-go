package backup

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"zelta.space/internal/app"
	"zelta.space/internal/command"
	"zelta.space/internal/config"
	"zelta.space/internal/endpoint"
	"zelta.space/internal/execx"
	"zelta.space/internal/units"
	"zelta.space/internal/zfs"
)

// resolveSnapName expands SNAP_NAME (including "$(command)" substitutions)
// and SNAP_PREFIX into a "@name", once per run.
func (r *run) resolveSnapName() string {
	if !r.snapNameResolved {
		r.snapName = snapName(r.ctx, r.rt, r.sum.startTime)
		r.snapNameResolved = true
	}
	return r.snapName
}

// snapName resolves the snapshot name for a runtime the way Zelta always
// has: surrounding quotes are removed, "$(cmd)" (or, with a warning, any
// name containing whitespace) is run with the shell, a leading "@" is
// optional, an empty name becomes the start time, and SNAP_PREFIX replaces a
// leading "zelta" or is prepended.
func snapName(ctx context.Context, rt *app.Runtime, start int64) string {
	return expandSnapName(ctx, rt, rt.Opts.Get("SNAP_NAME"), start)
}

// expandSnapName applies snapName's rules to a raw SNAP_NAME value.
func expandSnapName(ctx context.Context, rt *app.Runtime, name string, start int64) string {
	if len(name) > 0 && (name[0] == '\'' || name[0] == '"') {
		q := name[:1]
		name = strings.TrimSuffix(name[1:], q)
	}
	if after, ok := strings.CutPrefix(name, "$("); ok {
		name = runSnapCommand(ctx, rt, strings.TrimSuffix(after, ")"))
	}
	if strings.ContainsAny(name, " \t\n") {
		rt.Log.Warnf("to define dynamic snapshot names, use this format: \"$(%s)\"", name)
		name = runSnapCommand(ctx, rt, name)
	}
	name = strings.TrimPrefix(name, "@")
	if name == "" {
		name = strconv.FormatInt(start, 10)
	}
	if prefix := rt.Opts.Get("SNAP_PREFIX"); prefix != "" {
		if rest, ok := strings.CutPrefix(name, "zelta"); ok {
			name = prefix + rest
		} else {
			name = prefix + name
		}
	}
	return "@" + name
}

func runSnapCommand(ctx context.Context, rt *app.Runtime, cmd string) string {
	res := rt.Run(ctx, command.Script(cmd))
	if out := res.Stdout(); len(out) > 0 {
		return app.SanitizeLine(out[0])
	}
	return ""
}

// thresholdsAllowSkip reports whether SNAP_TIME and SNAP_SIZE say an
// IF_NEEDED snapshot can be skipped: every given threshold must agree.
func thresholdsAllowSkip(rt *app.Runtime, changed int64, changedMissing bool, written int64) (bool, error) {
	o := rt.Opts
	if !o.Bool("SNAP_TIME") && !o.Bool("SNAP_SIZE") {
		return false, nil
	}
	if o.Bool("SNAP_TIME") {
		cutoff, err := snapTimeCutoff(rt.Unix(), o.Get("SNAP_TIME"))
		if err != nil {
			return false, err
		}
		if changedMissing || changed == 0 || changed < cutoff {
			return false, nil
		}
	}
	if o.Bool("SNAP_SIZE") {
		size, ok := units.ParseSize(o.Get("SNAP_SIZE"))
		if !ok || written >= size {
			return false, nil
		}
	}
	return true, nil
}

// epochThreshold separates bare epoch times from bare second counts.
const epochThreshold = 1_000_000_000

// snapTimeCutoff converts SNAP_TIME into the oldest acceptable
// snapshots_changed time: bare numbers of at least 1e9 are epoch times,
// smaller bare numbers and unit durations are relative to now.
func snapTimeCutoff(now int64, v string) (int64, error) {
	v = strings.TrimSpace(v)
	if units.IsBareNumber(v) {
		n := int64(units.Atof(v))
		if n >= epochThreshold {
			return n, nil
		}
		return now - n, nil
	}
	d, err := units.ParseDuration(v)
	if err != nil {
		return 0, app.Fail("%s", err.Error())
	}
	return now - d, nil
}

// shouldSnapshot returns the reason prefix for a snapshot, or "".
func (r *run) shouldSnapshot() (string, error) {
	if r.tree.snapshotAttempted {
		return "", nil
	}
	verb := "snapshotting: "
	if r.dryRun() {
		verb = "would snapshot: "
	}
	switch mode := r.snapMode(); {
	case mode == "ALWAYS":
		return verb, nil
	case mode != "IF_NEEDED":
		return "", nil
	}
	switch r.tree.snapshotNeeded {
	case snapWritten:
		skip, err := thresholdsAllowSkip(r.rt, r.tree.snapshotsChanged, r.tree.snapshotsChangedMissing, r.sum.sourceWritten)
		if err != nil || skip {
			return "", err
		}
		return "source is written; " + verb, nil
	case snapMissing:
		return "missing source snapshot; " + verb, nil
	case snapLatest:
		return "action requires a snapshot delta; " + verb, nil
	}
	return "", nil
}

func snapshotCommand(rt *app.Runtime, ep endpoint.Endpoint, dsSnap string) command.Cmd {
	return zfs.Command(rt, ep, zfs.RemoteDefault, command.Word("snapshot"), command.Word("-r"), command.Name(dsSnap))
}

// sleep pauses before retrying a snapshot; tests replace it.
var sleep = time.Sleep

// snapshotAttempts bounds retries of a colliding default snapshot name.
const snapshotAttempts = 3

// defaultSnapName reports whether SNAP_NAME is the built-in, time-based
// default rather than a name the user chose.
func defaultSnapName(rt *app.Runtime) bool {
	return rt.Opts.Source("SNAP_NAME") == config.FromDefault
}

// takeSnapshot runs "zfs snapshot -r" for ep@name. Default names have
// one-second resolution, so commands started within the same second (a
// backup followed by a failover, say) can collide; when retry is set and the
// name already exists, it waits for the next second and tries a fresh
// default name. "zfs snapshot -r" is atomic, so a collision created nothing.
// Names chosen by the user are never changed. It returns the name used and
// the result of the last attempt.
func takeSnapshot(ctx context.Context, rt *app.Runtime, ep endpoint.Endpoint, name string, retry bool) (string, execx.Result) {
	for attempt := 1; ; attempt++ {
		res := rt.Run(ctx, snapshotCommand(rt, ep, ep.DS+name))
		if res.OK() || !retry || attempt == snapshotAttempts || !alreadyExists(res) {
			return name, res
		}
		now := rt.Now()
		sleep(now.Truncate(time.Second).Add(time.Second).Sub(now))
		next := expandSnapName(ctx, rt, rt.Now().UTC().Format(config.SnapTimeFormat), 0)
		if next == name {
			return name, res
		}
		rt.Log.Noticef("snapshot '%s' already exists; snapshotting: %s", ep.DS+name, next)
		name = next
	}
}

func alreadyExists(res execx.Result) bool {
	for _, l := range res.Lines {
		if strings.Contains(l.Text, "dataset already exists") {
			return true
		}
	}
	return false
}

// createSourceSnapshot takes a recursive source snapshot when force is set
// (the reason prefix to log) or when one is needed.
func (r *run) createSourceSnapshot(force string) (bool, error) {
	reason := force
	if reason == "" {
		var err error
		if reason, err = r.shouldSnapshot(); err != nil || reason == "" {
			return false, err
		}
	}
	name := r.resolveSnapName()
	r.rt.Log.Noticef("%s%s", reason, name)
	r.tree.snapshotNeeded = snapNone
	r.tree.snapshotAttempted = true
	cmd := snapshotCommand(r.rt, r.src, r.src.DS+name)
	if r.dryRun() {
		r.created = name
		r.tree.predictedSnapshot = name
		r.rt.Log.Noticef("+ %s", cmd.String())
		return true, nil
	}
	name, res := takeSnapshot(r.ctx, r.rt, r.src, name, defaultSnapName(r.rt))
	r.snapName = name
	failed := false
	for _, l := range res.Lines {
		line := app.SanitizeLine(l.Text)
		failed = true
		if strings.Contains(line, "permission denied") {
			r.rt.Log.Warnf("permission denied snapshotting: %s", r.src.DS)
			break
		}
		r.rt.Log.Warnf("unexpected `zfs snapshot` output: %s", line)
	}
	if !failed && !res.OK() {
		failed = true
		r.rt.Log.Warnf("unexpected `zfs snapshot` exit status %d: %s", res.FirstFailure(), r.src.DS+name)
	}
	// --snapshot requires this snapshot; don't sync an older one instead.
	if failed && r.snapMode() == "ALWAYS" {
		return false, app.Fail("error creating snapshot '%s'", r.src.DS+name)
	}
	if failed {
		return false, nil
	}
	r.created = name
	for _, suffix := range r.list {
		r.updateLatest(src, suffix, name)
	}
	return true, nil
}

// applyPredictedSnapshot lets dry-run planning see the snapshot that would
// have been created.
func (r *run) applyPredictedSnapshot() {
	if !r.dryRun() || r.tree.predictedSnapshot == "" || r.tree.predictedApplied {
		return
	}
	for _, suffix := range r.list {
		r.updateLatest(src, suffix, r.tree.predictedSnapshot)
	}
	r.tree.predictedApplied = true
}

// updateLatest records a new latest snapshot: on the source after a
// snapshot, or on the target after a successful sync, where it becomes the
// new match.
func (r *run) updateLatest(s side, suffix, snap string) {
	sd := r.dataset(src, suffix)
	srcLatest := sd.latest
	if s == src {
		r.tree.sourceSnapNum++
		if srcLatest == "" {
			sd.earliest = snap
			sd.next = snap
		}
		if r.pair(suffix).match == srcLatest {
			sd.next = snap
		}
		sd.latest = snap
		return
	}
	r.pair(suffix).match = snap
	r.dataset(tgt, suffix).latest = snap
	if snap == srcLatest || (r.tree.finalSnapshot != "" && snap == r.tree.finalSnapshot) {
		sd.next = ""
		r.tree.syncable--
		a := r.action(suffix)
		a.statusInfo = "up-to-date"
		a.canSync = false
		return
	}
	r.computeSendRange(suffix)
}

// Snapshot implements "zelta snapshot": a recursive snapshot of a tree,
// optionally skipped when SNAP_TIME or SNAP_SIZE thresholds are not met.
func Snapshot(ctx context.Context, rt *app.Runtime, args []string) error {
	_, err := TakeSnapshot(ctx, rt, args)
	return err
}

// TakeSnapshot is Snapshot returning the snapshot created ("pool/ds@name"),
// or "" when thresholds skipped it or in a dry run.
func TakeSnapshot(ctx context.Context, rt *app.Runtime, args []string) (string, error) {
	if rt.Opts.Bool("USAGE") {
		return "", app.Usage("")
	}
	if len(args) == 0 {
		return "", app.Usage("missing endpoint argument")
	}
	if len(args) > 1 {
		return "", app.Usage("too many operands: %s", args[1])
	}
	ep := rt.Endpoint(args[0])
	r := newRun(ctx, rt, args)
	r.src = ep
	name := ep.Snap
	if name == "" {
		if rt.Opts.Bool("SNAP_TIME") || rt.Opts.Bool("SNAP_SIZE") {
			r.resetState()
			if ok, err := r.loadProperties(src); err != nil {
				return "", err
			} else if !ok {
				return "", fail("source dataset '%s' does not exist", ep.ID)
			}
			skip, err := thresholdsAllowSkip(rt, r.tree.snapshotsChanged, r.tree.snapshotsChangedMissing, r.sum.sourceWritten)
			if err != nil {
				return "", err
			}
			if skip {
				rt.Log.Noticef("snapshot not needed: '%s'", ep.DS)
				return "", nil
			}
		}
		name = r.resolveSnapName()
	}
	if rt.DryRun() {
		rt.Log.Noticef("+ %s", snapshotCommand(rt, ep, ep.DS+name).String())
		return "", nil
	}
	name, res := takeSnapshot(ctx, rt, ep, name, ep.Snap == "" && defaultSnapName(rt))
	dsSnap := ep.DS + name
	for _, line := range res.Combined() {
		fmt.Fprintln(rt.Stderr, app.SanitizeLine(line))
	}
	if !res.OK() {
		rt.Log.Errorf("error creating '%s'", dsSnap)
		return "", &app.ExitError{Code: 1}
	}
	rt.Log.Noticef("snapshot created '%s'", dsSnap)
	return dsSnap, nil
}
