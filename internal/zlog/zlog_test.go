package zlog

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withCapture redirects stdout/stderr for a single call.
func withCapture(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()
	oldOut, oldErr := os.Stdout, os.Stderr
	rOut, wOut, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	rErr, wErr, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = wOut, wErr
	fn()
	os.Stdout, os.Stderr = oldOut, oldErr
	wOut.Close()
	wErr.Close()
	var so, se bytes.Buffer
	if _, err := io.Copy(&so, rOut); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&se, rErr); err != nil {
		t.Fatal(err)
	}
	return so.String(), se.String()
}

func TestLevelFiltering(t *testing.T) {
	s, err := New(Notice, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stdout, stderr := withCapture(t, func() {
		s.Error("boom")
		s.Warning("careful")
		s.Notice("see me")
		s.Info("hidden")
		s.Debug("hidden")
	})
	if !strings.Contains(stderr, "error: boom") {
		t.Errorf("stderr missing error: %q", stderr)
	}
	if !strings.Contains(stderr, "warning: careful") {
		t.Errorf("stderr missing warning: %q", stderr)
	}
	if stdout != "see me\n" {
		t.Errorf("stdout = %q, want notice line", stdout)
	}
	if strings.Contains(stderr, "hidden") {
		t.Errorf("info/debug leaked at notice max: %q", stderr)
	}
}

func TestTerminalModes(t *testing.T) {
	// LOG_MODE=json: everything prefixed to stderr, including notices.
	s, err := New(Info, "json", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stdout, stderr := withCapture(t, func() {
		s.Notice("n")
		s.Info("i")
	})
	if stdout != "" {
		t.Errorf("json mode wrote stdout: %q", stdout)
	}
	if !strings.Contains(stderr, "notice: n") || !strings.Contains(stderr, "info: i") {
		t.Errorf("json mode stderr = %q", stderr)
	}
}

func TestInfoToStderrUnprefixed(t *testing.T) {
	s, err := New(Info, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, stderr := withCapture(t, func() { s.Info("listing source: x") })
	if stderr != "listing source: x\n" {
		t.Errorf("info stderr = %q, want unprefixed line", stderr)
	}
}

func TestDebugPrefixed(t *testing.T) {
	s, err := New(Debug, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	_, stderr := withCapture(t, func() { s.Debug("`zfs list`") })
	if stderr != "debug: `zfs list`\n" {
		t.Errorf("debug stderr = %q", stderr)
	}
}

func TestLogFileAppend(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "zelta.log")
	s, err := New(Debug, "", "job: ", path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.Notice("hello")
	s.Close()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "notice: job: hello\n" {
		t.Errorf("file = %q", data)
	}
	// Append, not truncate.
	s2, err := New(Debug, "", "", path)
	if err != nil {
		t.Fatal(err)
	}
	s2.Info("again")
	s2.Close()
	data, _ = os.ReadFile(path)
	if string(data) != "notice: job: hello\ninfo: again\n" {
		t.Errorf("file after append = %q", data)
	}
}

func TestTextModeDropsPrefix(t *testing.T) {
	s, err := New(Notice, "text", "job: ", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stdout, _ := withCapture(t, func() { s.Notice("x") })
	if stdout != "x\n" {
		t.Errorf("text mode kept prefix: %q", stdout)
	}
}

func TestLimit(t *testing.T) {
	s, err := New(Debug, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	child := s.Limit(Notice)
	if child.Enabled(Info) {
		t.Error("Limit(Notice) still enabled for info")
	}
	if !child.Enabled(Notice) {
		t.Error("Limit(Notice) disabled for notice")
	}
	if !s.Enabled(Debug) {
		t.Error("original sink lost debug")
	}
}

func TestEmptyMessage(t *testing.T) {
	s, err := New(Notice, "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	stdout, _ := withCapture(t, func() { s.Notice("") })
	if stdout != "missing log message\n" {
		t.Errorf("empty notice = %q", stdout)
	}
}
