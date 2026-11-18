package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"zelta.space/internal/app"
	"zelta.space/internal/config"
	"zelta.space/internal/execx"
	"zelta.space/internal/version"
)

// call records one engine invocation.
type call struct {
	engine string
	args   []string
	opts   *config.Options
	rt     *app.Runtime
}

type harness struct {
	app     *App
	out     *bytes.Buffer
	err     *bytes.Buffer
	calls   []call
	fake    *execx.Fake
	env     map[string]string
	result  error
	manArgs []string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{out: &bytes.Buffer{}, err: &bytes.Buffer{}, fake: execx.NewFake()}
	h.env = map[string]string{"ZELTA_ETC": t.TempDir(), "HOME": t.TempDir(), "PATH": os.Getenv("PATH")}
	engines := map[string]Engine{}
	for _, name := range []string{"match", "backup", "snapshot", "clone", "revert", "rotate", "rebase", "lock", "unlock", "failover", "propsync", "prune", "zprune", "report"} {
		engines[name] = func(ctx context.Context, rt *app.Runtime, args []string) error {
			h.calls = append(h.calls, call{engine: name, args: args, opts: rt.Opts, rt: rt})
			return h.result
		}
	}
	h.app = &App{
		Stdin: strings.NewReader(""), Stdout: h.out, Stderr: h.err, Exec: h.fake,
		NewLoader: func() *config.Loader {
			return &config.Loader{Env: h.env, Now: app.TestNow, Hostname: "testhost", User: "tester", DirExists: func(p string) bool {
				st, err := os.Stat(p)
				return err == nil && st.IsDir()
			}}
		},
		Now:     func() time.Time { return app.TestNow },
		Engines: engines,
		Policy: func(ctx context.Context, rt *app.Runtime, args []string, loader *config.Loader, cli map[string]string, backup Engine) error {
			h.calls = append(h.calls, call{engine: "policy", args: args, opts: rt.Opts, rt: rt})
			if backup == nil || loader == nil {
				t.Errorf("policy dependencies missing")
			}
			return h.result
		},
		Columns:       func() []string { return []string{"ds_suffix", "relname", "match"} },
		PolicyObjects: func(string) []string { return []string{"SITE1", "host1", "tank/data"} },
		Man: func(_ context.Context, _ *App, topic string) int {
			h.manArgs = append(h.manArgs, topic)
			return 0
		},
	}
	return h
}

func (h *harness) run(args ...string) int {
	h.out.Reset()
	h.err.Reset()
	h.calls = nil
	return h.app.Main(context.Background(), append([]string{"zelta"}, args...))
}

func (h *harness) runAs(prog string, args ...string) int {
	h.out.Reset()
	h.err.Reset()
	h.calls = nil
	return h.app.Main(context.Background(), append([]string{"/usr/local/bin/" + prog}, args...))
}

func (h *harness) last(t *testing.T) call {
	t.Helper()
	if len(h.calls) != 1 {
		t.Fatalf("expected one engine call, got %d (stderr: %s)", len(h.calls), h.err.String())
	}
	return h.calls[0]
}

func TestTopLevel(t *testing.T) {
	h := newHarness(t)
	if code := h.run(); code != 1 || !strings.HasPrefix(h.err.String(), "usage: zelta command [OPTIONS]") || !strings.Contains(h.err.String(), "  match                      Compare datasets") {
		t.Errorf("no args: code=%d\n%s", code, h.err.String())
	}
	for _, v := range []string{"version", "-V", "--version"} {
		if code := h.run(v); code != 0 || h.out.String() != version.String()+"\n" {
			t.Errorf("%s: code=%d out=%q", v, code, h.out.String())
		}
	}
	for _, v := range []string{"--help", "-h"} {
		if code := h.run(v); code != 0 || !strings.Contains(h.out.String(), "Run 'zelta COMMAND --help'") || !strings.Contains(h.out.String(), "Recovery:") {
			t.Errorf("%s: code=%d\n%s", v, code, h.out.String())
		}
	}
	for _, v := range []string{"usage", "-?"} {
		if code := h.run(v); code != 1 || !strings.Contains(h.err.String(), "usage: zelta command") {
			t.Errorf("%s: code=%d", v, code)
		}
	}
	if code := h.run("list"); code != 1 || !strings.HasPrefix(h.err.String(), "unrecognized command: 'list'\ndid you mean 'zelta match'?\nusage:") {
		t.Errorf("unknown command: %q", h.err.String())
	}
	if h.run("bakup"); !strings.Contains(h.err.String(), "did you mean 'zelta backup'?") {
		t.Errorf("typo suggestion: %q", h.err.String())
	}
	if h.run("xyzzyplugh"); strings.Contains(h.err.String(), "did you mean") {
		t.Errorf("no suggestion expected: %q", h.err.String())
	}
	if code := h.run("help", "backup"); code != 0 || len(h.manArgs) != 1 || h.manArgs[0] != "backup" {
		t.Errorf("help topic: %v", h.manArgs)
	}
	if h.run("help"); h.manArgs[len(h.manArgs)-1] != "" {
		t.Errorf("help without topic: %v", h.manArgs)
	}
}

func TestVerbOptions(t *testing.T) {
	h := newHarness(t)
	code := h.run("backup", "-vv", "-q", "--dryrun", "src", "--depth", "2", "-X/a", "--exclude=@b", "-L", "--raw", "-c", "--push", "tgt")
	c := h.last(t)
	if code != 0 || c.engine != "backup" || strings.Join(c.args, " ") != "src tgt" {
		t.Fatalf("code=%d call=%+v", code, c)
	}
	want := map[string]string{"LOG_LEVEL": "3", "DRYRUN": "1", "DEPTH": "2", "EXCLUDE": "/a,@b", "SEND_OVERRIDE": "-L --raw -c", "SYNC_DIRECTION": "PUSH", "VERB": "backup", "SEND_INTR": "1"}
	for k, v := range want {
		if got := c.opts.Get(k); got != v {
			t.Errorf("%s = %q want %q", k, got, v)
		}
	}
	if c.rt.Log.Level() != 3 {
		t.Errorf("runtime log level = %d", c.rt.Log.Level())
	}

	h.run("sync", "-nvd2", "a", "b")
	if c := h.last(t); c.opts.Get("VERB") != "sync" || c.opts.Get("SEND_INTR") != "0" || c.opts.Get("DEPTH") != "2" || c.engine != "backup" {
		t.Errorf("sync: VERB=%q SEND_INTR=%q", c.opts.Get("VERB"), c.opts.Get("SEND_INTR"))
	}
	h.run("backup", "-R", "a", "b")
	if c := h.last(t); c.opts.Get("VERB") != "replicate" {
		t.Errorf("-R VERB = %q", c.opts.Get("VERB"))
	}
	h.run("match", "-Hpo", "relname,match", "a", "b")
	if c := h.last(t); c.opts.Get("PROPLIST") != "relname,match" || !c.opts.Bool("SCRIPTING_MODE") || !c.opts.Bool("PARSABLE") {
		t.Errorf("match flags: %v", c.opts.Keys())
	}
	h.run("backup", "-o", "compression=zstd", "-x", "mountpoint", "-o", "atime=off", "a", "b")
	if c := h.last(t); c.opts.Get("RECV_PROPS_ADD") != "compression=zstd,atime=off" || c.opts.Get("RECV_PROPS_DEL") != "mountpoint" {
		t.Errorf("recv props: %q %q", c.opts.Get("RECV_PROPS_ADD"), c.opts.Get("RECV_PROPS_DEL"))
	}
	h.run("policy", "--retry", "3", "--host-prefix", "SITE1")
	if c := h.last(t); c.opts.Get("RETRY") != "3" || c.opts.Get("ADD_HOST_PREFIX") != "1" || strings.Join(c.args, ",") != "SITE1" {
		t.Errorf("policy optional values: RETRY=%q HOST=%q args=%v", c.opts.Get("RETRY"), c.opts.Get("ADD_HOST_PREFIX"), c.args)
	}
	h.run("policy", "--retry", "--", "SITE1")
	if c := h.last(t); c.opts.Get("RETRY") != "1" || strings.Join(c.args, ",") != "SITE1" {
		t.Errorf("--retry default: %q %v", c.opts.Get("RETRY"), c.args)
	}
	h.run("revert", "-f", "pool/ds@snap")
	if c := h.last(t); !c.opts.Bool("REVERT_FORCE") || c.opts.Get("SNAP_MODE") != "0" {
		t.Errorf("revert: %q %q", c.opts.Get("REVERT_FORCE"), c.opts.Get("SNAP_MODE"))
	}
	h.run("lock", "-f", "--no-unmount", "a", "b", "c")
	if c := h.last(t); !c.opts.Bool("FORCE_UNMOUNT") || !c.opts.Bool("NO_UNMOUNT") || len(c.args) != 3 {
		t.Errorf("lock: %v", c.args)
	}
}

func TestFlagErrors(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		args []string
		want string
	}{
		{[]string{"backup", "--bogus", "a", "b"}, "error: invalid option '--bogus'"},
		{[]string{"backup", "--bogus=1", "a", "b"}, "error: invalid option '--bogus'"},
		{[]string{"match", "-k", "a"}, "error: invalid option '-k'"},
		{[]string{"match", "a", "--depth"}, "error: option '--depth' requires an argument"},
		{[]string{"match", "a", "-d"}, "error: option '-d' requires an argument"},
		{[]string{"backup", "--dryrun=1", "a", "b"}, "error: invalid option assignment '--dryrun=1'"},
		{[]string{"backup", "--initiator", "a", "b"}, "error: --initiator is deprecated; use '--pull' (default) or '--push'"},
		{[]string{"backup", "-z", "a", "b"}, "error: option '-z' is deprecated; consider '--json'"},
		{[]string{"rotate", "-i", "a", "b"}, "error: invalid option '-i'"},
	}
	for _, tt := range tests {
		code := h.run(tt.args...)
		if code != 1 || !strings.HasPrefix(h.err.String(), tt.want+"\n") || !strings.Contains(h.err.String(), "usage: zelta ") || len(h.calls) != 0 {
			t.Errorf("%v: code=%d stderr=%q", tt.args, code, h.err.String())
		}
	}
}

func TestWarningsAndHelp(t *testing.T) {
	h := newHarness(t)
	h.run("backup", "-s", "-t", "a", "b")
	if c := h.last(t); c.opts.Get("SNAP_MODE") != "IF_NEEDED" || c.opts.Get("SYNC_DIRECTION") != "PUSH" {
		t.Errorf("deprecated flags: %q %q", c.opts.Get("SNAP_MODE"), c.opts.Get("SYNC_DIRECTION"))
	}
	if !strings.Contains(h.err.String(), "warning: option '-s' for '--snapshot' is ambiguous") || !strings.Contains(h.err.String(), "warning: option '-t' is deprecated; use '--push'") {
		t.Errorf("warnings: %q", h.err.String())
	}

	code := h.run("backup", "-h", "a", "b")
	if code != 1 || len(h.calls) != 0 || !strings.Contains(h.err.String(), "warning: option '-h' is ambiguous") || !strings.Contains(h.err.String(), "usage: zelta backup [OPTIONS] SOURCE TARGET") {
		t.Errorf("backup -h: code=%d %q", code, h.err.String())
	}
	for _, args := range [][]string{{"match", "-h"}, {"match", "--help"}, {"backup", "--help"}, {"match", "-?"}} {
		code := h.run(args...)
		if code != 0 || len(h.calls) != 0 || !strings.Contains(h.out.String(), "usage: zelta "+args[0]) || !strings.Contains(h.out.String(), "zelta help "+args[0]) {
			t.Errorf("%v: code=%d out=%q", args, code, h.out.String())
		}
	}
	h.run("backup", "--help")
	out := h.out.String()
	for _, want := range []string{"Snapshot Options:", "--snap-name NAME", "-n, --dryrun, --dry-run", "-i, --incremental", "Pass-Through zfs send/recv Flags:", "--recv-pipe COMMAND"} {
		if !strings.Contains(out, want) {
			t.Errorf("backup help missing %q", want)
		}
	}
	if strings.Contains(out, "--initiator") || strings.Contains(out, "zelta-short") {
		t.Errorf("backup help shows hidden flags")
	}
	h.run("policy", "--help")
	if !strings.Contains(h.out.String(), "--retry[=N]") {
		t.Errorf("policy help: %s", h.out.String())
	}
}

func TestEngineErrors(t *testing.T) {
	h := newHarness(t)
	h.result = app.Fail("source dataset 'x' does not exist")
	if code := h.run("backup", "x", "y"); code != 1 || h.err.String() != "error: source dataset 'x' does not exist\n" {
		t.Errorf("fail: code=%d %q", code, h.err.String())
	}
	h.result = app.Usage("missing target endpoint argument")
	if code := h.run("backup", "x"); code != 1 || !strings.HasPrefix(h.err.String(), "missing target endpoint argument\nusage: zelta backup [OPTIONS] SOURCE TARGET") {
		t.Errorf("usage: code=%d %q", code, h.err.String())
	}
	h.result = &app.ExitError{Code: 2}
	if code := h.run("backup", "x", "y"); code != 2 || h.err.Len() != 0 {
		t.Errorf("exit 2: code=%d %q", code, h.err.String())
	}
	h.result = nil
	h.app.Engines = map[string]Engine{}
	if code := h.run("match", "a"); code != 1 || !strings.Contains(h.err.String(), "not available") {
		t.Errorf("missing engine: %q", h.err.String())
	}
	h.app.Policy = nil
	if code := h.run("policy"); code != 1 || !strings.Contains(h.err.String(), "not available") {
		t.Errorf("missing policy: %q", h.err.String())
	}
}

func TestJSONOutput(t *testing.T) {
	h := newHarness(t)
	h.app.Engines["backup"] = func(ctx context.Context, rt *app.Runtime, args []string) error {
		rt.Report("replicationErrorCode", 0)
		rt.Log.Noticef("syncing 1 datasets")
		rt.Log.Warnf("careful")
		return nil
	}
	code := h.run("backup", "--json", "me@host:pool/src@s1", "pool/tgt")
	if code != 0 {
		t.Fatalf("code=%d %s", code, h.err.String())
	}
	want := `{"output_version": {"command": "zelta backup","vers_major": 1,"vers_minor": 1},"sourceUser": "me","sourceHost": "host","sourceDataset": "pool/src","sourceSnapshot": "@s1","sourceEndpoint": "me@host:pool/src@s1","targetUser": "tester","targetHost": "testhost","targetDataset": "pool/tgt","targetEndpoint": "pool/tgt","replicationErrorCode": 0,"errorMessages": ["careful"]}` + "\n"
	if h.out.String() != want {
		t.Errorf("json:\n%s\nwant\n%s", h.out.String(), want)
	}
	if h.err.String() != "notice: syncing 1 datasets\nwarning: careful\n" {
		t.Errorf("json stderr = %q", h.err.String())
	}

	logFile := filepath.Join(t.TempDir(), "zelta.json")
	h.run("backup", "-j", "--log-file", logFile, "a", "b")
	data, _ := os.ReadFile(logFile)
	if !strings.HasPrefix(string(data), `{"output_version"`) || strings.Contains(h.out.String(), "output_version") {
		t.Errorf("json log file = %q stdout=%q", data, h.out.String())
	}
	h.result = app.Usage("bad")
	h.app.Engines["backup"] = func(context.Context, *app.Runtime, []string) error { return app.Usage("bad") }
	if h.run("backup", "--json", "a"); strings.Contains(h.out.String(), "output_version") {
		t.Errorf("usage errors do not print JSON")
	}
}

func TestEnvironmentPrecedence(t *testing.T) {
	h := newHarness(t)
	os.WriteFile(filepath.Join(h.env["ZELTA_ETC"], "zelta.env"), []byte("DEPTH=4\nLOG_LEVEL=3\nSNAP_NAME='@fromfile'\n"), 0o644)
	h.env["ZELTA_DEPTH"] = "5"
	h.env["ZELTA_INTR_FLAGS"] = "0"
	h.run("backup", "a", "b")
	c := h.last(t)
	if c.opts.Get("DEPTH") != "5" || c.opts.Get("LOG_LEVEL") != "3" || c.opts.Get("SNAP_NAME") != "@fromfile" || c.opts.Get("SEND_INTR") != "0" {
		t.Errorf("precedence: DEPTH=%q LOG_LEVEL=%q SNAP_NAME=%q SEND_INTR=%q", c.opts.Get("DEPTH"), c.opts.Get("LOG_LEVEL"), c.opts.Get("SNAP_NAME"), c.opts.Get("SEND_INTR"))
	}
	if !strings.Contains(h.err.String(), "warning: INTR_FLAGS will be deprecated") {
		t.Errorf("legacy warning: %q", h.err.String())
	}
	h.run("backup", "-d", "1", "-v", "a", "b")
	if c := h.last(t); c.opts.Get("DEPTH") != "1" || c.opts.Get("LOG_LEVEL") != "4" {
		t.Errorf("cli wins: %q %q", c.opts.Get("DEPTH"), c.opts.Get("LOG_LEVEL"))
	}
}

func TestLegacyNames(t *testing.T) {
	h := newHarness(t)
	for prog, engine := range map[string]string{"zmatch": "match", "zpull": "backup", "zpush": "backup", "zsync": "backup", "zeport": "report", "zprune": "zprune"} {
		if code := h.runAs(prog, "pool/a"); code != 0 {
			t.Errorf("%s: code=%d %s", prog, code, h.err.String())
			continue
		}
		if c := h.last(t); c.engine != engine {
			t.Errorf("%s ran %s", prog, c.engine)
		}
	}
	h.runAs("zsync", "a", "b")
	if c := h.last(t); c.opts.Get("VERB") != "sync" {
		t.Errorf("zsync VERB = %q", c.opts.Get("VERB"))
	}
	h.runAs("zprune", "--force", "--no-prune-guard", "pool/a")
	if c := h.last(t); !c.opts.Bool("PRUNE_FORCE") || c.opts.Get("PRUNE_GUARD") != "none" || c.opts.Get("VERB") != "prune" {
		t.Errorf("zprune opts: %v", c.opts.Keys())
	}
	if code := h.runAs("zprune", "-V"); code != 0 || h.out.String() != version.Zprune()+"\n" {
		t.Errorf("zprune -V: %q", h.out.String())
	}
	if code := h.runAs("zprune", "--help"); code != 0 || !strings.Contains(h.out.String(), "usage: zprune [OPTIONS] ENDPOINT") || !strings.Contains(h.out.String(), "--match-endpoint") {
		t.Errorf("zprune --help: %q", h.out.String())
	}
	h.runAs("zp", "SITE")
	if c := h.last(t); c.engine != "policy" {
		t.Errorf("zp ran %s", c.engine)
	}
}

// TestZeltaRefusesZprune checks that the zelta binary, which has no zprune
// engine, never runs zprune: not as a verb and not through a stale link.
func TestZeltaRefusesZprune(t *testing.T) {
	h := newHarness(t)
	delete(h.app.Engines, "zprune")
	if code := h.run("zprune", "--force", "--no-prune-guard", "pool/a"); code != 1 || len(h.calls) != 0 || !strings.Contains(h.err.String(), "unrecognized command: 'zprune'\ndid you mean 'zprune'?") {
		t.Errorf("zelta zprune: code=%d calls=%d %q", code, len(h.calls), h.err.String())
	}
	if code := h.runAs("zprune", "--force", "--no-prune-guard", "pool/a"); code != 1 || len(h.calls) != 0 || !strings.Contains(h.err.String(), "zprune is now a separate command") {
		t.Errorf("zprune link to zelta: code=%d calls=%d %q", code, len(h.calls), h.err.String())
	}
	if h.run("--help"); strings.Contains(h.out.String(), "zprune") {
		t.Errorf("zelta --help lists zprune: %q", h.out.String())
	}
}

func TestIPC(t *testing.T) {
	h := newHarness(t)
	if code := h.run("ipc-env", "prune", "--no-prune-guard", "-d2", "me@host:pool/ds"); code != 0 {
		t.Fatalf("ipc-env: %d %s", code, h.err.String())
	}
	for _, want := range []string{"export ZELTA_PRUNE_GUARD=none\n", "export ZELTA_DEPTH=2\n", "export ZELTA_SRC_DS=pool/ds\n", "export ZELTA_SRC_REMOTE=me@host\n", "export ZELTA_VERB=prune\n", "export ZELTA_OPERANDS=me@host:pool/ds\n"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("ipc-env missing %q", want)
		}
	}
	// Bracketed IPv6 hosts and values that need quoting, as FreeBSD sh prints them.
	if code := h.run("ipc-env", "backup", "--snap-name", "a b", "tank/source", "root@[2001:db8::1]:tank/backups@bk_test"); code != 0 {
		t.Fatalf("ipc-env ipv6: %d %s", code, h.err.String())
	}
	for _, want := range []string{"export ZELTA_TGT_REMOTE=root@2001:db8::1\n", "export ZELTA_TGT_HOST=2001:db8::1\n",
		"export ZELTA_TGT_DS=tank/backups\n", "export ZELTA_TGT_SNAP=@bk_test\n", "export ZELTA_SNAP_NAME='a b'\n"} {
		if !strings.Contains(h.out.String(), want) {
			t.Errorf("ipc-env missing %q in %s", want, h.out.String())
		}
	}
	for in, want := range map[string]string{"": "''", "a=b": "'a=b'", "~x": "'~x'", "it's": `'it'\''s'`, "a,b+c@d/e.f_g-h:i%!^{}]": "a,b+c@d/e.f_g-h:i%!^{}]"} {
		if got := setQuote(in); got != want {
			t.Errorf("setQuote(%q) = %s want %s", in, got, want)
		}
	}
	if code := h.run("ipc-env", "prune", "--bogus"); code != 1 || !strings.Contains(h.err.String(), "invalid option '--bogus'") {
		t.Errorf("ipc-env bad flag: %d %q", code, h.err.String())
	}
	if code := h.run("ipc-env", "backup", "--initiator"); code != 1 {
		t.Errorf("ipc-env invalid option: %d", code)
	}
	if code := h.run("ipc-env"); code != 1 {
		t.Errorf("ipc-env without verb: %d", code)
	}
	if code := h.run("ipc-env", "backup", "-h", "a"); code != 1 {
		t.Errorf("ipc-env usage: %d", code)
	}

	h.app.Stdin = strings.NewReader("2\x1chello\n1\x1ccareful\n4\x1chidden\nplain\n")
	h.env["ZELTA_LOG_LEVEL"] = "3"
	if code := h.run("ipc-log"); code != 0 || h.out.String() != "hello\nplain\n" || h.err.String() != "warning: careful\n" {
		t.Errorf("ipc-log: out=%q err=%q", h.out.String(), h.err.String())
	}
	h.env["ZELTA_LOG_FILE"] = filepath.Join(t.TempDir(), "missing", "x.log")
	if code := h.run("ipc-log"); code != 1 {
		t.Errorf("ipc-log bad file: %d", code)
	}
	delete(h.env, "ZELTA_LOG_FILE")
	h.app.Stdin = iotest.ErrReader(errors.New("boom"))
	if code := h.run("ipc-log"); code != 1 || h.err.String() != "error: reading log records: boom\n" {
		t.Errorf("ipc-log read error: %d %q", code, h.err.String())
	}

	h.env["ZELTA_OPERANDS"] = "pool/a\x1cpool/b"
	h.env["ZELTA_SRC_ID"] = "pool/a"
	h.run("ipc-run", "match")
	if c := h.last(t); strings.Join(c.args, ",") != "pool/a,pool/b" || c.opts.Get("SRC_ID") != "pool/a" {
		t.Errorf("ipc-run: %v %q", c.args, c.opts.Get("SRC_ID"))
	}
	if code := h.run("ipc-run"); code != 1 {
		t.Errorf("ipc-run without verb: %d", code)
	}

	h.fake.On("echo hi", execx.Out("hi").WithStderr("note"))
	if code := h.run("time", "echo", "hi"); code != 0 || h.out.String() != "hi\n" || !strings.Contains(h.err.String(), "note\nreal ") {
		t.Errorf("time: out=%q err=%q", h.out.String(), h.err.String())
	}
	if code := h.run("time"); code != 0 {
		t.Errorf("time without command")
	}
}

func TestCompletion(t *testing.T) {
	h := newHarness(t)
	for _, shell := range []string{"bash", "zsh", "fish", "powershell"} {
		if code := h.run("completion", shell); code != 0 || !strings.Contains(h.out.String(), "zelta") {
			t.Errorf("completion %s: code=%d", shell, code)
		}
	}
	h.fake.On("zfs list -H -o name -d 0", execx.Out("tank", "zroot"))
	h.fake.On("zfs list -H -o name -t filesystem,volume -d 1 tank", execx.Out("tank", "tank/data", "tank/vm"))
	h.fake.On("zfs list -H -o name -t snapshot -d 1 tank/data", execx.Out("tank/data@a", "tank/data@b"))
	h.fake.On("ssh -n -o BatchMode=yes -o ConnectTimeout=3 me@host zfs list -H -o name -t filesystem,volume -d 1 pool", execx.Out("pool", "pool/x"))
	tests := []struct {
		args []string
		want []string
	}{
		{[]string{"backup", "t"}, []string{"tank"}},
		{[]string{"backup", "tank/d"}, []string{"tank/data"}},
		{[]string{"backup", "tank/data@"}, []string{"tank/data@a", "tank/data@b"}},
		{[]string{"backup", "me@host:pool/"}, []string{"me@host:pool/x"}},
		{[]string{"match", "-o", "relname,m"}, []string{"relname,match"}},
		{[]string{"backup", "--snap-mode", ""}, []string{"IF_NEEDED", "ALWAYS"}},
		{[]string{"policy", "S"}, []string{"SITE1"}},
		{[]string{"help", ""}, []string{"backup", "options"}},
	}
	for _, tt := range tests {
		h.run(append([]string{"__complete"}, tt.args...)...)
		for _, w := range tt.want {
			if !strings.Contains(h.out.String(), w+"\n") && !strings.Contains(h.out.String(), w+"\t") {
				t.Errorf("complete %v missing %q in:\n%s", tt.args, w, h.out.String())
			}
		}
	}
	h.run("__complete", "revert", "a", "")
	if strings.Contains(h.out.String(), "tank") {
		t.Errorf("revert completes only one operand: %s", h.out.String())
	}
	if host, path := splitHost("u@[::1]:pool/x"); host != "u@[::1]" || path != "pool/x" {
		t.Errorf("splitHost ipv6 = %q %q", host, path)
	}
	if host, path := splitHost("localhost:pool"); host != "" || path != "pool" {
		t.Errorf("splitHost localhost = %q %q", host, path)
	}
	if host, path := splitHost("pool/a:b"); host != "" || path != "pool/a:b" {
		t.Errorf("splitHost dataset colon = %q %q", host, path)
	}
	if host, _ := splitHost("[::1"); host != "" {
		t.Errorf("splitHost unterminated")
	}
}

func TestSSHHosts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".ssh"), 0o700)
	os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte("Host backup1 *.wild\n  HostName x\nHost !neg db1\n"), 0o600)
	os.WriteFile(filepath.Join(home, ".ssh", "known_hosts"), []byte("vault1,10.0.0.1 ssh-ed25519 AAA\n|1|hashed ssh-rsa AAA\n[port]:2222 ssh-rsa AAA\n# comment\n"), 0o600)
	got := strings.Join(sshHosts(), ",")
	if got != "10.0.0.1,backup1,db1,vault1" {
		t.Errorf("sshHosts = %q", got)
	}
}

func TestSuggestionAndLevenshtein(t *testing.T) {
	if levenshtein("kitten", "sitting") != 3 || levenshtein("", "abc") != 3 || levenshtein("same", "same") != 0 {
		t.Errorf("levenshtein")
	}
	if suggestion("send") != "zelta backup" || suggestion("rollback") != "zelta revert" || suggestion("mach") != "zelta match" {
		t.Errorf("suggestions")
	}
}

func TestHelpRendering(t *testing.T) {
	var b bytes.Buffer
	for _, v := range verbs {
		b.Reset()
		verbHelp(&b, v, prog(v))
		if !strings.Contains(b.String(), "usage: "+prog(v)) {
			t.Errorf("%s help: %s", v.name, b.String())
		}
	}
	if got := wrapIndent("aaa bbb ccc", 7, "  "); got != "aaa bbb\n  ccc" {
		t.Errorf("wrap = %q", got)
	}
	if helpTopic(verb{name: "sync"}) != "backup" || helpTopic(verb{name: "match"}) != "match" {
		t.Errorf("helpTopic")
	}
	if len(helpTopics()) < 10 {
		t.Errorf("helpTopics = %v", helpTopics())
	}
}

func TestPreprocess(t *testing.T) {
	got := preprocess([]string{"-?", "--retry", "2", "--host-prefix", "x", "--", "-?"}, "policy")
	want := "--help --retry=2 --host-prefix x -- -?"
	if strings.Join(got, " ") != want {
		t.Errorf("preprocess = %q", strings.Join(got, " "))
	}
}

func TestSysTime(t *testing.T) {
	h := newHarness(t)
	h.fake.On("date +%s", execx.Out("1700000000"))
	h.env["ZELTA_SYSTIME"] = "date +%s"
	h.run("backup", "a", "b")
	if got := h.last(t).rt.Unix(); got != 1700000000 {
		t.Errorf("SYSTIME clock = %d", got)
	}
	h.fake.On("bad", execx.Out("not a number"))
	h.env["ZELTA_SYSTIME"] = "bad"
	h.run("backup", "a", "b")
	if got := h.last(t).rt.Now(); !got.Equal(app.TestNow) {
		t.Errorf("SYSTIME fallback = %v", got)
	}
}

type closingExec struct {
	execx.Executor
	closed *bool
}

func (c closingExec) Close() error { *c.closed = true; return nil }

func TestMultiplexHook(t *testing.T) {
	h := newHarness(t)
	closed, wrapped := false, 0
	h.app.Multiplex = func(e execx.Executor) execx.Executor {
		wrapped++
		return closingExec{Executor: e, closed: &closed}
	}
	h.run("backup", "a", "b")
	if wrapped != 1 || !closed {
		t.Errorf("multiplexer: wrapped=%d closed=%v", wrapped, closed)
	}
	if _, ok := h.last(t).rt.Exec.(closingExec); !ok {
		t.Errorf("engine did not receive the multiplexed executor")
	}
	for _, args := range [][]string{{"backup", "-n", "a", "b"}} {
		wrapped = 0
		h.run(args...)
		if wrapped != 0 {
			t.Errorf("%v: dry runs need no connections", args)
		}
	}
	h.env["ZELTA_REMOTE_MULTIPLEX"] = "0"
	wrapped = 0
	h.run("backup", "a", "b")
	if wrapped != 0 {
		t.Errorf("REMOTE_MULTIPLEX=0 must disable multiplexing")
	}
}
