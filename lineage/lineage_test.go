package lineage

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"git.belltower.it/djbell/zelta-go/zfs"
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
	if got := Format(steps); got != "zfs clone -p -o readonly=off tank/src@new tank/clone\nzfs clone -p -o readonly=off tank/src/child@new tank/clone/child\n" {
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
	steps, err := RevertPlan(RevertRequest{Endpoint: "tank/live@daily", AfterSnapshot: "@after"}, []zfs.ListRow{
		{Name: "tank/live@daily", Props: map[string]string{"type": "snapshot"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/live tank/live_daily\nzfs clone -p -o readonly=off tank/live_daily@daily tank/live\nzfs snapshot -r tank/live@after\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestRevertPlanSelectsRootAndChildSnapshots(t *testing.T) {
	rows := []zfs.ListRow{
		{Name: "tank/live/child@child-new", Props: map[string]string{"type": "snapshot"}},
		{Name: "tank/live@root-new", Props: map[string]string{"type": "snapshot"}},
	}
	steps, err := RevertPlan(RevertRequest{Endpoint: "tank/live", AfterSnapshot: "after"}, rows, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/live tank/live_root-new\nzfs clone -p -o readonly=off tank/live_root-new@root-new tank/live\nzfs clone -p -o readonly=off tank/live_root-new/child@child-new tank/live/child\nzfs snapshot -r tank/live@after\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestRevertPlanRejectsPreservationCollision(t *testing.T) {
	_, err := RevertPlan(RevertRequest{Endpoint: "tank/live@daily"}, []zfs.ListRow{
		{Name: "tank/live@daily", Props: map[string]string{"type": "snapshot"}},
	}, true)
	if err == nil {
		t.Fatal("expected preservation collision")
	}
}

func TestCloneRejectsRemoteMismatch(t *testing.T) {
	_, err := Clone(CloneRequest{Source: "a:tank/src@daily", Target: "b:tank/clone"})
	if err == nil {
		t.Fatal("expected host mismatch")
	}
}

func TestCloneTreatsLocalhostAsLocal(t *testing.T) {
	steps, err := Clone(CloneRequest{Source: "tank/src@daily", Target: "localhost:tank/clone"})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 1 {
		t.Fatalf("steps=%v", steps)
	}
}

func TestFormatRemoteWrapsLineageCommands(t *testing.T) {
	steps, err := Clone(CloneRequest{Source: "root@debian:apool/src@daily", Target: "root@debian:apool/clone"})
	if err != nil {
		t.Fatal(err)
	}
	out, err := FormatRemote(steps, "root@debian:apool/clone")
	if err != nil {
		t.Fatal(err)
	}
	want := "ssh -n root@debian 'zfs clone -p -o readonly=off apool/src@daily apool/clone'\n"
	if out != want {
		t.Fatalf("format=%q want %q", out, want)
	}
}

func TestApplyContinuesAfterChildCloneFailure(t *testing.T) {
	steps, err := RevertPlan(RevertRequest{Endpoint: "tank/live", AfterSnapshot: "after"}, []zfs.ListRow{
		{Name: "tank/live@root", Props: map[string]string{"type": "snapshot"}},
		{Name: "tank/live/child@child", Props: map[string]string{"type": "snapshot"}},
		{Name: "tank/live/other@other", Props: map[string]string{"type": "snapshot"}},
	}, false)
	if err != nil {
		t.Fatal(err)
	}
	fake := &failCloneFake{Fake: &zfs.Fake{}, failDataset: "tank/live/child"}
	result, err := Apply(context.Background(), fake, "tank/live", steps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Preserved || len(result.Completed) != 2 || result.Completed[0] != "" || result.Completed[1] != "/other" {
		t.Fatalf("progress=%+v", result)
	}
	if len(result.Failures) != 1 || result.Failures[0].DSSuffix != "/child" {
		t.Fatalf("failures=%+v", result.Failures)
	}
	if len(fake.Clones) != 2 {
		t.Fatalf("clones=%v", fake.Clones)
	}
}

type failCloneFake struct {
	*zfs.Fake
	failDataset string
}

func (f *failCloneFake) Clone(ctx context.Context, endpoint, sourceSnap, dataset string) error {
	if dataset == f.failDataset {
		return errors.New("injected clone failure")
	}
	return f.Fake.Clone(ctx, endpoint, sourceSnap, dataset)
}
