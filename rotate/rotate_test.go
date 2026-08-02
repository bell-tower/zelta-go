package rotate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/backup"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/match"
	"github.com/bell-tower/zelta-go/zfs"
)

func TestDirectDivergencePlan(t *testing.T) {
	steps, err := Plan(Request{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Match: "@base",
		SourceLast: "@new", TargetLast: "@other", Intermediate: true, Flags: backup.DefaultSendRecv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Preserve suffix is TGT latest (@other), not match (@base) — Awk rename_dataset.
	if got := formatSteps(steps); got != "zfs rename -fp tank/target tank/target_other\nzfs send -P -L -c -e tank/src@base\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s tank/target\nzfs send -P -L -c -e -I tank/src@base tank/src@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s tank/target\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestVerifiedSourceOriginPlanUsesOriginForSend(t *testing.T) {
	steps, err := Plan(Request{
		Source: endpoint.MustParse("tank/clone"), Target: endpoint.MustParse("tank/target"), SourceOrigin: "tank/original@base",
		OriginVerified: true, SourceLast: "@new", TargetLast: "@other",
		Intermediate: false, Flags: backup.DefaultSendRecv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Preserve = TGT latest (@other); recv -o origin still uses lineage snap @base on that preserve.
	if got := formatSteps(steps); got != "zfs rename -fp tank/target tank/target_other\nzfs send -P -L -c -e -i tank/original@base tank/clone@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s -o origin=tank/target_other@base tank/target\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestCloneOriginRecvKeepsSpaceInDatasetName(t *testing.T) {
	// Origin values may contain spaces (dataset names with spaces, like the
	// "space name" fixtures). The value must stay a single argv element after
	// -o, so zfs recv never sees "too many arguments" or a mangled property.
	steps, err := Plan(Request{
		Source: endpoint.MustParse("tank/clone space"), Target: endpoint.MustParse("tank/target space"),
		SourceOrigin: "tank/original space@base", OriginVerified: true,
		SourceLast: "@new", TargetLast: "@other", Intermediate: false, Flags: backup.DefaultSendRecv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	// Preserve suffix is TGT latest (@other) → preserved root "tank/target space_other".
	var recv []string
	for _, step := range steps {
		if step.Kind == "recv" {
			recv = step.Argv
		}
	}
	if recv == nil {
		t.Fatal("no recv step")
	}
	var originValues []string
	for i, arg := range recv {
		if arg == "-o" && i+1 < len(recv) && strings.HasPrefix(recv[i+1], "origin=") {
			originValues = append(originValues, recv[i+1])
		}
	}
	if len(originValues) != 1 || originValues[0] != "origin=tank/target space_other@base" {
		t.Fatalf("origin value not a single argv element: %v", recv)
	}
	// Last element must be the target dataset, intact.
	if last := recv[len(recv)-1]; last != "tank/target space" {
		t.Fatalf("recv target argv = %q; recv=%v", last, recv)
	}
}

func TestPlanUsesNextSourceSnapshotForOneRotateStep(t *testing.T) {
	steps, err := Plan(Request{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Match: "@base", SourceNext: "@next",
		SourceLast: "@latest", TargetLast: "@other", Intermediate: true, Flags: backup.DefaultSendRecv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := steps[3].Argv[len(steps[3].Argv)-1]; got != "tank/src@next" {
		t.Fatalf("send end=%q", got)
	}
}

func TestPlanAllowsTargetAtTheCommonMatch(t *testing.T) {
	steps, err := Plan(Request{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Match: "@base",
		SourceLast: "@new", TargetLast: "@base", Intermediate: true, Flags: backup.DefaultSendRecv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(steps) != 5 {
		t.Fatalf("steps=%v", steps)
	}
}

func TestCommandsShellLines(t *testing.T) {
	steps := []Step{
		{Kind: "rename", Argv: []string{"zfs", "rename", "-fp", "bpool/target", "bpool/target_base"}},
		{Kind: "send", Argv: []string{"zfs", "send", "-I", "apool/src@base", "apool/src@new"}},
		{Kind: "recv", Argv: []string{"zfs", "recv", "-s", "bpool/target"}},
	}
	src := endpoint.MustParse("root@debian:apool/src")
	tgt := endpoint.MustParse("root@vault:bpool/target")
	cmds, err := Commands(steps, src, tgt, "PULL")
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 2 {
		t.Fatalf("cmds=%v", cmds)
	}
	line0, err := cmds[0].ShellLine()
	if err != nil {
		t.Fatal(err)
	}
	line1, err := cmds[1].ShellLine()
	if err != nil {
		t.Fatal(err)
	}
	want0 := "ssh -n root@vault 'zfs rename -fp bpool/target bpool/target_base'"
	want1 := "ssh -n root@vault \"ssh -n root@debian zfs send -I apool/src@base apool/src@new | zfs recv -s bpool/target\""
	if line0 != want0 {
		t.Fatalf("line0=%q want %q", line0, want0)
	}
	if line1 != want1 {
		t.Fatalf("line1=%q want %q", line1, want1)
	}
}

func TestPlanTreeAddsFullSourceOnlyChild(t *testing.T) {
	steps, err := PlanTree(TreeRequest{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Intermediate: true, Flags: backup.DefaultSendRecv(),
		Pairs: []*match.Pair{
			{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other", SrcType: "filesystem"},
			{DSSuffix: "/child", SrcName: "tank/src/child", SrcLast: "@child", SrcType: "filesystem"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := formatSteps(steps); got != "zfs rename -fp tank/target tank/target_other\nzfs send -P -L -c -e tank/src@base\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s tank/target\nzfs send -P -L -c -e -I tank/src@base tank/src@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s tank/target\nzfs send -P -L -c -e tank/src/child@child\nzfs recv -v -u -x mountpoint -o canmount=noauto -s tank/target/child\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestPlanTreeUsesVerifiedChildOrigin(t *testing.T) {
	steps, err := PlanTree(TreeRequest{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Intermediate: true, Flags: backup.DefaultSendRecv(),
		TargetRows: []zfs.ListRow{{Name: "tank/target@base"}, {Name: "tank/target/child@child-base"}},
		Pairs: []*match.Pair{
			{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other"},
			{DSSuffix: "/child", SrcName: "tank/src/child", SrcOrigin: "tank/original/child@child-base", SrcLast: "@child-new"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := formatSteps(steps); got != "zfs rename -fp tank/target tank/target_other\nzfs send -P -L -c -e tank/src@base\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s tank/target\nzfs send -P -L -c -e -I tank/src@base tank/src@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s tank/target\nzfs send -P -L -c -e -I tank/original/child@child-base tank/src/child@child-new\nzfs recv -v -u -x mountpoint -o canmount=noauto -s -o origin=tank/target_other/child@child-base tank/target/child\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestExecuteRenamesThenPipesInPlanOrder(t *testing.T) {
	steps, err := Plan(Request{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Match: "@base",
		SourceLast: "@new", TargetLast: "@other", Intermediate: true, Flags: backup.DefaultSendRecv(),
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &zfs.Fake{}
	if err := Execute(context.Background(), fake, TreeRequest{Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), SyncDirection: backup.DirectionPull}, steps); err != nil {
		t.Fatal(err)
	}
	if len(fake.Renames) != 1 || fake.Renames[0].OldDataset != "tank/target" || fake.Renames[0].NewDataset != "tank/target_other" {
		t.Fatalf("renames=%v", fake.Renames)
	}
	if len(fake.Pipes) != 2 || fake.Pipes[0].Direction != "PULL" || fake.Pipes[1].Direction != "PULL" {
		t.Fatalf("pipes=%v", fake.Pipes)
	}
}

func TestRotateRejectsNoNewSourceSnapshot(t *testing.T) {
	_, err := Plan(Request{Source: endpoint.MustParse("s"), Target: endpoint.MustParse("t"), Match: "@base", SourceLast: "@base", TargetLast: "@other"})
	if err == nil {
		t.Fatal("expected up-to-date source error")
	}
}

func TestRotateRejectsUnverifiedRollbackLineage(t *testing.T) {
	_, err := PlanTree(TreeRequest{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), TargetRows: []zfs.ListRow{{Name: "tank/target@other"}},
		Pairs: []*match.Pair{{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", SrcLast: "@new", TgtLast: "@other"}},
	})
	if err == nil {
		t.Fatal("expected unverified lineage error")
	}
}

func TestRotateRejectsPreservationCollision(t *testing.T) {
	_, err := PlanTree(TreeRequest{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), PreservationExists: true,
		Pairs: []*match.Pair{{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other"}},
	})
	if err == nil {
		t.Fatal("expected preservation collision")
	}
}

func TestNeedsPreservationDetectsRemainingSourceDelta(t *testing.T) {
	if !NeedsPreservation(&match.Result{Pairs: []*match.Pair{{SrcName: "tank/src", SrcLast: "@new", Match: "@next"}}}) {
		t.Fatal("expected remaining delta")
	}
	if NeedsPreservation(&match.Result{Pairs: []*match.Pair{{SrcName: "tank/src", SrcLast: "@same", Match: "@same"}}}) {
		t.Fatal("did not expect remaining delta")
	}
}

func TestFullSendCountIgnoresVerifiedOriginPairs(t *testing.T) {
	rows := []zfs.ListRow{{Name: "tank/target@base"}, {Name: "tank/target/child@base"}}
	pairs := []*match.Pair{
		{DSSuffix: "", SrcName: "tank/clone", SrcLast: "@new", SrcOrigin: "tank/original@base"},
		{DSSuffix: "/child", SrcName: "tank/clone/child", SrcLast: "@new", SrcOrigin: "tank/original/child@base"},
		{DSSuffix: "/full", SrcName: "tank/clone/full", SrcLast: "@new"},
	}
	if got := FullSendCount(pairs, "tank/target", rows); got != 1 {
		t.Fatalf("FullSendCount=%d want 1", got)
	}
}

func TestStreamCountCountsSendSteps(t *testing.T) {
	if got := StreamCount([]Step{{Kind: "rename"}, {Kind: "send"}, {Kind: "recv"}, {Kind: "send"}, {Kind: "recv"}}); got != 2 {
		t.Fatalf("StreamCount=%d", got)
	}
}

func TestExecuteResultContinuesAfterChildFailure(t *testing.T) {
	steps, err := PlanTree(TreeRequest{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Intermediate: true, Flags: backup.DefaultSendRecv(),
		Pairs: []*match.Pair{
			{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other"},
			{DSSuffix: "/child", SrcName: "tank/src/child", TgtName: "tank/target/child", Match: "@base", SrcLast: "@child-new", TgtLast: "@child-other"},
			{DSSuffix: "/other", SrcName: "tank/src/other", TgtName: "tank/target/other", Match: "@base", SrcLast: "@other-new", TgtLast: "@other-old"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &failPipeFake{Fake: &zfs.Fake{}, failSuffix: "tank/src/child"}
	result, err := ExecuteResult(context.Background(), fake, TreeRequest{Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), SyncDirection: backup.DirectionPull}, steps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Preserved || len(result.Completed) != 2 || result.Completed[0] != "" || result.Completed[1] != "/other" {
		t.Fatalf("progress=%+v", result)
	}
	if len(result.Failures) != 1 || result.Failures[0].DSSuffix != "/child" {
		t.Fatalf("failures=%+v", result.Failures)
	}
	if len(fake.Pipes) != 5 {
		t.Fatalf("pipes=%v", fake.Pipes)
	}
}

func TestExecuteResultStopsAfterPreservationFailure(t *testing.T) {
	steps, err := PlanTree(TreeRequest{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target"), Intermediate: true, Flags: backup.DefaultSendRecv(),
		Pairs: []*match.Pair{{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &failRenameFake{Fake: &zfs.Fake{}}
	result, err := ExecuteResult(context.Background(), fake, TreeRequest{Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target")}, steps)
	if err != nil {
		t.Fatal(err)
	}
	if result.Preserved || len(result.Failures) != 1 || result.Failures[0].Kind != "rename" {
		t.Fatalf("result=%+v", result)
	}
	if len(fake.Pipes) != 0 {
		t.Fatalf("preservation failure must not send: %v", fake.Pipes)
	}
}

func TestExecuteResultReportsSnapshotFailureAfterPreservation(t *testing.T) {
	fake := &failSnapshotFake{Fake: &zfs.Fake{}}
	steps := []Step{
		{Kind: "rename", Argv: []string{"zfs", "rename", "-fp", "tank/target", "tank/target_base"}},
		{Kind: "snapshot", Argv: []string{"zfs", "snapshot", "-r", "tank/src@new"}},
	}
	result, err := ExecuteResult(context.Background(), fake, TreeRequest{Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/target")}, steps)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Preserved || len(result.Failures) != 1 || result.Failures[0].Kind != "snapshot" {
		t.Fatalf("result=%+v", result)
	}
	if len(fake.Pipes) != 0 {
		t.Fatalf("snapshot failure must not send: %v", fake.Pipes)
	}
}

type failPipeFake struct {
	*zfs.Fake
	failSuffix string
}

type failRenameFake struct{ *zfs.Fake }

func (f *failRenameFake) Rename(context.Context, string, string, string) error {
	return errors.New("injected rename failure")
}

type failSnapshotFake struct{ *zfs.Fake }

func (f *failSnapshotFake) Snapshot(context.Context, string, string, bool) error {
	return errors.New("injected snapshot failure")
}

func (f *failPipeFake) RunPipeDirection(ctx context.Context, leftEp string, leftArgv []string, rightEp string, rightArgv []string, direction string) error {
	if len(leftArgv) > 0 && leftArgv[len(leftArgv)-1] == f.failSuffix+"@child-new" {
		return errors.New("injected receive failure")
	}
	return f.Fake.RunPipeDirection(ctx, leftEp, leftArgv, rightEp, rightArgv, direction)
}

func formatSteps(steps []Step) string {
	var b strings.Builder
	for _, step := range steps {
		if len(step.Argv) == 0 {
			continue
		}
		b.WriteString(strings.Join(step.Argv, " "))
		b.WriteByte('\n')
	}
	return b.String()
}
