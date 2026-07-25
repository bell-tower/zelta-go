package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestRunPruneGolden(t *testing.T) {
	dir := filepath.Join("..", "..", "testdata", "golden", "prune", "latest-guard")
	src, err := os.ReadFile(filepath.Join(dir, "src.list"))
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := os.ReadFile(filepath.Join(dir, "tgt.list"))
	if err != nil {
		t.Fatal(err)
	}
	wantOut, err := os.ReadFile(filepath.Join(dir, "expected.out"))
	if err != nil {
		t.Fatal(err)
	}
	wantErrBytes, err := os.ReadFile(filepath.Join(dir, "expected.err"))
	if err != nil {
		t.Fatal(err)
	}

	fakeDir := t.TempDir()
	srcPath := filepath.Join(fakeDir, "src.list")
	tgtPath := filepath.Join(fakeDir, "tgt.list")
	if err := os.WriteFile(srcPath, src, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(tgtPath, tgt, 0o600); err != nil {
		t.Fatal(err)
	}
	zfsPath := filepath.Join(fakeDir, "zfs")
	script := fmt.Sprintf("#!/bin/sh\ncase \"$*\" in\n  *bpool/tgt) cat %q ;;\n  *) cat %q ;;\nesac\n", tgtPath, srcPath)
	if err := os.WriteFile(zfsPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, code := captureOutput(func() int {
		return runPrune([]string{
			"--prune-num=0",
			"--prune-time=0",
			"--prune-guard=latest",
			"--match-endpoint=bpool/tgt",
			"apool/treetop",
		})
	})
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if stdout != string(wantOut) {
		t.Errorf("stdout mismatch\ngot:\n%s\nwant:\n%s", stdout, wantOut)
	}
	if stderr != string(wantErrBytes) {
		t.Errorf("stderr mismatch\ngot:\n%s\nwant:\n%s", stderr, wantErrBytes)
	}
}
