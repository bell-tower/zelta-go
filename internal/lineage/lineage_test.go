package lineage

import (
	"reflect"
	"testing"

	"git.belltower.it/djbell/zelta-go/internal/zfs"
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

func TestClonePlanSelectsLatestSnapshotPerDataset(t *testing.T) {
	rows := []zfs.ListRow{
		{Name: "tank/src/child@new", Props: map[string]string{"type": "snapshot"}},
		{Name: "tank/src@new", Props: map[string]string{"type": "snapshot"}},
		{Name: "tank/src/child@old", Props: map[string]string{"type": "snapshot"}},
		{Name: "tank/src@old", Props: map[string]string{"type": "snapshot"}},
	}
	steps, err := ClonePlan(CloneRequest{Source: "tank/src", Target: "tank/clone"}, rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs clone -p -o readonly=off tank/src/child@new tank/clone/child\nzfs clone -p -o readonly=off tank/src@new tank/clone\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestClonePlanRejectsExistingTargetAndHonorsDepth(t *testing.T) {
	rows := []zfs.ListRow{
		{Name: "tank/src@new", Props: map[string]string{"type": "snapshot"}},
		{Name: "tank/src/child@new", Props: map[string]string{"type": "snapshot"}},
	}
	if _, err := ClonePlan(CloneRequest{Source: "tank/src", Target: "tank/clone"}, rows, true); err == nil {
		t.Fatal("expected existing target error")
	}
	steps, err := ClonePlan(CloneRequest{Source: "tank/src", Target: "tank/clone", Depth: 1}, rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 || steps[0].Argv[len(steps[0].Argv)-1] != "tank/clone" {
		t.Fatalf("steps=%v", steps)
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
