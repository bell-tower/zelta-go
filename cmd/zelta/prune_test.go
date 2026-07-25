package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestRunPruneWithFakeZFS(t *testing.T) {
	dir := t.TempDir()
	zfsPath := filepath.Join(dir, "zfs")
	script := `#!/bin/sh
printf '%s\n' 'apool/treetop@new	90	0	1000000	0	1000	-' 'apool/treetop@old	70	1000	800000	0	2000	-' 'apool/treetop	1	0	900000	1M	1000	-'
`
	if err := os.WriteFile(zfsPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	originalPath := os.Getenv("PATH")
	t.Setenv("PATH", dir+string(os.PathListSeparator)+originalPath)

	stdout, stderr, code := captureOutput(func() int {
		return runPrune([]string{"--prune-num", "1", "--prune-time", "0", "apool/treetop"})
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	if stdout != "apool/treetop@old\n" {
		t.Fatalf("stdout=%q", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr=%q", stderr)
	}
}

func TestRunPruneRejectsMissingOperand(t *testing.T) {
	stdout, stderr, code := captureOutput(func() int { return runPrune(nil) })
	if code != 2 || stdout != "" || stderr != "usage: zelta prune [OPTIONS] ENDPOINT\nReports snapshot prune candidates on ENDPOINT.\n" {
		t.Fatalf("stdout=%q stderr=%q exit=%d", stdout, stderr, code)
	}
}

func captureOutput(fn func() int) (stdout, stderr string, code int) {
	oldOut, oldErr := os.Stdout, os.Stderr
	outR, outW, _ := os.Pipe()
	errR, errW, _ := os.Pipe()
	os.Stdout, os.Stderr = outW, errW
	code = fn()
	outW.Close()
	errW.Close()
	var outBuf, errBuf bytes.Buffer
	outBuf.ReadFrom(outR)
	errBuf.ReadFrom(errR)
	os.Stdout, os.Stderr = oldOut, oldErr
	return outBuf.String(), errBuf.String(), code
}
