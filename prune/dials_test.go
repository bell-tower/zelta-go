package prune

import (
	"testing"
	"time"
)

func TestParsePruneGuard(t *testing.T) {
	g, err := ParsePruneGuard("")
	if err != nil || g != GuardLatest {
		t.Fatalf("empty: %v %v", g, err)
	}
	g, err = ParsePruneGuard("UNSYNCED")
	if err != nil || g != GuardUnsynced {
		t.Fatalf("unsynced: %v %v", g, err)
	}
	if _, err := ParsePruneGuard("nope"); err == nil {
		t.Fatal("want error")
	}
}

func TestParsePruneTime(t *testing.T) {
	d, err := ParsePruneTime("")
	if err != nil || d != nil {
		t.Fatalf("empty: %v %v", d, err)
	}
	d, err = ParsePruneTime("0")
	if err != nil || d == nil || *d != 0 {
		t.Fatalf("0: %v %v", d, err)
	}
	d, err = ParsePruneTime("2days")
	if err != nil || d == nil || *d != 2*24*time.Hour {
		t.Fatalf("2days: %v %v", d, err)
	}
	if _, err := ParsePruneTime("bogus"); err == nil {
		t.Fatal("want error")
	}
}
