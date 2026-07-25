package rotate

import (
	"testing"

	"git.belltower.it/djbell/zelta-go/internal/opt"
)

func TestDirectDivergencePlan(t *testing.T) {
	steps, err := Plan(Request{
		Source: "tank/src", Target: "tank/target", Match: "@base",
		SourceLast: "@new", TargetLast: "@other", Flags: opt.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/target tank/target_base\nzfs send -P -L -c -e -I tank/src@base tank/src@new\nzfs recv -v tank/target\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestRotateRejectsNoNewSourceSnapshot(t *testing.T) {
	_, err := Plan(Request{Source: "s", Target: "t", Match: "@base", SourceLast: "@base", TargetLast: "@other"})
	if err == nil {
		t.Fatal("expected up-to-date source error")
	}
}
