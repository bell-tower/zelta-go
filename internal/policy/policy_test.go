package policy

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zelta.space/internal/app"
	"zelta.space/internal/config"
	"zelta.space/internal/execx"
)

var ctx = context.Background()

// The policy tests run against the shipped example configuration.
const centralized = "../../examples/policy/centralized/zelta.yaml"

// writeConf writes a policy file (and optional extra files) into a temp dir
// and returns the policy path.
func writeConf(t *testing.T, conf string, extra map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range extra {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, "zelta.conf")
	if err := os.WriteFile(path, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func newRT(opts map[string]string) *app.TestRuntime { return app.NewTest("policy", opts) }

func golden(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestDryRunMatchesAwk(t *testing.T) {
	tr := newRT(map[string]string{"CONFIG": centralized, "DRYRUN": "1"})
	if err := Run(ctx, tr.Runtime, nil, Deps{}); err != nil {
		t.Fatal(err)
	}
	if got, want := tr.Out.String(), golden(t, "centralized.table.golden"); got != want {
		t.Errorf("table mismatch:\n%s\nwant:\n%s", got, want)
	}

	tr = newRT(map[string]string{"CONFIG": centralized, "DRYRUN": "1", "NO_HEADER": "1"})
	Run(ctx, tr.Runtime, []string{"NYC1", "host1.client0"}, Deps{})
	if got, want := tr.Out.String(), golden(t, "centralized.H.golden"); got != want {
		t.Errorf("-H mismatch:\n%s\nwant:\n%s", got, want)
	}

	tr = newRT(map[string]string{"CONFIG": centralized, "DRYRUN": "1", "LOG_LEVEL": "3"})
	Run(ctx, tr.Runtime, []string{"fw2.dal1"}, Deps{})
	if got, want := tr.Out.String(), golden(t, "centralized.verbose.golden"); got != want {
		t.Errorf("verbose mismatch:\n%s\nwant:\n%s", got, want)
	}
	if strings.Contains(tr.Err.String(), "warning") {
		t.Errorf("unexpected warnings: %s", tr.Err.String())
	}
}

func TestShortTableHeader(t *testing.T) {
	path := writeConf(t, "S:\n  h:\n  - p: x:y\n", nil)
	tr := newRT(map[string]string{"CONFIG": path, "DRYRUN": "1"})
	Run(ctx, tr.Runtime, nil, Deps{})
	if got := tr.Out.String(); got != "SOURCE  TARGET\nh:p     x:y\n" {
		t.Errorf("table = %q", got)
	}
}

func TestSelectionErrors(t *testing.T) {
	tr := newRT(map[string]string{"CONFIG": centralized, "DRYRUN": "1"})
	err := Run(ctx, tr.Runtime, []string{"nope", "other"}, Deps{})
	if app.Code(err) != 1 || err.Error() != "policy object(s) not found: nope, other" {
		t.Errorf("not found: %v", err)
	}
	path := writeConf(t, "# nothing here\nDEPTH: 1\n", nil)
	tr = newRT(map[string]string{"CONFIG": path})
	err = Run(ctx, tr.Runtime, nil, Deps{})
	var ue *app.UsageError
	if !errors.As(err, &ue) || err.Error() != "no datasets defined in "+path {
		t.Errorf("no datasets: %v", err)
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		conf, want string
	}{
		{"  host:\n", "configuration parse error at %s:1: host:"},
		{"S:\n  h:\n      DEPTH: 1\n", "configuration parse error at %s:3: DEPTH: 1"},
		{"S:\n  - pool/ds  # comment\n", "configuration parse error at %s:2: - pool/ds  # comment"},
		{"S:\n  h:\n bad line\n", "configuration parse error at %s:3: bad line"},
		{"BOGUS: 1\n", "unknown option: BOGUS"},
		{"S:\n  h:\n    options:\n      prune-num: 3\n", "unknown option: PRUNE_NUM"},
	}
	for _, tt := range tests {
		path := writeConf(t, tt.conf, nil)
		tr := newRT(map[string]string{"CONFIG": path})
		err := Run(ctx, tr.Runtime, nil, Deps{})
		want := strings.Replace(tt.want, "%s", path, 1)
		var ue *app.UsageError
		if !errors.As(err, &ue) || err.Error() != want {
			t.Errorf("conf %q: err = %v, want %q", tt.conf, err, want)
		}
	}
}

func TestImports(t *testing.T) {
	path := writeConf(t, strings.Join([]string{
		"SITE0:",
		"  host1.example:",
		"    options:",
		"      import: targets/vault1.yaml  # comment",
		"    datasets:",
		"      import: sources/host1.yaml",
	}, "\n"), map[string]string{
		"targets/vault1.yaml": "BACKUP_ROOT: vault1:tank/Backups\nimport: ../rules/prefix.yaml\n",
		"rules/prefix.yaml":   "ADD_HOST_PREFIX: 1\n",
		"sources/host1.yaml":  "- tank/vm/app1\n\n- tank/vm/app2: other:pool/x\n",
	})
	tr := newRT(map[string]string{"CONFIG": path, "DRYRUN": "1", "NO_HEADER": "1"})
	if err := Run(ctx, tr.Runtime, nil, Deps{}); err != nil {
		t.Fatal(err)
	}
	want := "host1.example:tank/vm/app1 vault1:tank/Backups/host1.example/app1\nhost1.example:tank/vm/app2 other:pool/x\n"
	if got := tr.Out.String(); got != want {
		t.Errorf("imports = %q", got)
	}

	errorCases := []struct {
		files map[string]string
		conf  string
		want  string
	}{
		{map[string]string{"a.yaml": "import: zelta.conf\n"}, "import: a.yaml\n", "recursive import detected: "},
		{map[string]string{"empty.yaml": ""}, "import: empty.yaml\n", "empty or unreadable policy file: "},
		{nil, "import: /nonexistent/x.yaml\n", "empty or unreadable policy file: /nonexistent/x.yaml"},
		{map[string]string{"long.yaml": strings.Repeat("x", 1<<20+1)}, "import: long.yaml\n", "unreadable policy file: "},
	}
	for _, c := range errorCases {
		path := writeConf(t, c.conf, c.files)
		err := Run(ctx, newRT(map[string]string{"CONFIG": path}).Runtime, nil, Deps{})
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%q: err = %v", c.conf, err)
		}
	}

	files := map[string]string{}
	for i := range 10 {
		files[filepathName(i)] = "import: " + filepathName(i+1) + "\n"
	}
	path = writeConf(t, "import: "+filepathName(0)+"\n", files)
	if err := Run(ctx, newRT(map[string]string{"CONFIG": path}).Runtime, nil, Deps{}); err == nil || !strings.HasPrefix(err.Error(), "import depth exceeded near ") {
		t.Errorf("depth: %v", err)
	}
	if err := Run(ctx, newRT(map[string]string{"CONFIG": "/nonexistent/zelta.conf"}).Runtime, nil, Deps{}); err == nil ||
		err.Error() != "empty or unreadable policy file: /nonexistent/zelta.conf" {
		t.Errorf("missing config: %v", err)
	}
}

func filepathName(i int) string { return "f" + string(rune('a'+i)) + ".yaml" }

func TestPathHelpers(t *testing.T) {
	for in, want := range map[string]string{"zelta.conf": ".", "/zelta.conf": "/", "a/b/c.yaml": "a/b"} {
		if got := fileDir(in); got != want {
			t.Errorf("fileDir(%q) = %q", in, got)
		}
	}
	if resolveImport("/abs.yaml", "x/y.yaml") != "/abs.yaml" || resolveImport("r.yaml", "zelta.conf") != "./r.yaml" {
		t.Errorf("resolveImport")
	}
	if got := fields("    - pool/ds: host:tank/x"); strings.Join(got, "|") != "||pool/ds|host:tank/x" {
		t.Errorf("fields = %q", got)
	}
	if got := fields("SITE:"); strings.Join(got, "|") != "SITE|" {
		t.Errorf("fields = %q", got)
	}
}

func TestResolveTarget(t *testing.T) {
	j := &Job{Host: "web1", Source: "sink/source/dataset"}
	tests := []struct {
		target string
		opts   map[string]string
		want   string
	}{
		{"explicit:pool/x", map[string]string{"BACKUP_ROOT": "tank/backups"}, "explicit:pool/x"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups"}, "tank/backups/dataset"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups", "ADD_DATASET_PREFIX": "0"}, "tank/backups/dataset"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups", "ADD_DATASET_PREFIX": "1"}, "tank/backups/source/dataset"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups", "ADD_DATASET_PREFIX": "-1"}, "tank/backups/sink/source/dataset"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups", "ADD_DATASET_PREFIX": "9"}, "tank/backups/sink/source/dataset"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups", "ADD_HOST_PREFIX": "1"}, "tank/backups/web1/dataset"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups", "ADD_HOST_PREFIX": "0"}, "tank/backups/dataset"},
		{"", map[string]string{"BACKUP_ROOT": "tank/backups", "ADD_HOST_PREFIX": "1", "ADD_DATASET_PREFIX": "1"}, "tank/backups/web1/source/dataset"},
		{"", map[string]string{}, ""},
	}
	for _, tt := range tests {
		if got := resolveTarget(tt.target, tt.opts, j); got != tt.want {
			t.Errorf("resolveTarget(%q, %v) = %q want %q", tt.target, tt.opts, got, tt.want)
		}
	}
}

const selectConf = `BACKUP_ROOT: bk:tank/Backups
NYC:
  user@web1.example:
  - zroot/jail/www
  db1:
  - zroot/db: other:tank/db
DAL:
  db2:
  - zroot/db
  - zroot/notarget
`

func TestOperandMatching(t *testing.T) {
	path := writeConf(t, selectConf, nil)
	tests := map[string]int{
		"NYC":                     2,
		"web1.example":            1,
		"user@web1.example":       1,
		"zroot/db":                2,
		"other:tank/db":           1,
		"bk:tank/Backups/db":      1,
		"db1:zroot/db":            1,
		"www":                     1,
		"db":                      2,
		"user@web1.example:zroot": 0,
	}
	for op, want := range tests {
		tr := newRT(map[string]string{"CONFIG": path, "DRYRUN": "1", "NO_HEADER": "1"})
		err := Run(ctx, tr.Runtime, []string{op}, Deps{})
		got := strings.Count(tr.Out.String(), "\n")
		if got != want || (want == 0) != (err != nil) {
			t.Errorf("operand %q selected %d jobs (err %v), want %d", op, got, err, want)
		}
	}
}

func TestOptionNormalization(t *testing.T) {
	path := writeConf(t, strings.Join([]string{
		"THREADS: 3",
		"intermediate: no",
		"zelta-snap-mode: ALWAYS",
		"RESUME: yes",
		"LOG_LEVEL: x",
		"RETRY: 2",
		"DEPTH: 4",
		"S:",
		"  BACKUP_ROOT: bk:tank",
		"  h:",
		"    options:",
		"      LOG_LEVEL: 3",
		"      send-intr: TRUE",
		"    datasets:",
		"    - pool/a",
	}, "\n"), nil)
	tr := newRT(map[string]string{"CONFIG": path, "DEPTH": "9", "LOG_LEVEL": "4"})
	p := newParser(tr.Opts, tr.Log, nil)
	lines, err := loadLines(path, 0, map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.parse(lines); err != nil {
		t.Fatal(err)
	}
	j := p.jobs[0]
	want := map[string]string{"JOBS": "3", "SEND_INTR": "1", "SNAP_MODE": "ALWAYS", "RESUME": "1", "RETRY": "2", "BACKUP_ROOT": "bk:tank"}
	for k, v := range want {
		if j.Opts[k] != v {
			t.Errorf("%s = %q want %q", k, j.Opts[k], v)
		}
	}
	if _, ok := j.Opts["DEPTH"]; ok {
		t.Errorf("command-line DEPTH must override the policy file")
	}
	if _, ok := j.Opts["LOG_LEVEL"]; ok {
		t.Errorf("command-line LOG_LEVEL must override the policy file")
	}
	if p.global["JOBS"] != "3" || j.Target != "bk:tank/a" {
		t.Errorf("global = %v target = %q", p.global, j.Target)
	}
	errs := tr.Err.String()
	for _, w := range []string{"reassigning the option 'THREADS' to 'JOBS'", "skipping DEPTH; already in args/user env"} {
		if !strings.Contains(errs, w) {
			t.Errorf("missing %q in %q", w, errs)
		}
	}

	tr = newRT(map[string]string{"CONFIG": path})
	p = newParser(tr.Opts, tr.Log, nil)
	p.parse(lines)
	if p.jobs[0].Opts["LOG_LEVEL"] != "3" || p.jobs[0].Opts["DEPTH"] != "4" {
		t.Errorf("policy LOG_LEVEL/DEPTH = %v", p.jobs[0].Opts)
	}
	if !strings.Contains(tr.Err.String(), "warning: option 'LOG_LEVEL' is an integer; 'x' invalid") {
		t.Errorf("stderr = %q", tr.Err.String())
	}
}

// recorder is a Backup stub that records each job's options.
type recorder struct {
	mu   sync.Mutex
	runs []string
	opts map[string]*config.Options
	fail map[string]int // failures remaining per target
}

func newRecorder() *recorder {
	return &recorder{opts: map[string]*config.Options{}, fail: map[string]int{}}
}

func (r *recorder) backup(_ context.Context, rt *app.Runtime, args []string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.runs = append(r.runs, strings.Join(args, " -> "))
	r.opts[args[1]] = rt.Opts
	if r.fail[args[1]] > 0 {
		r.fail[args[1]]--
		return app.Fail("sync failed")
	}
	rt.Log.Noticef("12 datasets up-to-date")
	return nil
}

func TestRunJobs(t *testing.T) {
	path := writeConf(t, "BACKUP_SITE:\n  localhost:\n    datasets:\n    - apool/treetop: bpool/backups\n", nil)
	rec := newRecorder()
	tr := newRT(map[string]string{"CONFIG": path})
	if err := Run(ctx, tr.Runtime, nil, Deps{Backup: rec.backup, CLI: map[string]string{"DEPTH": "2", "RETRY": "5"}}); err != nil {
		t.Fatal(err)
	}
	if got := tr.RawOut.String(); got != "[BACKUP_SITE: bpool/backups] localhost:apool/treetop: 12 datasets up-to-date\n" {
		t.Errorf("job output = %q", got)
	}
	o := rec.opts["bpool/backups"]
	if o.Get("VERB") != "backup" || o.Get("DEPTH") != "2" || o.Get("RETRY") == "5" || o.Get("LOG_PREFIX") == "" {
		t.Errorf("job options: %v", o.Environ())
	}

	rec = newRecorder()
	rec.fail["bpool/backups"] = 1
	tr = newRT(map[string]string{"CONFIG": path})
	if err := Run(ctx, tr.Runtime, nil, Deps{Backup: rec.backup, CLI: map[string]string{"VERB": "replicate"}}); app.Code(err) != 1 {
		t.Errorf("failure without retry: %v", err)
	}
	if len(rec.runs) != 1 || rec.opts["bpool/backups"].Get("VERB") != "backup" {
		t.Errorf("runs = %v", rec.runs)
	}
	if !strings.Contains(tr.RawErr.String(), "error: [BACKUP_SITE: bpool/backups] localhost:apool/treetop: sync failed") {
		t.Errorf("job error = %q", tr.RawErr.String())
	}

	err := Run(ctx, newRT(map[string]string{"CONFIG": path}).Runtime, nil, Deps{})
	if app.Code(err) != 1 {
		t.Errorf("nil backup must fail: %v", err)
	}
}

func TestRetries(t *testing.T) {
	path := writeConf(t, "RETRY: 2\nA:\n  h:\n  - p/a: t/a\n  - p/b: t/b\nB:\n  RETRY: 0\n  h:\n  - p/c: t/c\n", nil)
	rec := newRecorder()
	rec.fail["t/a"] = 2
	rec.fail["t/b"] = 5
	rec.fail["t/c"] = 1
	tr := newRT(map[string]string{"CONFIG": path})
	err := Run(ctx, tr.Runtime, nil, Deps{Backup: rec.backup})
	if app.Code(err) != 1 {
		t.Fatalf("err = %v", err)
	}
	count := map[string]int{}
	for _, r := range rec.runs {
		count[r]++
	}
	if count["h:p/a -> t/a"] != 3 || count["h:p/b -> t/b"] != 3 || count["h:p/c -> t/c"] != 1 {
		t.Errorf("runs = %v", count)
	}
	if n := strings.Count(tr.Out.String(), "retrying: [A: t/a] h:p/a"); n != 2 {
		t.Errorf("retry notices = %q", tr.Out.String())
	}
}

func TestConcurrentSites(t *testing.T) {
	path := writeConf(t, "JOBS: 2\nA:\n  h:\n  - p/a: t/a\n  - p/a2: t/a2\nB:\n  h:\n  - p/b: t/b\nC:\n  h:\n  - p/c: t/c\n", nil)
	var running, peak int32
	release := make(chan struct{})
	var once sync.Once
	backup := func(_ context.Context, rt *app.Runtime, args []string) error {
		n := atomic.AddInt32(&running, 1)
		defer atomic.AddInt32(&running, -1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		if n >= 2 {
			once.Do(func() { close(release) })
		}
		select {
		case <-release:
		case <-time.After(2 * time.Second):
		}
		return nil
	}
	tr := newRT(map[string]string{"CONFIG": path, "LOG_LEVEL": "4"})
	if err := Run(ctx, tr.Runtime, nil, Deps{Backup: backup}); err != nil {
		t.Fatal(err)
	}
	if peak != 2 {
		t.Errorf("peak concurrency = %d, want 2", peak)
	}
	if !strings.Contains(tr.Err.String(), "launching 2 'zelta policy' jobs") {
		t.Errorf("stderr = %q", tr.Err.String())
	}

	rec := newRecorder()
	rec.fail["t/b"] = 1
	err := Run(ctx, newRT(map[string]string{"CONFIG": path}).Runtime, []string{"A", "B"}, Deps{Backup: rec.backup})
	if app.Code(err) != 1 || len(rec.runs) != 3 {
		t.Errorf("concurrent failure: %v %v", err, rec.runs)
	}
}

func TestExternalBackupCommand(t *testing.T) {
	path := writeConf(t, "SNAP_TIME: 8h\nS:\n  BACKUP_COMMAND: echo backup\n  h:\n  - p/a: t/a\n  - p/b: t/b\n", nil)
	tr := newRT(map[string]string{"CONFIG": path})
	script := `ZELTA_DEPTH="2" ZELTA_LOG_PREFIX="[S: t/a] h:p/a: " ZELTA_SNAP_TIME="8h" echo backup 'h:p/a' 't/a'`
	tr.Fake.On("{ "+script+" ; }", execx.Out("ran").WithStderr("noise"))
	tr.Fake.On(script, execx.Out("ran").WithStderr("noise"))
	tr.Fake.OnContains("'t/b'", execx.Fail(3, "nope"))
	err := Run(ctx, tr.Runtime, nil, Deps{CLI: map[string]string{"DEPTH": "2"}})
	if app.Code(err) != 1 {
		t.Errorf("err = %v", err)
	}
	if tr.Fake.Calls[0] != script {
		t.Errorf("script = %q\nwant  %q", tr.Fake.Calls[0], script)
	}
	if tr.RawOut.String() != "ran\n" || tr.RawErr.String() != "noise\nnope\n" {
		t.Errorf("streams out=%q err=%q", tr.RawOut.String(), tr.RawErr.String())
	}

	tr = newRT(map[string]string{"CONFIG": path, "DRYRUN": "1", "LOG_LEVEL": "3"})
	Run(ctx, tr.Runtime, []string{"p/a"}, Deps{})
	if got := tr.Out.String(); got != `+ ZELTA_SNAP_TIME="8h" echo backup 'h:p/a' 't/a'`+"\n" {
		t.Errorf("display = %q", got)
	}
}

// TestPrecedence checks defaults < zelta.env < policy < environment <
// command line using a real loader.
func TestPrecedence(t *testing.T) {
	etc := t.TempDir()
	os.WriteFile(filepath.Join(etc, "zelta.env"), []byte("DEPTH=5\nSNAP_PREFIX=fromenvfile\nRECV_TOP=-o readonly=off\nBAD LINE\n"), 0o644)
	loader := &config.Loader{
		Env:       map[string]string{"ZELTA_ETC": etc, "PATH": os.Getenv("PATH"), "ZELTA_SEND_INTR": "1"},
		Now:       app.TestNow,
		Hostname:  "testhost",
		User:      "tester",
		DirExists: func(string) bool { return false },
	}
	path := writeConf(t, "DEPTH: 2\nSEND_INTR: 0\nSNAP_NAME: policy\nS:\n  h:\n  - p/a: t/a\n  - p/b: t/b\n", nil)
	rec := newRecorder()
	tr := newRT(map[string]string{"CONFIG": path})
	err := Run(ctx, tr.Runtime, nil, Deps{Loader: loader, Backup: rec.backup, CLI: map[string]string{"SNAP_NAME": "cli", "JOBS": "4"}})
	if err != nil {
		t.Fatal(err)
	}
	o := rec.opts["t/a"]
	want := map[string]string{
		"DEPTH":       "2",               // policy beats zelta.env
		"SNAP_PREFIX": "fromenvfile",     // zelta.env beats defaults
		"RECV_TOP":    "-o readonly=off", // zelta.env
		"SEND_INTR":   "1",               // environment beats policy
		"SNAP_NAME":   "cli",             // command line beats policy
		"LOG_PREFIX":  "[S: t/a] h:p/a: ",
		"VERB":        "backup",
	}
	for k, v := range want {
		if o.Get(k) != v {
			t.Errorf("%s = %q want %q", k, o.Get(k), v)
		}
	}
	if o.Has("JOBS") {
		t.Errorf("policy-scoped CLI options must not be forwarded")
	}
	if n := strings.Count(tr.Err.String(), "ignoring invalid zelta.env line"); n != 1 {
		t.Errorf("zelta.env warnings should be logged once, got %d: %q", n, tr.Err.String())
	}
}

func TestObjects(t *testing.T) {
	path := writeConf(t, selectConf+"  BOGUS: x\n  host2:\n  - zroot/x: y\n", nil)
	got := Objects(path)
	for _, want := range []string{"NYC", "DAL", "web1.example", "user@web1.example", "db1", "zroot/db", "other:tank/db",
		"bk:tank/Backups/www", "user@web1.example:zroot/jail/www", "www", "db", "notarget", "host2", "y"} {
		i := sort.SearchStrings(got, want)
		if i >= len(got) || got[i] != want {
			t.Errorf("Objects missing %q: %v", want, got)
		}
	}
	if Objects("/nonexistent/zelta.conf") != nil {
		t.Errorf("missing file should yield nil")
	}
	if objs := Objects(centralized); len(objs) < 50 {
		t.Errorf("centralized objects = %d", len(objs))
	}
	if got := Objects(writeConf(t, "  bad:\n", nil)); len(got) != 0 {
		t.Errorf("lenient parse = %v", got)
	}
}

func TestNoTargetWarning(t *testing.T) {
	path := writeConf(t, "S:\n  h:\n  - p/a\n  - p/b: t/b\n", nil)
	tr := newRT(map[string]string{"CONFIG": path, "DRYRUN": "1", "NO_HEADER": "1"})
	if err := Run(ctx, tr.Runtime, nil, Deps{}); err != nil {
		t.Fatal(err)
	}
	if tr.Out.String() != "h:p/b t/b\n" || !strings.Contains(tr.Err.String(), "warning: no target defined for p/a") {
		t.Errorf("out=%q err=%q", tr.Out.String(), tr.Err.String())
	}
}
