package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunCloneAndBackupDryRun(t *testing.T) {
	dir := t.TempDir()
	cloneList := filepath.Join(dir, "clone.list")
	backupSourceList := filepath.Join(dir, "backup-source.list")
	originList := filepath.Join(dir, "origin.list")
	if err := os.WriteFile(cloneList, []byte("tank/source@base\tsnapshot\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backupSourceList, []byte("tank/clone\t10\t0\t100\t1K\tfilesystem\ttank/source@base\ntank/clone@new\t11\t0\t200\t1K\tsnapshot\t-\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(originList, []byte("backup/source\t1\t0\t100\t1K\tfilesystem\t-\nbackup/source@base\t2\t0\t200\t1K\tsnapshot\t-\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	zfsPath := filepath.Join(dir, "zfs")
	script := fmt.Sprintf(`#!/bin/sh
case "$*" in
  *"-o name,type"*"tank/source") cat %q ;;
  *"name tank/clone") echo "cannot open 'tank/clone': dataset does not exist" >&2; exit 1 ;;
  *"name,guid,written,creation,used,type,encryption,ivsetguid,receive_resume_token,origin"*"tank/clone") cat %q ;;
  *"name,guid,written,creation,used,type,encryption,ivsetguid,receive_resume_token,origin"*"backup/source") cat %q ;;
  *"name,guid,written,creation,used,type,encryption,ivsetguid,receive_resume_token,origin"*"backup/clone") ;;
  *"name backup") echo "cannot open 'backup': dataset does not exist" >&2; exit 1 ;;
  *) echo "unexpected zfs argv: $*" >&2; exit 1 ;;
esac
`, cloneList, backupSourceList, originList)
	if err := os.WriteFile(zfsPath, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))

	stdout, stderr, code := captureOutput(func() int {
		return runClone([]string{
			"-n", "--no-snapshot",
			"tank/source", "tank/clone", "backup/source", "backup/clone",
		})
	})
	if code != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	cloneAt := strings.Index(stdout, "zfs clone")
	sendAt := strings.Index(stdout, "zfs send")
	if cloneAt < 0 || sendAt < 0 || cloneAt > sendAt {
		t.Fatalf("workflow order missing:\n%s", stdout)
	}
	if !strings.Contains(stdout, "-i tank/source@base tank/clone@new") ||
		!strings.Contains(stdout, "-o origin=backup/source@base") {
		t.Fatalf("origin backup plan missing:\n%s", stdout)
	}
	if stderr != "" {
		t.Fatalf("stderr=%q", stderr)
	}
}
