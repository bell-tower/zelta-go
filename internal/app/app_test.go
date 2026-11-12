package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/execx"
)

func TestErrors(t *testing.T) {
	if err := Fail("source '%s' missing", "x"); err.Error() != "source 'x' missing" || Code(err) != 1 {
		t.Errorf("Fail = %v (%d)", err, Code(err))
	}
	if err := Exit(2, "boom"); err.Error() != "boom" || Code(err) != 2 {
		t.Errorf("Exit = %v", err)
	}
	if err := (&ExitError{Code: 3}); err.Error() != "exit status 3" || Code(err) != 3 {
		t.Errorf("ExitError = %v", err)
	}
	if err := Usage("bad %s", "list"); err.Error() != "bad list" || Code(err) != 1 {
		t.Errorf("Usage = %v", err)
	}
	if Usage("plain").Error() != "plain" || Code(nil) != 0 || Code(errors.New("x")) != 1 {
		t.Errorf("Code")
	}
	if SanitizeLine("a\rb\r") != "ab" {
		t.Errorf("SanitizeLine")
	}
}

func TestRuntimeHelpers(t *testing.T) {
	tr := NewTest("backup", map[string]string{"HOSTNAME": "box", "REMOTE_RECV": "ssh -T", "DRYRUN": "1", "LOG_LEVEL": "4"})
	ep := tr.Endpoint("pool/ds")
	if ep.Host != "box" || ep.User != "tester" {
		t.Errorf("endpoint = %+v", ep)
	}
	tr.Opts.Set("HOSTNAME", "", config.FromCLI)
	if tr.Endpoint("pool").Host != "testhost" {
		t.Errorf("hostname fallback")
	}
	remote := tr.Endpoint("me@h:pool")
	if r := tr.Remote(remote, "RECV"); r == nil || strings.Join(r.Prefix, " ") != "ssh -T" {
		t.Errorf("recv remote = %+v", r)
	}
	if r := tr.Remote(remote, ""); r == nil || strings.Join(r.Prefix, " ") != "ssh -n" {
		t.Errorf("default remote = %+v", r)
	}
	if !tr.DryRun() || tr.Unix() != TestNow.Unix() {
		t.Errorf("DryRun/Unix")
	}
	tr.Fake.On("zfs list", execx.Out("x"))
	if res := tr.Run(context.Background(), command.New(command.Words("zfs", "list")...)); !res.OK() || tr.Err.String() != "debug: `zfs list`\n" {
		t.Errorf("Run = %+v, log %q", res, tr.Err.String())
	}
	tr.Report("ignored", 1) // no JSON document: no-op
	tr.TTY = func() (io.ReadWriteCloser, error) { return nil, errors.New("no tty") }
	if _, err := tr.OpenTTY(); err == nil {
		t.Errorf("custom TTY")
	}
	tr.TTY = nil
	if f, err := tr.OpenTTY(); err == nil {
		f.Close()
	}
}

func base(out, errb *bytes.Buffer) *Runtime {
	return &Runtime{Exec: execx.NewFake(), Stdout: out, Stderr: errb, Hostname: "h", User: "u"}
}

func TestBuildAndFinish(t *testing.T) {
	var out, errb bytes.Buffer
	o := config.NewOptions()
	o.Set("LOG_LEVEL", "2", config.FromDefault)
	o.Set("VERB", "backup", config.FromDefault)
	rt, err := Build(base(&out, &errb), o)
	if err != nil || rt.JSON != nil {
		t.Fatalf("text build: %v %v", err, rt.JSON)
	}
	rt.Log.Noticef("hello")
	if code := Finish(rt, Fail("broken")); code != 1 || out.String() != "hello\n" || errb.String() != "error: broken\n" {
		t.Errorf("finish: %d %q %q", code, out.String(), errb.String())
	}

	out.Reset()
	errb.Reset()
	rt, _ = Build(base(&out, &errb), o)
	if code := Finish(rt, errors.New("plain")); code != 1 || errb.String() != "error: plain\n" {
		t.Errorf("plain error: %q", errb.String())
	}
	errb.Reset()
	rt, _ = Build(base(&out, &errb), o)
	if code := Finish(rt, Usage("u")); code != 1 || errb.Len() != 0 {
		t.Errorf("usage errors are printed by the CLI: %q", errb.String())
	}
	rt, _ = Build(base(&out, &errb), o)
	if code := Finish(rt, &ExitError{Code: 2}); code != 2 || errb.Len() != 0 {
		t.Errorf("silent exit: %q", errb.String())
	}

	out.Reset()
	o.Set("LOG_MODE", "json", config.FromCLI)
	o.Set("JSON_PRETTY", "1", config.FromCLI)
	rt, _ = Build(base(&out, &errb), o)
	rt.Report("replicationErrorCode", 0)
	rt.Log.Warnf("careful")
	if code := Finish(rt, nil); code != 0 || !strings.Contains(out.String(), "\"command\": \"zelta backup\",\n") || !strings.Contains(out.String(), "\"careful\"") {
		t.Errorf("pretty json = %q", out.String())
	}

	dir := t.TempDir()
	file := filepath.Join(dir, "zelta.json")
	o.Set("LOG_FILE", file, config.FromCLI)
	o.Set("JSON_PRETTY", "0", config.FromCLI)
	out.Reset()
	rt, _ = Build(base(&out, &errb), o)
	Finish(rt, nil)
	data, _ := os.ReadFile(file)
	if out.Len() != 0 || !strings.HasPrefix(string(data), `{"output_version"`) {
		t.Errorf("json log file: stdout=%q file=%q", out.String(), data)
	}

	errb.Reset()
	o.Set("LOG_FILE", filepath.Join(dir, "missing", "x.json"), config.FromCLI)
	rt, _ = Build(base(&out, &errb), o)
	Finish(rt, nil)
	if !strings.Contains(errb.String(), "cannot open log file") {
		t.Errorf("unwritable json file: %q", errb.String())
	}

	errb.Reset()
	o.Set("LOG_MODE", "", config.FromCLI)
	rt, _ = Build(base(&out, &errb), o)
	if !strings.Contains(errb.String(), "cannot open log file") || rt == nil {
		t.Errorf("unwritable log file: %q", errb.String())
	}
}

func TestDefaultTTY(t *testing.T) {
	if f, err := DefaultTTY(); err == nil {
		f.Close()
	}
}
