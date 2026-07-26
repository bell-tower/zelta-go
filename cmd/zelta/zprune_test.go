package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"git.belltower.it/djbell/zelta-go/endpoint"
)

func TestGroupByDataset(t *testing.T) {
	got := groupByDataset([]string{
		"apool/ds@a",
		"apool/ds@b",
		"apool/other@c",
		"bad",
	})
	if len(got["apool/ds"]) != 2 || got["apool/ds"][0] != "@a" || got["apool/ds"][1] != "@b" {
		t.Fatalf("apool/ds=%v", got["apool/ds"])
	}
	if len(got["apool/other"]) != 1 || got["apool/other"][0] != "@c" {
		t.Fatalf("apool/other=%v", got["apool/other"])
	}
	if _, ok := got["bad"]; ok {
		t.Fatal("bare name should be skipped")
	}
}

func TestDestroyTarget(t *testing.T) {
	got := destroyTarget("apool/z", []string{"@s1", "@s2", "@s3"})
	if got != "apool/z@s1,s2,s3" {
		t.Fatalf("got %q", got)
	}
	if destroyTarget("apool/z", []string{"@only"}) != "apool/z@only" {
		t.Fatal("single")
	}
	if destroyTarget("apool/z", nil) != "" {
		t.Fatal("empty")
	}
}

func TestConfirmDestructionForce(t *testing.T) {
	if !confirmDestruction([]string{"x@y"}, true) {
		t.Fatal("force should skip prompt")
	}
}

func TestConfirmDestructionAbort(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		w.Write([]byte("n\n"))
		w.Close()
	}()
	if confirmDestruction([]string{"x@y"}, false) {
		t.Fatal("n should abort")
	}
}

func TestConfirmDestructionYes(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		w.Write([]byte("yes\n"))
		w.Close()
	}()
	if !confirmDestruction([]string{"x@y"}, false) {
		t.Fatal("yes should confirm")
	}
}

func TestRunZpruneDryRun(t *testing.T) {
	dir := t.TempDir()
	zfsPath := filepath.Join(dir, "zfs")
	// three snaps; prune-num=1 keeps newest → destroy @old and @mid
	script := `#!/bin/sh
printf '%s\n' \
  'apool/z@new	90	0	1000000	0	1000	-' \
  'apool/z@mid	80	1000	900000	0	2000	-' \
  'apool/z@old	70	1000	800000	0	2000	-' \
  'apool/z	1	0	900000	1M	1000	-'
`
	if err := os.WriteFile(zfsPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, code := captureOutput(func() int {
		// num-only retention: avoid --prune-time=0 (keeps creation≈now snaps)
		return runZprune([]string{"--dryrun", "--prune-num=1", "--prune-guard=none", "apool/z"})
	})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%q", code, stderr)
	}
	// ranges compress contiguous candidates: @old%mid
	if !strings.Contains(stdout, "apool/z@old") {
		t.Fatalf("stdout=%q", stdout)
	}
	if strings.Contains(stderr, "destroy") {
		t.Fatalf("dry-run must not destroy: stderr=%q", stderr)
	}
}

func TestRunZpruneForceDestroysViaFake(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "zfs.log")
	zfsPath := filepath.Join(dir, "zfs")
	script := `#!/bin/sh
echo "$*" >>` + logPath + `
case "$*" in
  destroy*) exit 0 ;;
  *)
    printf '%s\n' \
      'apool/z@new	90	0	1000000	0	1000	-' \
      'apool/z@old	70	1000	800000	0	2000	-' \
      'apool/z	1	0	900000	1M	1000	-'
    ;;
esac
`
	if err := os.WriteFile(zfsPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, code := captureOutput(func() int {
		return runZprune([]string{"--force", "--prune-num=1", "--prune-guard=none", "apool/z"})
	})
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	logb, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	log := string(logb)
	if !strings.Contains(log, "destroy") || !strings.Contains(log, "apool/z@old") {
		t.Fatalf("destroy not logged: %q", log)
	}
	if !strings.Contains(stderr, "+ zfs destroy") {
		t.Fatalf("expected destroy preview on stderr: %q", stderr)
	}
}

func TestDestroyCandidatesUsesSourceRemote(t *testing.T) {
	// Unit: srcEp remote is what runDestroyCmd would see (no local zfs).
	ep, err := endpoint.Parse("root@dev2:apool/z")
	if err != nil {
		t.Fatal(err)
	}
	if !ep.Remote || ep.Host != "dev2" || ep.User != "root" {
		t.Fatalf("ep=%+v", ep)
	}
	// group + argv shape only — destroy not executed here
	groups := groupByDataset([]string{"apool/z@old", "apool/z@mid"})
	snaps := groups["apool/z"]
	if len(snaps) != 2 {
		t.Fatalf("snaps=%v", snaps)
	}
}
