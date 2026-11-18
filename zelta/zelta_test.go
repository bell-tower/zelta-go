package zelta

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"zelta.space/internal/execx"
)

var testNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// recorder is an slog.Handler that keeps records as "LEVEL op: message".
type recorder struct {
	mu    sync.Mutex
	level slog.Level
	lines []string
}

func (r *recorder) Enabled(_ context.Context, l slog.Level) bool { return l >= r.level }
func (r *recorder) WithAttrs([]slog.Attr) slog.Handler           { return r }
func (r *recorder) WithGroup(string) slog.Handler                { return r }
func (r *recorder) Handle(_ context.Context, rec slog.Record) error {
	op := ""
	rec.Attrs(func(a slog.Attr) bool {
		if a.Key == "op" {
			op = a.Value.String()
		}
		return true
	})
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, fmt.Sprintf("%s %s: %s", rec.Level, op, rec.Message))
	return nil
}

func (r *recorder) has(line string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Contains(r.lines, line)
}

func (r *recorder) String() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return strings.Join(r.lines, "\n")
}

// snap is a listed dataset ("" savepoint) or snapshot.
type snap struct {
	savepoint string
	guid      string
	written   int64
	creation  int64
}

var listRe = regexp.MustCompile(`^zfs list -Hpr -o (\S+) -t all -Screatetxg(?: -d\d+)? '([^']+)'$`)

// fakeZFS answers "zfs list" for the given trees (newest first) and any
// other command from extra rules registered on the returned fake.
func fakeZFS(trees map[string][]snap) *execx.Fake {
	f := execx.NewFake()
	f.OnFunc(listRe.MatchString, func(s string) execx.Result {
		m := listRe.FindStringSubmatch(s)
		rows, ok := trees[m[2]]
		if !ok {
			return execx.Fail(1, "cannot open '"+m[2]+"': dataset does not exist")
		}
		var out []string
		for _, r := range rows {
			var cells []string
			for p := range strings.SplitSeq(m[1], ",") {
				switch p {
				case "name":
					cells = append(cells, m[2]+r.savepoint)
				case "guid":
					cells = append(cells, r.guid)
				case "written":
					cells = append(cells, fmt.Sprint(r.written))
				case "creation":
					cells = append(cells, fmt.Sprint(r.creation))
				case "clones":
					cells = append(cells, "-")
				default:
					cells = append(cells, "0")
				}
			}
			out = append(out, strings.Join(cells, "\t"))
		}
		return execx.Out(out...)
	})
	return f
}

func newClient(t *testing.T, f *execx.Fake, opts ...Option) (*Client, *recorder) {
	t.Helper()
	rec := &recorder{level: LevelTrace}
	opts = append([]Option{withExecutor(f), withClock(func() time.Time { return testNow }), WithLogger(slog.New(rec))}, opts...)
	c, err := New(opts...)
	if err != nil {
		t.Fatal(err)
	}
	return c, rec
}

func noUnmatched(t *testing.T, f *execx.Fake, rec *recorder) {
	t.Helper()
	if len(f.Unmatched) > 0 {
		t.Fatalf("unmatched commands:\n%s\nlog:\n%s", strings.Join(f.Unmatched, "\n"), rec)
	}
}

func mutations(f *execx.Fake) []string {
	var out []string
	for _, c := range f.Calls {
		if !strings.Contains(c, "zfs list ") && !strings.Contains(c, "zfs get ") {
			out = append(out, c)
		}
	}
	return out
}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("error %v (%T) is not *Error", err, err)
	}
	return e
}

func TestNewValidatesSettings(t *testing.T) {
	if _, err := New(WithSettings(map[string]string{"ZELTA_RECV_TOP": "x", "SNAP_PREFIX": "y"})); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]string{
		"recv_top": "invalid option name", "1X": "invalid option name", "": "invalid option name",
		"LOG_MODE": "managed by the client", "ZELTA_VERB": "managed by the client",
	} {
		_, err := New(WithSettings(map[string]string{key: "1"}))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%q: err = %v, want %q", key, err, want)
		}
	}
	c, _ := New()
	if _, err := c.Match(context.Background(), "a/b", "", &MatchOptions{Settings: map[string]string{"LOG_FILE": "/tmp/x"}}); err == nil || !strings.Contains(err.Error(), "managed") {
		t.Errorf("operation settings are validated: %v", err)
	}
	if Version() == "" {
		t.Error("empty version")
	}
}

func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		in   string
		want Endpoint
		str  string
	}{
		{"tank/home", Endpoint{Dataset: "tank/home"}, "tank/home"},
		{"tank/home@snap1", Endpoint{Dataset: "tank/home", Snapshot: "snap1"}, "tank/home@snap1"},
		{"host:pool/ds", Endpoint{Host: "host", Dataset: "pool/ds"}, "host:pool/ds"},
		{"me@host:pool/ds@s", Endpoint{User: "me", Host: "host", Dataset: "pool/ds", Snapshot: "s"}, "me@host:pool/ds@s"},
		{"me@[fe80::1]:pool/ds", Endpoint{User: "me", Host: "fe80::1", Dataset: "pool/ds"}, "me@[fe80::1]:pool/ds"},
		{"localhost:pool/a:b", Endpoint{Dataset: "pool/a:b"}, "localhost:pool/a:b"},
	}
	for _, tt := range tests {
		got, err := ParseEndpoint(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseEndpoint(%q) = %+v, %v; want %+v", tt.in, got, err, tt.want)
		}
		if s := got.String(); s != tt.str {
			t.Errorf("%+v.String() = %q, want %q", got, s, tt.str)
		}
		if got.IsRemote() != (tt.want.Host != "") {
			t.Errorf("%q IsRemote = %v", tt.in, got.IsRemote())
		}
	}
	if _, err := ParseEndpoint("host:"); err == nil {
		t.Error("endpoint without dataset accepted")
	}
}

func matchTrees() map[string][]snap {
	return map[string][]snap{
		"apool/src": {{"@c", "3", 30, 300}, {"@b", "2", 20, 200}, {"@a", "1", 10, 100}, {"", "9", 60, 50},
			{"/kid@b", "22", 5, 200}, {"/kid", "8", 5, 50}},
		"bpool/tgt": {{"@b", "2", 0, 200}, {"@a", "1", 0, 100}, {"", "7", 0, 50},
			{"/old@x", "77", 0, 90}, {"/old", "6", 0, 50}},
	}
}

func TestMatch(t *testing.T) {
	f := fakeZFS(matchTrees())
	c, rec := newClient(t, f)
	res, err := c.Match(context.Background(), "apool/src", "bpool/tgt", &MatchOptions{Filter: Filter{Depth: 0}})
	if err != nil {
		t.Fatal(err)
	}
	noUnmatched(t, f, rec)
	if res.UpToDate != 0 || res.Syncable != 2 || res.Blocked != 1 || res.Summary != "2 syncable, 1 blocked" {
		t.Errorf("counts = %+v", res)
	}
	if len(res.Datasets) != 3 {
		t.Fatalf("datasets = %+v", res.Datasets)
	}
	top, kid, old := res.Datasets[0], res.Datasets[1], res.Datasets[2]
	if top.Suffix != "" || top.State != Syncable || top.Match != "@b" || top.Matches != 2 || top.TransferBytes != 30 || top.Info != "syncable (incremental)" {
		t.Errorf("top = %+v", top)
	}
	wantSrc := DatasetSnapshots{Name: "apool/src", Snapshots: 3, First: "@a", Last: "@c", Next: "@c", Newer: 1, Written: 60}
	if *top.Source != wantSrc {
		t.Errorf("top source = %+v", *top.Source)
	}
	if top.Target.Name != "bpool/tgt" || top.Target.Last != "@b" || top.Target.Newer != 0 {
		t.Errorf("top target = %+v", *top.Target)
	}
	if kid.Suffix != "/kid" || kid.State != Syncable || kid.Target != nil || kid.Info != "syncable (full)" {
		t.Errorf("kid = %+v", kid)
	}
	if old.Suffix != "/old" || old.State != Blocked || old.Source != nil || old.Target.Newer != 1 || old.Target.Next != "@x" {
		t.Errorf("old = %+v", old)
	}
	if !rec.has("DEBUG-4 match: `zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg 'apool/src'`") {
		t.Errorf("commands are traced:\n%s", rec)
	}

	// Up to date, without sizes, filtered.
	f = fakeZFS(map[string][]snap{"apool/src": {{"@b", "2", 0, 2}, {"", "9", 0, 1}}, "bpool/tgt": {{"@b", "2", 0, 2}, {"", "8", 0, 1}}})
	c, rec = newClient(t, f)
	res, err = c.Match(context.Background(), "apool/src", "bpool/tgt", &MatchOptions{NoSizes: true, Filter: Filter{Depth: 1, Exclude: []string{"@x"}, Include: []string{"@*"}}})
	if err != nil {
		t.Fatal(err)
	}
	if res.UpToDate != 1 || res.Datasets[0].State != UpToDate {
		t.Errorf("res = %+v", res)
	}
	if got := f.CallsMatching("'apool/src'"); len(got) != 1 || got[0] != "zfs list -Hpr -o name,guid -t all -Screatetxg -d1 'apool/src'" {
		t.Errorf("listing = %q", got)
	}
	_ = rec

	// Source alone.
	f = fakeZFS(matchTrees())
	c, _ = newClient(t, f)
	if res, err = c.Match(context.Background(), "apool/src", "", nil); err != nil || res.Syncable != 2 {
		t.Errorf("source only: %+v %v", res, err)
	}
}

func TestMatchErrors(t *testing.T) {
	f := fakeZFS(nil)
	c, rec := newClient(t, f)
	ctx := context.Background()
	_, err := c.Match(ctx, "apool/none", "bpool/none", nil)
	e := asError(t, err)
	if e.Op != "match" || e.Code != 1 || e.Message != "datasets inaccessible or do not exist" ||
		e.Error() != "zelta match: datasets inaccessible or do not exist" {
		t.Errorf("err = %#v", e)
	}
	if !rec.has("ERROR match: datasets inaccessible or do not exist") {
		t.Errorf("error is logged:\n%s", rec)
	}
	if _, err = c.Match(ctx, "", "", nil); asError(t, err).Message != "missing source endpoint" {
		t.Errorf("err = %v", err)
	}
	if _, err = c.Match(ctx, "apool/src", "", &MatchOptions{Filter: Filter{Depth: -1}}); !strings.Contains(fmt.Sprint(err), "depth of '-1' invalid") {
		t.Errorf("err = %v", err)
	}
	if _, err = c.Match(ctx, "host:", "", nil); !strings.Contains(fmt.Sprint(err), "invalid endpoint 'host:'") {
		t.Errorf("err = %v", err)
	}
}

const fullRecvTop = "-o readonly=on -u -x mountpoint -o canmount=noauto -s"

func getCmd(ds, depth string) string {
	return "zfs get -Hpr -s local,none -t filesystem,volume -o name,property,value " + depth + "type,written,encryption,origin,receive_resume_token,snapshots_changed '" + ds + "'"
}

func received(kind string, size int) execx.Result {
	return execx.Out(fmt.Sprintf("size\t%d", size), "receiving "+kind+" stream of x@s into y@s", "received 1K stream in 0.5 seconds (2K/sec)")
}

func backupFake() *execx.Fake {
	f := fakeZFS(map[string][]snap{"apool/src": {{"@b", "2", 0, 2}, {"@a", "1", 0, 1}, {"", "9", 0, 1}}})
	f.On(getCmd("apool/src", ""), execx.Out("apool/src\ttype\tfilesystem", "apool/src\twritten\t0"))
	f.On(getCmd("bpool/tgt", ""), execx.Fail(1, "cannot open 'bpool/tgt': dataset does not exist"))
	f.On(getCmd("apool/src", "-d0 "), execx.Out("apool/src\ttype\tfilesystem", "apool/src\twritten\t0"))
	f.On(getCmd("bpool/tgt", "-d0 "), execx.Fail(1, "cannot open 'bpool/tgt': dataset does not exist"))
	f.On("{ zfs send -P -L -c -e 'apool/src@a'|zfs recv -v "+fullRecvTop+" 'bpool/tgt' ; }", received("full", 2048))
	f.On("{ zfs send -P -L -c -e -I 'apool/src@a' 'apool/src@b'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1024))
	return f
}

func TestBackup(t *testing.T) {
	f := backupFake()
	c, rec := newClient(t, f)
	res, err := c.Backup(context.Background(), "apool/src", "bpool/tgt", nil)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	noUnmatched(t, f, rec)
	want := &BackupResult{
		Datasets:    []DatasetResult{{Suffix: "", Source: "apool/src", Target: "bpool/tgt", Synced: true, UpToDate: true}},
		Streams:     []string{"apool/src@a", "apool/src@a%b"},
		StreamsSent: 2, StreamsReceived: 2, Bytes: 3072, ReceiveTime: time.Second,
	}
	if !reflect.DeepEqual(res, want) {
		t.Errorf("result = %+v\nwant     %+v", res, want)
	}
	if !res.UpToDate() || (&BackupResult{}).UpToDate() {
		t.Error("UpToDate()")
	}
	for _, line := range []string{"INFO backup: syncing 1 datasets", "INFO backup: 3K sent, 2 streams received in 1 seconds"} {
		if !rec.has(line) {
			t.Errorf("missing %q in\n%s", line, rec)
		}
	}
}

func TestBackupOptions(t *testing.T) {
	f := backupFake()
	f.On("zfs snapshot -r 'apool/src@manual'", execx.Out())
	c, rec := newClient(t, f, WithSettings(map[string]string{"RECV_TOP": "-o client=1"}))
	opts := &BackupOptions{
		Snapshot: SnapshotPolicy{Mode: SnapshotAlways, Name: "manual"},
		Parallel: 2, Direction: Push, Incremental: true, Bookmark: false, NoResume: true,
		DryRun:   true,
		Settings: map[string]string{"RECV_TOP": "-o op=1"},
	}
	res, err := c.Backup(context.Background(), "apool/src", "bpool/tgt", opts)
	if err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	if len(mutations(f)) != 0 {
		t.Errorf("dry run ran %q", mutations(f))
	}
	if res.Snapshot != "@manual" {
		t.Errorf("snapshot = %q", res.Snapshot)
	}
	for _, line := range []string{
		"INFO backup: + zfs snapshot -r 'apool/src@manual'",
		"INFO backup: + { zfs send -P -L -c -e 'apool/src@manual'|zfs recv -v -o op=1 -u -x mountpoint -o canmount=noauto 'bpool/tgt' ; }",
	} {
		if !rec.has(line) {
			t.Errorf("missing %q in\n%s", line, rec)
		}
	}

	// Replicate selects the replicate verb; Rotate refuses it.
	f = backupFake()
	c, rec = newClient(t, f)
	if _, err := c.Backup(context.Background(), "apool/src", "bpool/tgt", &BackupOptions{Replicate: true, DryRun: true}); err != nil {
		t.Fatalf("%v\n%s", err, rec)
	}
	if !strings.Contains(rec.String(), "zfs send -P --raw -s -R") {
		t.Errorf("replicate send:\n%s", rec)
	}
	if _, err := c.Rotate(context.Background(), "a/b", "c/d", &BackupOptions{Replicate: true}); err == nil {
		t.Error("Rotate accepted Replicate")
	}
}

func TestBackupFailure(t *testing.T) {
	f2 := fakeZFS(map[string][]snap{"apool/src": {{"@a", "1", 0, 1}, {"", "9", 0, 1}}})
	f2.On(getCmd("apool/src", ""), execx.Out("apool/src\ttype\tfilesystem", "apool/src\twritten\t0"))
	f2.On(getCmd("bpool/tgt", ""), execx.Fail(1, "cannot open 'bpool/tgt': dataset does not exist"))
	f2.On("{ zfs send -P -L -c -e 'apool/src@a'|zfs recv -v "+fullRecvTop+" 'bpool/tgt' ; }", execx.Fail(1, "cannot receive: permission denied"))
	c, rec := newClient(t, f2)
	res, err := c.Backup(context.Background(), "apool/src", "bpool/tgt", nil)
	e := asError(t, err)
	if e.Code != 2 || res == nil || res.Datasets[0].Synced || res.Datasets[0].UpToDate {
		t.Errorf("err = %#v res = %+v", e, res)
	}
	if !strings.Contains(e.Message, "sync command failed before receiving stream") || len(e.Messages) != 1 {
		t.Errorf("messages = %q\n%s", e.Messages, rec)
	}
	if _, err := c.Backup(context.Background(), "apool/src", "", nil); asError(t, err).Code != 1 {
		t.Errorf("missing target: %v", err)
	}
}
