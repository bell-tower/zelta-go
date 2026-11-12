package app

import (
	"bytes"
	"context"
	"time"

	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/jsonout"
	"github.com/bell-tower/zelta-go/internal/logging"
)

// TestRuntime is a Runtime wired to buffers and a fake executor, for tests.
type TestRuntime struct {
	*Runtime
	Fake           *execx.Fake
	Out, Err       *bytes.Buffer // leveled log streams
	RawOut, RawErr *bytes.Buffer // Runtime.Stdout / Runtime.Stderr
}

// TestNow is the fixed clock used by NewTest: 2026-01-02 03:04:05 UTC.
var TestNow = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

// NewTest builds a test runtime for verb with the given option overrides,
// applied on top of the built-in defaults. Logging uses LOG_LEVEL and
// LOG_MODE from the options.
func NewTest(verb string, opts map[string]string) *TestRuntime {
	o := config.NewOptions()
	for k, v := range opts {
		o.Set(k, v, config.FromCLI)
	}
	o.SetDefault("VERB", verb)
	o.SetDefault("HOSTNAME", "testhost")
	ApplyTestDefaults(o, verb)
	tr := &TestRuntime{Fake: execx.NewFake(), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, RawOut: &bytes.Buffer{}, RawErr: &bytes.Buffer{}}
	log, _ := logging.New(logging.Config{Level: o.Int("LOG_LEVEL"), Mode: o.Get("LOG_MODE"), Prefix: o.Get("LOG_PREFIX"), Stdout: tr.Out, Stderr: tr.Err})
	var doc *jsonout.Document
	if o.Get("LOG_MODE") == "json" {
		doc = jsonout.New("zelta " + o.Get("VERB"))
	}
	tr.Runtime = &Runtime{
		Opts: o, Log: log, Exec: tr.Fake,
		Stdin: &bytes.Buffer{}, Stdout: tr.RawOut, Stderr: tr.RawErr,
		Now: func() time.Time { return TestNow }, Hostname: "testhost", User: "tester",
		JSON: doc,
	}
	return tr
}

// ApplyTestDefaults fills the built-in defaults for verb into o without
// reading the environment or zelta.env.
func ApplyTestDefaults(o *config.Options, verb string) {
	l := &config.Loader{Env: map[string]string{"ZELTA_ENV": "/nonexistent/zelta.env", "ZELTA_ETC": "/nonexistent"}, Now: TestNow, Hostname: "testhost", User: "tester", DirExists: func(string) bool { return false }}
	base, _, _ := l.Load(context.Background(), verb, nil)
	for _, k := range base.Keys() {
		if _, ok := o.Lookup(k); !ok {
			o.Set(k, base.Get(k), base.Source(k))
		}
	}
}
