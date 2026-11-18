package zelta

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"zelta.space/internal/execx"
)

const defaultSnap = "@zelta_2026-01-02_03.04.05"

func TestSnapshot(t *testing.T) {
	ctx := context.Background()
	f := execx.NewFake().On("zfs snapshot -r 'apool/src"+defaultSnap+"'", execx.Out())
	c, rec := newClient(t, f)
	name, err := c.Snapshot(ctx, "apool/src", nil)
	if err != nil || name != "apool/src"+defaultSnap {
		t.Fatalf("Snapshot = %q, %v\n%s", name, err, rec)
	}
	if !rec.has("INFO snapshot: snapshot created 'apool/src" + defaultSnap + "'") {
		t.Errorf("log:\n%s", rec)
	}

	f = execx.NewFake().On("zfs snapshot -r 'apool/src@mine'", execx.Out())
	c, _ = newClient(t, f)
	if name, err = c.Snapshot(ctx, "apool/src", &SnapshotOptions{Name: "mine"}); err != nil || name != "apool/src@mine" {
		t.Errorf("named = %q, %v", name, err)
	}
	if name, err = c.Snapshot(ctx, "apool/src@mine", &SnapshotOptions{Name: "other"}); err != nil || name != "apool/src@mine" {
		t.Errorf("endpoint name wins: %q, %v", name, err)
	}
	if name, err = c.Snapshot(ctx, "apool/src", &SnapshotOptions{Name: "x", DryRun: true}); err != nil || name != "" || len(f.CallsMatching("@x")) != 0 {
		t.Errorf("dry run = %q, %v", name, err)
	}
	for _, bad := range []string{"$(date)", "a b", "x@y", "'q'"} {
		if _, err := c.Snapshot(ctx, "apool/src", &SnapshotOptions{Name: bad}); err == nil || !strings.Contains(err.Error(), "invalid snapshot name") {
			t.Errorf("%q: %v", bad, err)
		}
		if _, err := c.Backup(ctx, "a/b", "c/d", &BackupOptions{Snapshot: SnapshotPolicy{Name: bad}}); err == nil {
			t.Errorf("backup accepted %q", bad)
		}
	}
	if len(f.CallsMatching("date")) != 0 {
		t.Error("ran a shell substitution")
	}

	// Thresholds skip the snapshot.
	f = execx.NewFake().On(getCmd("apool/src", ""), execx.Out(
		"apool/src\ttype\tfilesystem", "apool/src\twritten\t10",
		"apool/src\tsnapshots_changed\t"+itoa(testNow.Unix()-60)))
	c, rec = newClient(t, f)
	name, err = c.Snapshot(ctx, "apool/src", &SnapshotOptions{MinInterval: time.Hour, MinWritten: 1 << 20})
	if err != nil || name != "" || len(f.CallsMatching("zfs snapshot")) != 0 {
		t.Errorf("skip = %q, %v, %v\n%s", name, err, f.Calls, rec)
	}
	if !rec.has("INFO snapshot: snapshot not needed: 'apool/src'") {
		t.Errorf("log:\n%s", rec)
	}

	f = execx.NewFake().On("zfs snapshot -r 'apool/src"+defaultSnap+"'", execx.Fail(1, "cannot create snapshot: permission denied"))
	c, rec = newClient(t, f)
	_, err = c.Snapshot(ctx, "apool/src", nil)
	if e := asError(t, err); e.Code != 1 || e.Message != "error creating 'apool/src"+defaultSnap+"'" {
		t.Errorf("err = %#v", e)
	}
	if !rec.has("WARN snapshot: cannot create snapshot: permission denied") {
		t.Errorf("zfs output is logged:\n%s", rec)
	}
	if _, err = c.Snapshot(ctx, "", nil); asError(t, err).Message != "missing endpoint argument" {
		t.Errorf("missing endpoint: %v", err)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func pruneTrees() map[string][]snap {
	day := int64(86400)
	now := testNow.Unix()
	var src, guard []snap
	for i := range 6 {
		s := snap{"@s" + itoa(int64(6-i)), itoa(int64(100 + 6 - i)), 10, now - int64(i+1)*40*day}
		src = append(src, s)
		if i >= 1 {
			guard = append(guard, snap{s.savepoint, s.guid, 0, s.creation})
		}
	}
	src = append(src, snap{"", "1", 0, 0})
	guard = append(guard, snap{"", "2", 0, 0})
	return map[string][]snap{"apool/src": src, "bpool/tgt": guard}
}

func TestPrune(t *testing.T) {
	ctx := context.Background()
	f := fakeZFS(pruneTrees())
	c, rec := newClient(t, f)
	opts := &PruneOptions{Guard: "bpool/tgt", Keep: 1}
	plan, err := c.PlanPrune(ctx, "apool/src", opts)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	want := []PruneDataset{{Name: "apool/src", Suffix: "", Destroy: []string{"@s1", "@s2", "@s3"}, Keep: []string{"@s4"}}}
	if !reflect.DeepEqual(plan.Datasets, want) {
		t.Errorf("plan = %+v", plan.Datasets)
	}
	if got := plan.Candidates(); !reflect.DeepEqual(got, []string{"apool/src@s1%s3"}) {
		t.Errorf("candidates = %q", got)
	}
	if len(mutations(f)) != 0 || plan.Destroyed != nil {
		t.Errorf("PlanPrune destroyed: %q", mutations(f))
	}

	f.On("zfs destroy 'apool/src@s1%s3'", execx.Out())
	plan, err = c.Prune(ctx, "apool/src", &PruneOptions{Guard: "bpool/tgt", Keep: 1, DryRun: true})
	if err != nil || len(mutations(f)) != 0 || len(plan.Destroyed) != 0 || !rec.has("INFO prune: + zfs destroy 'apool/src@s1%s3'") {
		t.Errorf("dry run: %v %q\n%s", err, mutations(f), rec)
	}
	plan, err = c.Prune(ctx, "apool/src", opts)
	if err != nil || !reflect.DeepEqual(plan.Destroyed, []string{"apool/src@s1%s3"}) || !reflect.DeepEqual(mutations(f), []string{"zfs destroy 'apool/src@s1%s3'"}) {
		t.Errorf("prune: %v %+v %q", err, plan, mutations(f))
	}

	// Without a guard, Prune refuses unless the guard is disabled.
	_, err = c.Prune(ctx, "apool/src", &PruneOptions{Keep: 1})
	if e := asError(t, err); !strings.Contains(e.Message, "match endpoint required") {
		t.Errorf("unguarded: %v", err)
	}
	f = fakeZFS(pruneTrees())
	f.On("zfs destroy 'apool/src@s1%s5'", execx.Fail(1, "cannot destroy: busy"))
	c, rec = newClient(t, f)
	plan, err = c.Prune(ctx, "apool/src", &PruneOptions{GuardMode: GuardNone, Keep: 1, KeepFor: time.Hour, Name: "s*", Reclaim: 0, Grid: ""})
	if e := asError(t, err); e.Code != 1 || plan == nil || len(plan.Destroyed) != 0 || e.Message != "destroy command failed: apool/src@s1%s5" || !rec.has("WARN prune: cannot destroy: busy") {
		t.Errorf("failed destroy: %v %+v\n%s", err, plan, rec)
	}

	_, err = c.PlanPrune(ctx, "apool/none", nil)
	if e := asError(t, err); e.Message != "datasets inaccessible or do not exist" {
		t.Errorf("missing: %v", err)
	}
	if _, err = c.PlanPrune(ctx, "", nil); asError(t, err).Message != "missing endpoint" {
		t.Errorf("no endpoint: %v", err)
	}
	if _, err = c.PlanPrune(ctx, "apool/src", &PruneOptions{Grid: "bogus x"}); err == nil {
		t.Error("bad grid accepted")
	}
	if _, err = c.PlanPrune(ctx, "apool/src", &PruneOptions{GuardMode: "sometimes"}); err == nil {
		t.Error("bad guard mode accepted")
	}
	// Excluding @s1 keeps it, so the reclaim estimate needs written@s1.
	f.OnFunc(func(s string) bool { return strings.HasPrefix(s, "zfs get -Hpo name,value written@s1 ") }, func(s string) execx.Result {
		var out []string
		for _, n := range strings.Fields(s)[4:] {
			out = append(out, strings.Trim(n, "'")+"\t0")
		}
		return execx.Out(out...)
	})
	if _, err = c.PlanPrune(ctx, "apool/src", &PruneOptions{Reclaim: 15, Filter: Filter{Exclude: []string{"@s1"}}}); err != nil {
		t.Errorf("reclaim: %v", err)
	}
}

func TestPruneApproved(t *testing.T) {
	ctx := context.Background()
	trees := pruneTrees()
	f := fakeZFS(trees)
	f.On("zfs destroy 'apool/src@s1%s3'", execx.Out())
	c, rec := newClient(t, f)
	opts := &PruneOptions{Guard: "bpool/tgt", Keep: 1}
	seen, err := c.PlanPrune(ctx, "apool/src", opts)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}

	// The guard catches up, so a fresh plan also selects @s4; the user
	// never saw that, so only @s1..@s3 are destroyed.
	trees["bpool/tgt"] = append([]snap{{"@s6", "106", 0, trees["apool/src"][0].creation}}, trees["bpool/tgt"]...)
	plan, err := c.Prune(ctx, "apool/src", &PruneOptions{Guard: "bpool/tgt", Keep: 1, Approved: seen})
	if err != nil || len(plan.Skipped) != 0 || !reflect.DeepEqual(mutations(f), []string{"zfs destroy 'apool/src@s1%s3'"}) {
		t.Errorf("shifted window: %v %+v %q\n%s", err, plan, mutations(f), rec)
	}

	// The user unchecks @s2 and @s1 is replaced under the same name: only
	// @s3 is destroyed, and the replaced @s1 is reported.
	f = fakeZFS(trees)
	f.On("zfs destroy 'apool/src@s3'", execx.Out())
	c, _ = newClient(t, f)
	seen.Datasets[0].Destroy = []string{"@s1", "@s3", "@bogus"}
	trees["apool/src"][5].guid = "999"
	plan, err = c.Prune(ctx, "apool/src", &PruneOptions{Guard: "bpool/tgt", Keep: 1, Approved: seen})
	if err != nil || !reflect.DeepEqual(plan.Skipped, []string{"apool/src@s1"}) || !reflect.DeepEqual(mutations(f), []string{"zfs destroy 'apool/src@s3'"}) {
		t.Errorf("trimmed: %v %+v %q", err, plan, mutations(f))
	}
}

func TestLockFailoverPropsync(t *testing.T) {
	ctx := context.Background()
	f := execx.NewFake()
	c, rec := newClient(t, f)
	if err := c.Lock(ctx, "pool/svc", &LockOptions{ForceUnmount: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Lock(ctx, "pool/svc2", &LockOptions{NoUnmount: true, DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Unlock(ctx, "h:pool/x", &BasicOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if err := c.Failover(ctx, "a:pool/svc", "b:pool/svc", &LockOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{
		"INFO lock: + zfs set readonly=on 'pool/svc'",
		`INFO lock: + zfs list -Hpr -o name,mounted -t filesystem -Screatetxg 'pool/svc' | while read ds mounted; do [ "$mounted" = yes ] && zfs unmount -f "$ds"; done`,
		`INFO unlock: + ssh -n h "zfs inherit readonly 'pool/x'"`,
		`INFO failover: + ssh -n a "zfs set readonly=on 'pool/svc'"`,
		`INFO failover: + ssh -n b "zfs mount -R 'pool/svc'"`,
	} {
		if !rec.has(line) {
			t.Errorf("missing %q in\n%s", line, rec)
		}
	}
	if strings.Contains(rec.String(), "pool/svc2' | while read") {
		t.Errorf("NoUnmount ignored:\n%s", rec)
	}
	if len(f.Calls) != 0 {
		t.Errorf("dry run executed %q", f.Calls)
	}

	err := c.Propsync(ctx, "pool/a", "", nil)
	if e := asError(t, err); e.Message != "zelta propsync requires SOURCE and TARGET" {
		t.Errorf("propsync: %#v", e)
	}
	if err := c.Unlock(ctx, "", nil); asError(t, err).Message != "zelta unlock requires at least one dataset" {
		t.Errorf("unlock: %v", err)
	}
	if err := c.Lock(ctx, "", nil); err == nil {
		t.Error("lock without endpoint")
	}
	if err := c.Failover(ctx, "a", "", nil); err == nil {
		t.Error("failover without target")
	}
}

func TestFailoverFinalBackupLogs(t *testing.T) {
	// The final backup runs nested in-process; its messages must reach the
	// client's logger rather than any process stream.
	f := backupFake()
	f.OnRegexp(`^zfs (set|list|inherit|mount|get -Hpr -s local -t filesystem,volume -o name,property,value all) `, execx.Out())
	f.On("zfs snapshot -r 'apool/src"+defaultSnap+"'", execx.Out())
	c, rec := newClient(t, f)
	if err := c.Failover(context.Background(), "apool/src", "bpool/tgt", nil); err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	noUnmatched(t, f, rec)
	for _, line := range []string{"INFO failover: syncing 1 datasets", "INFO failover: 3K sent, 2 streams received in 1 seconds"} {
		if !rec.has(line) {
			t.Errorf("nested backup did not log %q through the client:\n%s", line, rec)
		}
	}
	if got := mutations(f); len(got) != 6 || got[0] != "zfs set readonly=on 'apool/src'" {
		t.Errorf("failover steps = %q", got)
	}
}

func TestCloneRevertRebase(t *testing.T) {
	ctx := context.Background()
	f := execx.NewFake()
	c, _ := newClient(t, f)
	if err := c.Clone(ctx, "a/b", "a/c", &CloneOptions{OriginBackup: "x/y"}); err == nil || !strings.Contains(err.Error(), "both") {
		t.Errorf("clone: %v", err)
	}
	if err := c.Clone(ctx, "a/b", "", nil); asError(t, err).Message != "missing target endpoint argument" {
		t.Errorf("clone: %v", err)
	}
	if err := c.Clone(ctx, "a/b", "a/c", &CloneOptions{OriginBackup: "x/y", TargetBackup: "x/z", Filter: Filter{Depth: 1}}); err == nil {
		t.Error("clone of a missing dataset succeeded")
	}
	if err := c.Revert(ctx, "", nil); asError(t, err).Message != "no endpoints given" {
		t.Errorf("revert: %v", err)
	}
	if err := c.Revert(ctx, "a/b", &RevertOptions{Force: true}); err == nil {
		t.Error("revert of a missing dataset succeeded")
	}
	if err := c.Rebase(ctx, "a/b", "h:a/c", nil); asError(t, err).Message != "rebase requires UPSTREAM and TARGET on the same host" {
		t.Errorf("rebase: %v", err)
	}
	if _, err := c.Rotate(ctx, "a/b", "", nil); asError(t, err).Op != "rotate" {
		t.Errorf("rotate: %v", err)
	}
}

func writeConf(t *testing.T, conf string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "zelta.conf")
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestPolicy(t *testing.T) {
	ctx := context.Background()
	path := writeConf(t, "S:\n  localhost:\n  - apool/src: bpool/tgt\n")
	f := backupFake()
	c, rec := newClient(t, f)
	if err := c.Policy(ctx, &PolicyOptions{Config: path, DryRun: true, Jobs: 1}); err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	if !rec.has("INFO policy: + zelta backup 'localhost:apool/src' 'bpool/tgt'") {
		t.Errorf("dry-run table:\n%s", rec)
	}
	rec.lines = nil
	if err := c.Policy(ctx, &PolicyOptions{Config: path}, "S"); err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	noUnmatched(t, f, rec)
	if len(f.CallsMatching("zfs recv")) != 2 || !strings.Contains(rec.String(), "streams received") {
		t.Errorf("policy run:\n%s\n%q", rec, f.Calls)
	}
	err := c.Policy(ctx, &PolicyOptions{Config: path}, "nope")
	if e := asError(t, err); e.Message != "policy object(s) not found: nope" {
		t.Errorf("selector: %v", err)
	}
}

func TestEnvironment(t *testing.T) {
	dir := t.TempDir()
	env := filepath.Join(dir, "zelta.env")
	if err := os.WriteFile(env, []byte("RECV_TOP=-o fromfile=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZELTA_ENV", env)
	t.Setenv("ZELTA_SEND_DEFAULT", "-L")
	ctx := context.Background()
	opts := &BackupOptions{DryRun: true}

	f := backupFake()
	c, rec := newClient(t, f)
	if _, err := c.Backup(ctx, "apool/src", "bpool/tgt", opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.String(), "zfs send -P -L -c -e 'apool/src@a'|zfs recv -v -o readonly=on ") {
		t.Errorf("hermetic client read the environment:\n%s", rec)
	}

	f = backupFake()
	c, rec = newClient(t, f, WithEnvironment())
	if _, err := c.Backup(ctx, "apool/src", "bpool/tgt", opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.String(), "zfs send -P -L 'apool/src@a'|zfs recv -v -o fromfile=1 ") {
		t.Errorf("environment ignored:\n%s", rec)
	}

	f = backupFake()
	c, rec = newClient(t, f, WithEnvironment(), WithSettings(map[string]string{"SEND_DEFAULT": "-c"}))
	if _, err := c.Backup(ctx, "apool/src", "bpool/tgt", opts); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rec.String(), "zfs send -P -c 'apool/src@a'") {
		t.Errorf("client settings must beat the environment:\n%s", rec)
	}

	t.Setenv("ZELTA_DEPTH", "bogus")
	f = backupFake()
	c, _ = newClient(t, f, WithEnvironment())
	if _, err := c.Backup(ctx, "apool/src", "bpool/tgt", opts); err == nil {
		t.Error("environment DEPTH ignored")
	}
}

func TestRemoteCommandDerivesDefaults(t *testing.T) {
	f := execx.NewFake()
	c, rec := newClient(t, f, WithRemoteCommand("ssh -p 2222"))
	if err := c.Lock(context.Background(), "h:pool/x", &LockOptions{DryRun: true}); err != nil {
		t.Fatal(err)
	}
	if !rec.has(`INFO lock: + ssh -p 2222 -n h "zfs set readonly=on 'pool/x'"`) {
		t.Errorf("remote command:\n%s", rec)
	}
}

func TestMultiplexedRemote(t *testing.T) {
	// Remote operations go through ssh multiplexing unless disabled; with a
	// fake executor the master cannot start and commands run directly.
	f := execx.NewFake().OnPrefix("ssh -n h ", execx.Out("h:pool/x\t1"))
	f.OnPrefix("ssh ", execx.Fail(255, "no master"))
	c, _ := newClient(t, f)
	c.Unlock(context.Background(), "h:pool/x", nil)
	if len(f.Calls) == 0 {
		t.Error("no commands ran")
	}
}

func TestErrorsAndCancel(t *testing.T) {
	e := &Error{Op: "backup", Code: 2}
	if e.Error() != "zelta backup: exit status 2" || e.Unwrap() != nil {
		t.Errorf("error = %q", e.Error())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := fakeZFS(matchTrees())
	c, _ := newClient(t, f)
	_, err := c.Match(ctx, "apool/src", "bpool/tgt", nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("canceled match: %v", err)
	}
}

func TestLogging(t *testing.T) {
	tests := []struct {
		level slog.Level
		want  int
	}{{LevelTrace, 4}, {slog.LevelDebug, 3}, {slog.LevelInfo, 2}, {slog.LevelWarn, 1}, {slog.LevelError, 1}}
	for _, tt := range tests {
		c, _ := New(WithLogger(slog.New(&recorder{level: tt.level})))
		if got := c.level(); got != tt.want {
			t.Errorf("level(%v) = %d, want %d", tt.level, got, tt.want)
		}
	}
	if c, _ := New(); c.level() != 1 {
		t.Error("default level")
	}

	// Without a logger, nothing is printed and errors are still reported.
	f := fakeZFS(nil)
	c, _ := New(withExecutor(f))
	stdout, stderr := os.Stdout, os.Stderr
	r, w, _ := os.Pipe()
	os.Stdout, os.Stderr = w, w
	_, err := c.Match(context.Background(), "apool/x", "", nil)
	os.Stdout, os.Stderr = stdout, stderr
	w.Close()
	var buf bytes.Buffer
	buf.ReadFrom(r)
	if buf.Len() != 0 {
		t.Errorf("wrote to process streams: %q", buf.String())
	}
	if e := asError(t, err); len(e.Messages) != 1 {
		t.Errorf("messages = %q", e.Messages)
	}

	var lines []string
	lw := &lineWriter{emit: func(s string) { lines = append(lines, s) }}
	lw.Write([]byte("a\r\nb"))
	lw.Write([]byte("\n\nc"))
	if !reflect.DeepEqual(lines, []string{"a", "b"}) {
		t.Errorf("lines = %q", lines)
	}
}
