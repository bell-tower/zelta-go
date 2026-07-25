package lineage

import (
	"reflect"
	"testing"
)

func TestClonePlan(t *testing.T) {
	steps, err := Clone(CloneRequest{Source: "tank/src@daily", Target: "tank/clone"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"zfs", "clone", "-p", "-o", "readonly=off", "tank/src@daily", "tank/clone"}
	if !reflect.DeepEqual(steps[0].Argv, want) {
		t.Fatalf("argv=%v want %v", steps[0].Argv, want)
	}
}

func TestRevertPreservesBeforeClone(t *testing.T) {
	steps, err := Revert(RevertRequest{Endpoint: "tank/live@daily"})
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/live tank/live_daily\nzfs clone -p -o readonly=off tank/live@daily tank/live\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestCloneRejectsRemoteMismatch(t *testing.T) {
	_, err := Clone(CloneRequest{Source: "a:tank/src@daily", Target: "b:tank/clone"})
	if err == nil {
		t.Fatal("expected host mismatch")
	}
}
