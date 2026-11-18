package backup

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"zelta.space/internal/app"
	"zelta.space/internal/execx"
)

const defaultSnap = "@zelta_2026-01-02_03.04.05"

var testNow = app.TestNow.Unix()

// writtenFx scripts an incremental pair whose source has unsnapshotted
// writes; the second source listing includes the new snapshot.
func writtenFx(t *testing.T, opts map[string]string, extraProps ...string) *fx {
	return writtenFxSnap(t, opts, execx.Out(), extraProps...)
}

func writtenFxSnap(t *testing.T, opts map[string]string, snapRes execx.Result, extraProps ...string) *fx {
	f := newFx(t, "backup", opts)
	props := append([]string{". type filesystem", ". written 4096"}, extraProps...)
	f.props("apool/src", props...).props("bpool/tgt", ". type filesystem").
		list("bpool/tgt", "@a 1", ". 8").
		on("zfs snapshot -r 'apool/src"+defaultSnap+"'", snapRes)
	f.tr.Fake.OnFunc(func(s string) bool { return s == listCmd("", "apool/src", "name,guid", "") }, func(string) execx.Result {
		if len(f.tr.Fake.CallsMatching("zfs snapshot")) > 0 {
			return execx.Out("apool/src"+defaultSnap+"\t3", "apool/src@a\t1", "apool/src\t9")
		}
		return execx.Out("apool/src@a\t1", "apool/src\t9")
	})
	f.on("{ zfs send -P -L -c -e -I 'apool/src@a' 'apool/src"+defaultSnap+"'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1024, 1))
	return f
}

func TestSnapshotWhenWritten(t *testing.T) {
	f := writtenFx(t, nil)
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v %s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(), "source is written; snapshotting: "+defaultSnap, "syncing 1 datasets", "dataset up-to-date", "1K sent, 1 streams received in 0.5 seconds")
}

func TestSnapshotThresholds(t *testing.T) {
	recent := ". snapshots_changed " + strconv.FormatInt(testNow-100, 10)
	old := ". snapshots_changed " + strconv.FormatInt(testNow-7200, 10)
	tests := []struct {
		name  string
		opts  map[string]string
		props []string
		snap  bool
	}{
		{"recent within 1h", map[string]string{"SNAP_TIME": "1h"}, []string{recent}, false},
		{"old beyond 1h", map[string]string{"SNAP_TIME": "1h"}, []string{old}, true},
		{"bare seconds", map[string]string{"SNAP_TIME": "3600"}, []string{recent}, false},
		{"epoch cutoff", map[string]string{"SNAP_TIME": strconv.FormatInt(testNow-50, 10)}, []string{recent}, true},
		{"missing changed time", map[string]string{"SNAP_TIME": "1h"}, []string{". snapshots_changed -"}, true},
		{"no changed time", map[string]string{"SNAP_TIME": "1h"}, nil, true},
		{"small writes", map[string]string{"SNAP_SIZE": "1M"}, nil, false},
		{"large writes", map[string]string{"SNAP_SIZE": "1K"}, nil, true},
		{"bad size", map[string]string{"SNAP_SIZE": "lots"}, nil, true},
		{"both must agree", map[string]string{"SNAP_TIME": "1h", "SNAP_SIZE": "1K"}, []string{recent}, true},
		{"always ignores thresholds", map[string]string{"SNAP_TIME": "1h", "SNAP_MODE": "ALWAYS"}, []string{recent}, true},
	}
	for _, tt := range tests {
		f := writtenFx(t, tt.opts, tt.props...)
		f.backup("apool/src", "bpool/tgt")
		snapped := len(f.tr.Fake.CallsMatching("zfs snapshot")) > 0
		if snapped != tt.snap {
			t.Errorf("%s: snapshot = %v\n%s%s", tt.name, snapped, f.tr.Out.String(), f.tr.Err.String())
		}
	}
	f := writtenFx(t, map[string]string{"SNAP_TIME": "1m"}, recent)
	err := f.backup("apool/src", "bpool/tgt")
	if err == nil || !strings.Contains(err.Error(), "ambiguous duration unit 'm'") {
		t.Errorf("err = %v", err)
	}
	if c, _ := snapTimeCutoff(1000, "30"); c != 970 {
		t.Errorf("cutoff = %d", c)
	}
}

func TestSnapshotModes(t *testing.T) {
	// SNAP_MODE=ALWAYS snapshots even an unwritten source.
	f := newFx(t, "backup", map[string]string{"SNAP_MODE": "always", "DRYRUN": "1"})
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").list("apool/src", "@a 1", ". 9").list("bpool/tgt", "@a 1", ". 8")
	f.backup("apool/src", "bpool/tgt")
	eqLines(t, "always", f.tr.Out.String(),
		"would snapshot: "+defaultSnap,
		"+ zfs snapshot -r 'apool/src"+defaultSnap+"'",
		"would sync 1 datasets",
		"+ { zfs send -P -L -c -e -I 'apool/src@a' 'apool/src"+defaultSnap+"'|zfs recv -v -s 'bpool/tgt' ; }")

	// A dataset without snapshots triggers one.
	f = newFx(t, "backup", map[string]string{"DRYRUN": "1"})
	f.props("apool/src", ". type filesystem", "/new type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "/new 5", "@a 1", ". 9").list("bpool/tgt", "@a 1", ". 8")
	f.backup("apool/src", "bpool/tgt")
	contains(t, "missing", f.tr.Out.String(), "missing source snapshot; would snapshot: "+defaultSnap,
		"+ { zfs send -P -L -c -e 'apool/src/new"+defaultSnap+"'|zfs recv -v -u -x mountpoint -o canmount=noauto -s 'bpool/tgt/new' ; }")

	// Rotate needs a delta when the source has nothing newer.
	f = newFx(t, "rotate", map[string]string{"DRYRUN": "1", "VERB": "rotate"})
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").list("apool/src", "@a 1", ". 9").list("bpool/tgt", "@b 2", "@a 1", ". 8")
	f.backup("apool/src", "bpool/tgt")
	contains(t, "rotate", f.tr.Out.String(), "action requires a snapshot delta; would snapshot: "+defaultSnap)

	// No snapshots and no snapshot mode is fatal.
	f = newFx(t, "backup", map[string]string{"SNAP_MODE": "0"})
	f.props("apool/src", ". type filesystem").missing("bpool/tgt").list("apool/src", ". 9")
	err := f.backup("apool/src", "bpool/tgt")
	if err == nil || err.Error() != "source 'apool/src' has no snapshots" {
		t.Errorf("err = %v", err)
	}

	// Failed snapshots are warnings and fall back to existing snapshots.
	for _, out := range []execx.Result{execx.Fail(1, "cannot create snapshots : permission denied"), execx.Fail(1, "weird"), execx.Fail(1)} {
		f = writtenFxSnap(t, nil, out)
		f.backup("apool/src", "bpool/tgt")
		contains(t, "failed snapshot", f.tr.Err.String(), "warning: ")
	}
}

func TestSnapName(t *testing.T) {
	tests := []struct {
		name, prefix, want string
	}{
		{"", "", "@1767323045"},
		{"plain", "", "@plain"},
		{"@at", "", "@at"},
		{"'quoted'", "", "@quoted"},
		{`"dq"`, "", "@dq"},
		{"zelta_x", "daily", "@daily_x"},
		{"other", "me_", "@me_other"},
		{"$(echo dyn)", "", "@dyn"},
		{"'$(echo dyn)'", "", "@dyn"},
		{"echo spaced", "", "@spaced"},
	}
	for _, tt := range tests {
		tr := app.NewTest("backup", map[string]string{"SNAP_NAME": tt.name, "SNAP_PREFIX": tt.prefix})
		if tt.name == "" {
			tr.Opts.Set("SNAP_NAME", "", 0)
		}
		tr.Fake.On("echo dyn", execx.Out("dyn")).On("echo spaced", execx.Out("spaced\r"))
		if got := snapName(context.Background(), tr.Runtime, testNow); got != tt.want {
			t.Errorf("snapName(%q, %q) = %q want %q", tt.name, tt.prefix, got, tt.want)
		}
		if strings.Contains(tt.name, " ") && !strings.Contains(tt.name, "$(") && !strings.Contains(tr.Err.String(), "use this format") {
			t.Errorf("missing whitespace warning")
		}
	}
	tr := app.NewTest("backup", map[string]string{"SNAP_NAME": "$(false)"})
	tr.Fake.On("false", execx.Fail(1))
	if got := snapName(context.Background(), tr.Runtime, 5); got != "@5" {
		t.Errorf("failed command = %q", got)
	}
}

func TestSnapshotVerb(t *testing.T) {
	f := newFx(t, "snapshot", map[string]string{"SNAP_NAME": "manual_test"})
	f.on("zfs snapshot -r 'apool/src@manual_test'", execx.Out())
	if err := f.run(Snapshot, "apool/src"); err != nil {
		t.Fatal(err)
	}
	eqLines(t, "stdout", f.tr.Out.String(), "snapshot created 'apool/src@manual_test'")

	f = newFx(t, "snapshot", nil)
	f.on("ssh -n me@h zfs snapshot -r 'apool/src@given'", execx.Fail(1, "cannot create snapshot"))
	if code := finish(f, f.run(Snapshot, "me@h:apool/src@given")); code != 1 {
		t.Errorf("code = %d", code)
	}
	eqLines(t, "stderr", f.tr.Err.String(), "error: error creating 'apool/src@given'")
	if f.tr.RawErr.String() != "cannot create snapshot\n" {
		t.Errorf("raw stderr = %q", f.tr.RawErr.String())
	}

	f = newFx(t, "snapshot", map[string]string{"DRYRUN": "1"})
	f.run(Snapshot, "apool/src")
	eqLines(t, "dry run", f.tr.Out.String(), "+ zfs snapshot -r 'apool/src"+defaultSnap+"'")

	f = newFx(t, "snapshot", map[string]string{"SNAP_TIME": "1h"})
	f.props("apool/src", ". snapshots_changed "+strconv.FormatInt(testNow-60, 10))
	if err := f.run(Snapshot, "apool/src"); err != nil {
		t.Fatal(err)
	}
	eqLines(t, "skip", f.tr.Out.String(), "snapshot not needed: 'apool/src'")

	f = newFx(t, "snapshot", map[string]string{"SNAP_SIZE": "1K"})
	f.props("apool/src", ". written 4096").on("zfs snapshot -r 'apool/src"+defaultSnap+"'", execx.Out())
	f.run(Snapshot, "apool/src")
	eqLines(t, "size", f.tr.Out.String(), "snapshot created 'apool/src"+defaultSnap+"'")

	f = newFx(t, "snapshot", map[string]string{"SNAP_SIZE": "1K"})
	f.missing("apool/src")
	if err := f.run(Snapshot, "apool/src"); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing source err = %v", err)
	}
	f = newFx(t, "snapshot", map[string]string{"SNAP_TIME": "2m"})
	f.props("apool/src", ". written 1")
	if err := f.run(Snapshot, "apool/src"); err == nil {
		t.Errorf("ambiguous duration should fail")
	}
	f = newFx(t, "snapshot", map[string]string{"SNAP_TIME": "1h"})
	f.on(getCmd("", "apool/src", ""), execx.Out("ssh: Could not resolve hostname x"))
	if err := f.run(Snapshot, "apool/src"); exitCode(err) != 1 {
		t.Errorf("connection error = %v", err)
	}
	for _, args := range [][]string{nil, {"a", "b"}} {
		f = newFx(t, "snapshot", nil)
		if err := f.run(Snapshot, args...); err == nil {
			t.Errorf("args %v should be a usage error", args)
		}
	}
	f = newFx(t, "snapshot", map[string]string{"USAGE": "1"})
	if err := f.run(Snapshot, "a"); err == nil {
		t.Errorf("usage")
	}
}

// snapshotFailure scripts the 031 ShellSpec case: the source was written,
// the snapshot cannot be created, and an older snapshot exists.
func snapshotFailure(t *testing.T, opts map[string]string) *fx {
	f := newFx(t, "backup", opts)
	f.props("apool/source", ". type filesystem", ". written 4096").
		missing("bpool/backups/source").
		list("apool/source", "@old 2", ". 1").
		on("zfs create -vupo canmount=noauto 'bpool/backups'", execx.Out()).
		on("zfs snapshot -r 'apool/source@new'", execx.Fail(1, "cannot create snapshot 'apool/source@new': dataset already exists")).
		on("{ zfs send -P -L -c -e 'apool/source@old'|zfs recv -v "+fullRecvTop+" 'bpool/backups/source' ; }", received("full", 1024, 1))
	return f
}

func TestForcedSnapshotFailureStops(t *testing.T) {
	f := snapshotFailure(t, map[string]string{"SNAP_MODE": "ALWAYS", "SNAP_NAME": "new", "LOG_LEVEL": "0"})
	err := f.backup("apool/source", "bpool/backups/source")
	if app.Code(err) != 1 || err.Error() != "error creating snapshot 'apool/source@new'" {
		t.Fatalf("err = %v", err)
	}
	if sends := f.tr.Fake.CallsMatching("zfs send"); len(sends) != 0 {
		t.Errorf("an older snapshot must not be synced: %v", sends)
	}
}

func TestSnapshotFailureFallsBackWithoutForce(t *testing.T) {
	f := snapshotFailure(t, map[string]string{"SNAP_NAME": "new", "LOG_LEVEL": "0"})
	if err := f.backup("apool/source", "bpool/backups/source"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	if sends := f.tr.Fake.CallsMatching("zfs send"); len(sends) != 1 || !strings.Contains(sends[0], "'apool/source@old'") {
		t.Errorf("sends = %v", sends)
	}
}
