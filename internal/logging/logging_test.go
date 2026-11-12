package logging

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
)

func newTest(t *testing.T, cfg Config) (*Logger, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	cfg.Stdout, cfg.Stderr = &out, &errb
	l, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return l, &out, &errb
}

func logAll(l *Logger) {
	l.Errorf("e%d", 0)
	l.Warnf("w")
	l.Noticef("n")
	l.Infof("i")
	l.Debugf("d")
}

func TestTerminalMode(t *testing.T) {
	l, out, errb := newTest(t, Config{Level: 4, Prefix: "[p] "})
	logAll(l)
	if got, want := out.String(), "[p] n\n"; got != want {
		t.Errorf("stdout = %q want %q", got, want)
	}
	if got, want := errb.String(), "error: [p] e0\nwarning: [p] w\n[p] i\ndebug: [p] d\n"; got != want {
		t.Errorf("stderr = %q want %q", got, want)
	}
	if got := l.Messages(); !reflect.DeepEqual(got, []string{"e0", "w"}) {
		t.Errorf("messages = %#v", got)
	}
}

func TestLevelFiltering(t *testing.T) {
	l, out, errb := newTest(t, Config{Level: 1})
	logAll(l)
	if out.String() != "" || errb.String() != "error: e0\nwarning: w\n" {
		t.Errorf("out=%q err=%q", out, errb)
	}
	l, out, errb = newTest(t, Config{Level: -1})
	logAll(l)
	if out.Len()+errb.Len() != 0 {
		t.Errorf("negative level wrote output")
	}
	if len(l.Messages()) != 2 {
		t.Errorf("messages are collected regardless of level")
	}
	if !l.Enabled(Error-1) || l.Enabled(Error) || l.Level() != -1 {
		t.Errorf("Enabled mismatch")
	}
}

func TestTextModeDropsPrefix(t *testing.T) {
	l, out, _ := newTest(t, Config{Level: 2, Mode: "text", Prefix: "[p] "})
	l.Noticef("n")
	if out.String() != "n\n" {
		t.Errorf("stdout = %q", out)
	}
}

func TestJSONMode(t *testing.T) {
	l, out, errb := newTest(t, Config{Level: 3, Mode: "json"})
	logAll(l)
	if out.Len() != 0 {
		t.Errorf("json mode wrote stdout: %q", out)
	}
	if got, want := errb.String(), "error: e0\nwarning: w\nnotice: n\ninfo: i\n"; got != want {
		t.Errorf("stderr = %q want %q", got, want)
	}
}

func TestLogFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "zelta.log")
	l, out, errb := newTest(t, Config{Level: 2, File: path})
	l.Noticef("hello")
	l.Warnf("careful")
	fmt.Fprintln(l.Stdout(), "direct")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got, want := string(data), "notice: hello\nwarning: careful\ndirect\n"; got != want {
		t.Errorf("file = %q want %q", got, want)
	}
	if out.Len()+errb.Len() != 0 {
		t.Errorf("log file mode wrote to streams")
	}
	if _, err := New(Config{File: filepath.Join(t.TempDir(), "missing", "x.log")}); err == nil {
		t.Errorf("expected open error")
	}
}

func TestEmptyMessageAndDerived(t *testing.T) {
	l, out, errb := newTest(t, Config{Level: 4, Prefix: "x "})
	l.Noticef("")
	d := l.With("[job] ")
	d.Noticef("hi")
	d.Warnf("w")
	q := l.WithLevel(0)
	q.Noticef("hidden")
	fmt.Fprint(l.Stderr(), "raw\n")
	if got, want := out.String(), "missing log message\n[job] hi\n"; got != want {
		t.Errorf("stdout = %q want %q", got, want)
	}
	if got, want := errb.String(), "warning: [job] w\nraw\n"; got != want {
		t.Errorf("stderr = %q want %q", got, want)
	}
	if len(l.Messages()) != 0 || len(d.Messages()) != 1 {
		t.Errorf("derived loggers must collect separately")
	}
	if Discard().Close() != nil {
		t.Errorf("discard close")
	}
}

func TestConcurrentLogging(t *testing.T) {
	l, out, _ := newTest(t, Config{Level: 2})
	var wg sync.WaitGroup
	for i := range 50 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l.With(fmt.Sprintf("[%d] ", i)).Noticef("line")
		}(i)
	}
	wg.Wait()
	if n := bytes.Count(out.Bytes(), []byte("\n")); n != 50 {
		t.Errorf("got %d lines", n)
	}
}

func TestParseRecord(t *testing.T) {
	tests := []struct {
		in  string
		lvl Level
		msg string
	}{
		{"2\x1chello", Notice, "hello"},
		{"0\x1cboom", Error, "boom"},
		{"4\x1c", Debug, ""},
		{"plain text", Notice, "plain text"},
		{"x\x1cy", Notice, "x y"},
		{"\x1conly", Notice, "only"},
	}
	for _, tt := range tests {
		lvl, msg := Parse(tt.in)
		if lvl != tt.lvl || msg != tt.msg {
			t.Errorf("Parse(%q) = %d,%q", tt.in, lvl, msg)
		}
	}
	for l, want := range map[Level]string{Error: "error: ", Warning: "warning: ", Notice: "notice: ", Info: "info: ", Debug: "debug: ", 9: "error: "} {
		if l.Prefix() != want {
			t.Errorf("prefix %d = %q", l, l.Prefix())
		}
	}
}

func TestHandler(t *testing.T) {
	var got []string
	var out, errb bytes.Buffer
	l, _ := New(Config{Level: 2, Prefix: "p ", Stdout: &out, Stderr: &errb, Handler: func(lvl Level, msg string) {
		got = append(got, lvl.Prefix()+msg)
	}})
	logAll(l)
	if want := []string{"error: p e0", "warning: p w", "notice: p n"}; !reflect.DeepEqual(got, want) {
		t.Errorf("handler got %#v", got)
	}
	if out.Len()+errb.Len() != 0 {
		t.Errorf("handler output also went to streams")
	}
}
