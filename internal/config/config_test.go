package config

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

var fixedNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func testLoader(t *testing.T, env map[string]string) *Loader {
	t.Helper()
	if env == nil {
		env = map[string]string{}
	}
	if _, ok := env["ZELTA_ETC"]; !ok {
		env["ZELTA_ETC"] = t.TempDir()
	}
	env["PATH"] = os.Getenv("PATH")
	return &Loader{Env: env, Now: fixedNow, Hostname: "testhost", User: "tester", DirExists: dirExists}
}

func writeEnvFile(t *testing.T, l *Loader, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(l.Env["ZELTA_ETC"], "zelta.env"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDefaults(t *testing.T) {
	o, warnings, err := testLoader(t, nil).Load(context.Background(), "backup", nil)
	if err != nil || len(warnings) != 0 {
		t.Fatalf("err=%v warnings=%v", err, warnings)
	}
	want := map[string]string{
		"LOG_LEVEL": "2", "SNAP_MODE": "IF_NEEDED", "SEND_INTR": "1", "SEND_DEFAULT": "-L -c -e",
		"REMOTE_DEFAULT": "ssh -n", "REMOTE_SEND": "ssh -n", "REMOTE_RECV": "ssh",
		"SNAP_NAME": "zelta_2026-01-02_03.04.05", "HOSTNAME": "testhost", "VERB": "backup",
		"RECV_FS": "-u -x mountpoint -o canmount=noauto",
	}
	for k, v := range want {
		if got := o.Get(k); got != v {
			t.Errorf("%s = %q want %q", k, got, v)
		}
	}
	if !strings.HasSuffix(o.Get("CONFIG"), "zelta.conf") || !strings.HasSuffix(o.Get("ENV"), "zelta.env") {
		t.Errorf("paths: %q %q", o.Get("CONFIG"), o.Get("ENV"))
	}
}

func TestVerbDefaults(t *testing.T) {
	for verb, want := range map[string]string{"sync": "0", "clone": "0", "rotate": "0", "backup": "1", "revert": "0"} {
		o, _, _ := testLoader(t, nil).Load(context.Background(), verb, nil)
		if got := o.Get("SEND_INTR"); got != want {
			t.Errorf("%s SEND_INTR = %q want %q", verb, got, want)
		}
	}
	o, _, _ := testLoader(t, map[string]string{"ZELTA_SEND_INTR": "1"}).Load(context.Background(), "sync", nil)
	if o.Get("SEND_INTR") != "1" {
		t.Errorf("environment should win over sync default")
	}
	o, _, _ = testLoader(t, map[string]string{"ZELTA_SNAP_MODE": "ALWAYS"}).Load(context.Background(), "revert", nil)
	if o.Get("SNAP_MODE") != "0" {
		t.Errorf("revert forces SNAP_MODE=0")
	}
}

func TestPrecedence(t *testing.T) {
	l := testLoader(t, map[string]string{"ZELTA_DEPTH": "5", "ZELTA_VERB": "evil", "ZELTA_SRC_ID": "x"})
	writeEnvFile(t, l, "DEPTH=2\nLOG_LEVEL=3 # comment\nZELTA_RETRY=4\nEXCLUDE='@a,@b'\n")
	o, _, err := l.Load(context.Background(), "policy", map[string]string{"RETRY": "9", "SNAP_MODE": "ALWAYS"})
	if err != nil {
		t.Fatal(err)
	}
	checks := map[string]struct {
		val string
		src Source
	}{
		"DEPTH":     {"5", FromEnv},
		"LOG_LEVEL": {"3", FromEnvFile},
		"RETRY":     {"9", FromPolicy},
		"SNAP_MODE": {"ALWAYS", FromPolicy},
		"EXCLUDE":   {"@a,@b", FromEnvFile},
		"VERB":      {"policy", FromDefault},
	}
	for k, want := range checks {
		if o.Get(k) != want.val || o.Source(k) != want.src {
			t.Errorf("%s = %q (%v) want %q (%v)", k, o.Get(k), o.Source(k), want.val, want.src)
		}
	}
	if _, ok := o.Lookup("SRC_ID"); ok {
		t.Errorf("internal keys must not leak from the environment")
	}
	l.Internal = true
	o, _, _ = l.Load(context.Background(), "match", nil)
	if o.Get("SRC_ID") != "x" || o.Get("VERB") != "evil" {
		t.Errorf("ipc-run keeps internal keys: %q %q", o.Get("SRC_ID"), o.Get("VERB"))
	}
}

func TestEnvFileShellSemantics(t *testing.T) {
	l := testLoader(t, map[string]string{"ZELTA_REMOTE_COMMAND": "ssh -p 2202", "MYVAR": "val"})
	writeEnvFile(t, l, strings.Join([]string{
		"  # indented comment",
		"",
		"SNAP_NAME='$(date -u +%s)'",
		`SNAP_PREFIX="$(echo pre)_"`,
		`REMOTE_DEFAULT="${ZELTA_REMOTE_COMMAND} -n"`,
		"REMOTE_COMMAND=ignored-because-env-wins",
		"SEND_DEFAULT=-L -c",
		`BOOKMARK_PREFIX="$MYVAR"`,
		"export LOG_MODE=json",
		"ZELTA_AWK=mawk",
		"not a valid line",
	}, "\n"))
	o, warnings, err := l.Load(context.Background(), "backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"SNAP_NAME":       "$(date -u +%s)",
		"SNAP_PREFIX":     "pre_",
		"REMOTE_DEFAULT":  "ssh -p 2202 -n",
		"REMOTE_COMMAND":  "ssh -p 2202",
		"REMOTE_SEND":     "ssh -p 2202 -n",
		"REMOTE_RECV":     "ssh -p 2202",
		"SEND_DEFAULT":    "-L -c",
		"BOOKMARK_PREFIX": "val",
		"LOG_MODE":        "json",
	}
	for k, v := range want {
		if got := o.Get(k); got != v {
			t.Errorf("%s = %q want %q", k, got, v)
		}
	}
	if all := strings.Join(warnings, "\n"); len(warnings) != 2 || !strings.Contains(all, "invalid zelta.env line") || !strings.Contains(all, "ZELTA_AWK ignored") {
		t.Errorf("warnings = %#v", warnings)
	}
	if _, ok := o.Lookup("AWK"); ok {
		t.Errorf("ZELTA_AWK must be ignored in zelta.env")
	}
}

func TestEnvFileErrors(t *testing.T) {
	l := testLoader(t, nil)
	writeEnvFile(t, l, "DEPTH=3\nBAD=${\nRETRY=2\n")
	o, warnings, err := l.Load(context.Background(), "backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	if o.Get("DEPTH") != "3" {
		t.Errorf("values before a failing line are kept: %q", o.Get("DEPTH"))
	}
	if len(warnings) == 0 {
		t.Errorf("expected a shell warning")
	}
	if vals, _, err := LoadEnvFile(context.Background(), "/nonexistent/zelta.env", nil); err != nil || len(vals) != 0 {
		t.Errorf("missing file: %v %v", vals, err)
	}
	if vals, _, err := EvalEnvFile(context.Background(), nil, nil); err != nil || len(vals) != 0 {
		t.Errorf("empty eval: %v %v", vals, err)
	}
}

func TestLegacyKeys(t *testing.T) {
	l := testLoader(t, map[string]string{"ZELTA_INTR_FLAGS": "0", "ZELTA_THREADS": "3", "ZELTA_RECEIVE_PREFIX": "pv", "ZELTA_JOBS": ""})
	o, warnings, _ := l.Load(context.Background(), "policy", nil)
	if o.Get("SEND_INTR") != "0" || o.Get("JOBS") != "3" || o.Get("RECV_PIPE") != "pv" {
		t.Errorf("legacy mapping failed: %q %q %q", o.Get("SEND_INTR"), o.Get("JOBS"), o.Get("RECV_PIPE"))
	}
	if len(warnings) != 1 || !strings.Contains(warnings[0], "INTR_FLAGS") {
		t.Errorf("warnings = %#v", warnings)
	}
	l = testLoader(t, map[string]string{"ZELTA_THREADS": "3", "ZELTA_JOBS": "5"})
	o, _, _ = l.Load(context.Background(), "policy", nil)
	if o.Get("JOBS") != "5" {
		t.Errorf("new key wins over legacy at same precedence: %q", o.Get("JOBS"))
	}
}

func TestPaths(t *testing.T) {
	home := t.TempDir()
	os.MkdirAll(filepath.Join(home, ".config/zelta"), 0o755)
	os.MkdirAll(filepath.Join(home, ".local/share/zelta/doc"), 0o755)
	l := &Loader{Env: map[string]string{"HOME": home}, DirExists: dirExists}
	p := l.Paths()
	if p.Etc != filepath.Join(home, ".config/zelta") || p.Doc != filepath.Join(home, ".local/share/zelta/doc") {
		t.Errorf("user paths = %#v", p)
	}
	l = &Loader{Env: map[string]string{"HOME": "/nonexistent"}, DirExists: func(p string) bool { return p == "/etc/zelta" }}
	if p := l.Paths(); p.Etc != "/etc/zelta" || p.Config != "/etc/zelta/zelta.conf" {
		t.Errorf("/etc fallback = %#v", p)
	}
	l = &Loader{Env: map[string]string{"ZELTA_ETC": "/x", "ZELTA_ENV": "/y/env", "ZELTA_CONFIG": "/z/conf", "ZELTA_DOC": "/d"}, DirExists: dirExists}
	if p := l.Paths(); !reflect.DeepEqual(p, Paths{Etc: "/x", Env: "/y/env", Config: "/z/conf", Doc: "/d"}) {
		t.Errorf("explicit paths = %#v", p)
	}
	l = &Loader{Env: map[string]string{}, DirExists: func(string) bool { return false }}
	if p := l.Paths(); p.Etc != "/usr/local/etc/zelta" {
		t.Errorf("system default = %#v", p)
	}
}

func TestNewLoader(t *testing.T) {
	t.Setenv("HOSTNAME", "fromenv")
	t.Setenv("USER", "someone")
	l := NewLoader()
	if l.Hostname != "fromenv" || l.User != "someone" || l.Env["HOSTNAME"] != "fromenv" {
		t.Errorf("loader = %q %q", l.Hostname, l.User)
	}
	t.Setenv("HOSTNAME", "")
	t.Setenv("USER", "")
	if l := NewLoader(); l.Hostname == "" || l.User == "" {
		t.Errorf("fallbacks = %q %q", l.Hostname, l.User)
	}
}

func spec(t *testing.T, verb, flag string) *Spec {
	t.Helper()
	var found *Spec
	for _, s := range SpecsFor(verb) {
		for _, f := range s.Flags {
			if f == flag {
				found = s
			}
		}
	}
	if found == nil {
		t.Fatalf("no spec for %s %s", verb, flag)
	}
	return found
}

func TestApplyOps(t *testing.T) {
	base := NewOptions()
	base.Set("LOG_LEVEL", "2", FromDefault)
	base.Set("EXCLUDE", "@old", FromEnv)
	ops := []Op{
		{Spec: spec(t, "backup", "-v"), Flag: "-v"},
		{Spec: spec(t, "backup", "-v"), Flag: "-v"},
		{Spec: spec(t, "backup", "-q"), Flag: "-q"},
		{Spec: spec(t, "backup", "--exclude"), Flag: "--exclude", Arg: "/a"},
		{Spec: spec(t, "backup", "-X"), Flag: "-X", Arg: "@b"},
		{Spec: spec(t, "backup", "-X"), Flag: "-X", Arg: ""},
		{Spec: spec(t, "backup", "-L"), Flag: "-L"},
		{Spec: spec(t, "backup", "--raw"), Flag: "--raw"},
		{Spec: spec(t, "backup", "--push"), Flag: "--push"},
		{Spec: spec(t, "backup", "--depth"), Flag: "--depth", Arg: "2"},
		{Spec: spec(t, "backup", "-n"), Flag: "-n"},
		{Spec: spec(t, "backup", "--no-resume"), Flag: "--no-resume"},
		{Spec: spec(t, "backup", "-s"), Flag: "-s"},
		{Spec: spec(t, "backup", "-F"), Flag: "-F"},
	}
	vals, warnings, err := ApplyOps(base, ops)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"LOG_LEVEL": "3", "EXCLUDE": "/a,@b", "SEND_OVERRIDE": "-L --raw", "SYNC_DIRECTION": "PUSH",
		"DEPTH": "2", "DRYRUN": "1", "RESUME": "0", "SNAP_MODE": "IF_NEEDED", "RECV_OVERRIDE": "-F",
	}
	if !reflect.DeepEqual(vals, want) {
		t.Errorf("vals = %#v\nwant %#v", vals, want)
	}
	if len(warnings) != 2 {
		t.Errorf("warnings = %#v", warnings)
	}
	base.Apply(vals)
	if base.Get("EXCLUDE") != "/a,@b" || base.Source("EXCLUDE") != FromCLI {
		t.Errorf("Apply failed")
	}

	vals, _, _ = ApplyOps(base, []Op{{Spec: spec(t, "match", "--log-level"), Flag: "--log-level", Arg: "1"}, {Spec: spec(t, "match", "-v"), Flag: "-v"}})
	if vals["LOG_LEVEL"] != "2" {
		t.Errorf("incr after set = %q", vals["LOG_LEVEL"])
	}
	_, _, err = ApplyOps(base, []Op{{Spec: spec(t, "backup", "--initiator"), Flag: "--initiator"}})
	var oe *OptionError
	if !errors.As(err, &oe) || !errors.Is(err, ErrInvalidOption) || !strings.Contains(err.Error(), "--initiator is deprecated") {
		t.Errorf("invalid option err = %v", err)
	}
}

func TestOptionsHelpers(t *testing.T) {
	o := NewOptions()
	o.Set("A", "yes", FromEnv)
	o.SetDefault("A", "no")
	o.SetDefault("B", "3")
	o.Override("A", "x", FromEnvFile)
	o.Override("C", "c", FromPolicy)
	if o.Get("A") != "yes" || o.Get("B") != "3" || o.Get("C") != "c" || o.Int("B") != 3 {
		t.Errorf("store mismatch: %v", o.Keys())
	}
	c := o.Clone()
	c.Unset("A")
	if !o.Has("A") || c.Has("A") {
		t.Errorf("clone not independent")
	}
	if got := o.Environ(); !reflect.DeepEqual(got, []string{"ZELTA_A=yes", "ZELTA_B=3", "ZELTA_C=c"}) {
		t.Errorf("Environ = %#v", got)
	}
	for v, want := range map[string]bool{"": false, "0": false, "no": false, "False": false, "off": false, "00": false,
		"1": true, "yes": true, "ALWAYS": true, "2": true, "-1": true, "0.0": false} {
		if Truthy(v) != want {
			t.Errorf("Truthy(%q) = %v", v, !want)
		}
	}
	if FromCLI.String() != "command line" || FromDefault.String() != "default" {
		t.Errorf("Source.String")
	}
}

func TestRegistry(t *testing.T) {
	seen := map[string]map[string]bool{}
	for _, s := range Specs {
		if len(s.Flags) == 0 || len(s.Verbs) == 0 {
			t.Errorf("spec without flags or verbs: %#v", s)
		}
		for _, f := range s.Flags {
			if !strings.HasPrefix(f, "-") {
				t.Errorf("bad flag %q", f)
			}
		}
		if s.Type == Invalid && s.Warning == "" {
			t.Errorf("invalid spec without message: %v", s.Flags)
		}
		if !s.Hidden && s.Desc == "" {
			t.Errorf("visible spec without description: %v", s.Flags)
		}
		for _, v := range s.Verbs {
			if seen[v] == nil {
				seen[v] = map[string]bool{}
			}
		}
	}
	if !PolicyScoped("RETRY") || PolicyScoped("DEPTH") || !PolicyKey("DEPTH") || !PolicyKey("REMOTE_COMMAND") || PolicyKey("BOGUS") || PolicyKey("PRUNE_NUM") {
		t.Errorf("policy key classification")
	}
	if typ, ok := KeyType("SEND_INTR"); !ok || typ != True {
		t.Errorf("KeyType SEND_INTR = %v %v", typ, ok)
	}
	if _, ok := KeyType("NOPE"); ok {
		t.Errorf("KeyType NOPE")
	}
	if n, w, ok := LegacyKey("SEND_FLAGS"); !ok || n != "SEND_OVERRIDE" || w == "" {
		t.Errorf("LegacyKey")
	}
	if _, _, ok := LegacyKey("NOPE"); ok {
		t.Errorf("LegacyKey NOPE")
	}
	if !spec(t, "backup", "--depth").TakesArg() || spec(t, "backup", "--push").TakesArg() || !spec(t, "backup", "-X").TakesArg() {
		t.Errorf("TakesArg")
	}
}

func TestLoaderClock(t *testing.T) {
	l := testLoader(t, nil)
	tick := fixedNow
	l.Clock = func() time.Time { tick = tick.Add(time.Minute); return tick }
	a, _, _ := l.Load(context.Background(), "backup", nil)
	b, _, _ := l.Load(context.Background(), "backup", nil)
	if a.Get("SNAP_NAME") != "zelta_2026-01-02_03.05.05" || b.Get("SNAP_NAME") != "zelta_2026-01-02_03.06.05" {
		t.Errorf("each load gets the current time: %q %q", a.Get("SNAP_NAME"), b.Get("SNAP_NAME"))
	}
	if NewLoader().Clock == nil {
		t.Errorf("process loader has no clock")
	}
}

func TestParseEnvFileReadError(t *testing.T) {
	r := io.MultiReader(strings.NewReader("A=1\n"), iotest.ErrReader(errors.New("boom")))
	lines, warnings := ParseEnvFile(r)
	if !reflect.DeepEqual(lines, []EnvFileLine{{Key: "ZELTA_A", Expr: "1"}}) ||
		!reflect.DeepEqual(warnings, []string{"error reading zelta.env: boom"}) {
		t.Errorf("lines=%#v warnings=%#v", lines, warnings)
	}
}
