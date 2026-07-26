package sdk_test

import (
	"context"
	"strings"
	"testing"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/prune"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func TestBackupDryRun(t *testing.T) {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/src@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
			"pool/tgt": "pool/tgt\t33333\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/tgt@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
		},
	}
	req := backup.Request{
		DryRun: true,
		Source: "pool/src",
		Target: "pool/tgt",
		JSON:   true,
	}
	res, err := backup.Run(ctx, f, req)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "skipped") {
		t.Fatalf("expected dry-run output with skipped datasets, got: %q", res.Output)
	}
	if res.JSONReport == nil {
		t.Fatal("expected JSONReport with JSON:true")
	}
}

func TestMatchCompare(t *testing.T) {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\npool/src@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096",
			"pool/tgt": "pool/tgt\t33333\t0\t2024-01-01 00:00:00\t-\npool/tgt@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096",
		},
	}
	req := match.Request{
		Source: "pool/src",
		Target: "pool/tgt",
	}
	res, err := match.Compare(ctx, f, req)
	if err != nil {
		t.Fatal(err)
	}
	if res == nil {
		t.Fatal("expected non-nil result")
	}
}

func TestPruneRun(t *testing.T) {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\t-\t-",
		},
	}
	req := prune.Request{
		Source:     "pool/src",
		PruneGuard: prune.GuardLatest,
	}
	res, err := prune.Run(ctx, f, req)
	if err != nil {
		t.Fatal(err)
	}
	if c := res.Candidates(); len(c) != 0 {
		t.Fatalf("expected empty candidates, got %v", c)
	}
}
