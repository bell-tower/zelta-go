package sdk_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"git.belltower.it/djbell/zelta-go/backup"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/prune"
	"git.belltower.it/djbell/zelta-go/zfs"
)

// External-module smoke: public packages only (no internal/).

func TestBackupDryRun(t *testing.T) {
	ctx := context.Background()
	f := &zfs.Fake{
		Lists: map[string]string{
			"pool/src": "pool/src\t11111\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/src@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
			"pool/tgt": "pool/tgt\t33333\t0\t2024-01-01 00:00:00\t-\tfilesystem\t-\t-\t-\npool/tgt@snap1\t22222\t1024\t2024-01-01 01:00:00\t4096\t-\t-\t-\t-",
		},
	}
	flags := backup.DefaultSendRecv()
	req := backup.Request{
		DryRun:   true,
		Source:   endpoint.Endpoint{Dataset: "pool/src"},
		Target:   endpoint.Endpoint{Dataset: "pool/tgt"},
		SnapMode: backup.SnapNever,
		Flags:    &flags,
		JSON:     true,
		OnLine:   func(string) {},
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

func TestBackupFromParseHelpers(t *testing.T) {
	src, err := endpoint.Parse("pool/src")
	if err != nil {
		t.Fatal(err)
	}
	tgt, err := endpoint.Parse("pool/tgt")
	if err != nil {
		t.Fatal(err)
	}
	st, err := backup.ParseSnapTime("1h")
	if err != nil || st != time.Hour {
		t.Fatalf("ParseSnapTime: %v %v", st, err)
	}
	mode, err := backup.ParseSnapMode("0")
	if err != nil {
		t.Fatal(err)
	}
	dir, err := backup.ParseSyncDirection("pull")
	if err != nil {
		t.Fatal(err)
	}
	req := backup.Request{
		Source:        src,
		Target:        tgt,
		SnapMode:      mode,
		SyncDirection: dir,
		DryRun:        true,
	}
	f := &zfs.Fake{Lists: map[string]string{
		"pool/src": "pool/src\t1\t0\t1\t1K\tfilesystem\t-\t-\t-\npool/src@a\t2\t0\t2\t1K\t-\t-\t-\t-",
		"pool/tgt": "pool/tgt\t1\t0\t1\t1K\tfilesystem\t-\t-\t-\npool/tgt@a\t2\t0\t2\t1K\t-\t-\t-\t-",
	}}
	if _, err := backup.Run(context.Background(), f, req); err != nil {
		t.Fatal(err)
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
		Source: endpoint.MustParse("pool/src"),
		Target: endpoint.MustParse("pool/tgt"),
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
	g, err := prune.ParsePruneGuard("latest")
	if err != nil {
		t.Fatal(err)
	}
	pt, err := prune.ParsePruneTime("0")
	if err != nil {
		t.Fatal(err)
	}
	req := prune.Request{
		Source:     endpoint.MustParse("pool/src"),
		PruneGuard: g,
		PruneNum:   1,
		PruneTime:  pt,
	}
	res, err := prune.Run(ctx, f, req)
	if err != nil {
		t.Fatal(err)
	}
	if c := res.Candidates(); len(c) != 0 {
		t.Fatalf("expected empty candidates, got %v", c)
	}
}
