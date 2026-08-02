package main

import (
	"os"
	"testing"
)

func TestRunReportMissingOperands(t *testing.T) {
	// Clear BACKUP_ROOT so the usage/missing path is hit.
	t.Setenv("ZELTA_BACKUP_ROOT", "")
	os.Unsetenv("ZELTA_BACKUP_ROOT")
	os.Unsetenv("BACKUP_ROOT")
	if code := runReport(nil); code != 1 {
		t.Fatalf("exit=%d want 1", code)
	}
}

func TestRunReportUsage(t *testing.T) {
	if code := runReport([]string{"-h"}); code != 0 {
		t.Fatalf("exit=%d want 0", code)
	}
}
