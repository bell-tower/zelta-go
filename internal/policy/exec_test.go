package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fakeZelta(t *testing.T, dir, script string) {
	t.Helper()
	p := filepath.Join(dir, "zelta")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRun(t *testing.T) {
	fakeDir := t.TempDir()
	fakeZelta(t, fakeDir, `echo "zelta $@" >&2; exit 0`)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nEXCLUDE: /tmp\nS:\n  h:\n  - pool/a\n  - pool/b\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}

	results := Run(jobs)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for i, r := range results {
		if r.Err != nil {
			t.Fatalf("job %d err: %v", i, r.Err)
		}
	}
}

func TestRunFailure(t *testing.T) {
	fakeDir := t.TempDir()
	fakeZelta(t, fakeDir, `exit 1`)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nS:\n  h:\n  - p/q\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}

	results := Run(jobs)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Err == nil {
		t.Fatal("expected error for failing job")
	}
}

func TestRunMixedSuccessFailure(t *testing.T) {
	fakeDir := t.TempDir()
	fakeZelta(t, fakeDir, `case "$2" in *pool/b) exit 1;; *) exit 0;; esac`)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nS:\n  h:\n  - pool/a\n  - pool/b\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}

	results := Run(jobs)
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].Err != nil {
		t.Fatalf("job 0 should succeed: %v", results[0].Err)
	}
	if results[1].Err == nil {
		t.Fatal("job 1 should fail")
	}
}

func TestRunEmptyJobs(t *testing.T) {
	results := Run(nil)
	if len(results) != 0 {
		t.Fatalf("expected 0 results for nil jobs")
	}
}

func TestRunEnvForwarding(t *testing.T) {
	fakeDir := t.TempDir()
	out := filepath.Join(fakeDir, "env.out")
	fakeZelta(t, fakeDir,
		`echo "EXCLUDE=$ZELTA_EXCLUDE" > `+out+`; echo "SNAP_TIME=$ZELTA_SNAP_TIME" >> `+out+`; echo "LOG_PREFIX=$ZELTA_LOG_PREFIX" >> `+out)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nEXCLUDE: /tmp,/swap\nSNAP_TIME: 4h\nS:\n  s:\n  - pool/a\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}

	results := Run(jobs)
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("expected success, got %v", results[0].Err)
	}

	b, _ := os.ReadFile(out)
	got := string(b)
	if !strings.Contains(got, "EXCLUDE=/tmp,/swap") {
		t.Fatalf("no EXCLUDE in env: %q", got)
	}
	if !strings.Contains(got, "SNAP_TIME=4h") {
		t.Fatalf("no SNAP_TIME in env: %q", got)
	}
	if !strings.Contains(got, "LOG_PREFIX=[S: tank/B/a] s:pool/a: ") {
		t.Fatalf("no LOG_PREFIX in env: %q", got)
	}
}

func TestRunSkipsPolicyScopeVars(t *testing.T) {
	fakeDir := t.TempDir()
	out := filepath.Join(fakeDir, "env.out")
	fakeZelta(t, fakeDir,
		`echo "JOBS=$ZELTA_JOBS" > `+out+`; echo "BACKUP_ROOT=$ZELTA_BACKUP_ROOT" >> `+out+`; echo "EXCLUDE=$ZELTA_EXCLUDE" >> `+out)
	t.Setenv("PATH", fakeDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nJOBS: 4\nEXCLUDE: /tmp\nS:\n  h:\n  - pool/a\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}

	results := Run(jobs)
	if len(results) != 1 || results[0].Err != nil {
		t.Fatalf("expected success, got %v", results[0].Err)
	}

	b, _ := os.ReadFile(out)
	got := string(b)
	if strings.Contains(got, "JOBS=4") {
		t.Fatalf("JOBS should not be forwarded: %q", got)
	}
	if strings.Contains(got, "BACKUP_ROOT=tank/B") {
		t.Fatalf("BACKUP_ROOT should not be forwarded: %q", got)
	}
	if !strings.Contains(got, "EXCLUDE=/tmp") {
		t.Fatalf("EXCLUDE should be present: %q", got)
	}
}
