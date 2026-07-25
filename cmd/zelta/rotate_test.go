package main

import (
	"testing"

	"git.belltower.it/djbell/zelta-go/internal/match"
)

func TestPrepareRotateSnapshotWhenSourceIsAtMatch(t *testing.T) {
	pairs := []*match.Pair{{SrcName: "tank/src", Match: "@base", SrcLast: "@base"}}
	savepoint, ok, err := prepareRotateSnapshot("IF_NEEDED", "manual", pairs)
	if err != nil || !ok || savepoint != "@manual" {
		t.Fatalf("result=%q %v %v", savepoint, ok, err)
	}
	if pairs[0].SrcLast != "@manual" {
		t.Fatalf("source last=%q", pairs[0].SrcLast)
	}
}

func TestPrepareRotateSnapshotHonorsNoSnapshot(t *testing.T) {
	pairs := []*match.Pair{{SrcName: "tank/src", Match: "@base", SrcLast: "@base"}}
	if _, _, err := prepareRotateSnapshot("SKIP", "", pairs); err == nil {
		t.Fatal("expected snapshot required error")
	}
}
