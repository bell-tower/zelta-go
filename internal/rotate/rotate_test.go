package rotate

import (
	"context"
	"testing"

	"git.belltower.it/djbell/zelta-go/internal/match"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

func TestDirectDivergencePlan(t *testing.T) {
	steps, err := Plan(Request{
		Source: "tank/src", Target: "tank/target", Match: "@base",
		SourceLast: "@new", TargetLast: "@other", Intermediate: true, Flags: opt.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/target tank/target_base\nzfs send -P -L -c -e -I tank/src@base tank/src@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s -o origin=tank/target_base@base tank/target\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestVerifiedSourceOriginPlanUsesOriginForSend(t *testing.T) {
	steps, err := Plan(Request{
		Source: "tank/clone", Target: "tank/target", SourceOrigin: "tank/original@base",
		OriginVerified: true, SourceLast: "@new", TargetLast: "@other",
		Intermediate: false, Flags: opt.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/target tank/target_base\nzfs send -P -L -c -e -i tank/original@base tank/clone@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s -o origin=tank/target_base@base tank/target\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestPlanTreeAddsFullSourceOnlyChild(t *testing.T) {
	steps, err := PlanTree(TreeRequest{
		Source: "tank/src", Target: "tank/target", Intermediate: true, Flags: opt.Default(),
		Pairs: []*match.Pair{
			{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other", SrcType: "filesystem"},
			{DSSuffix: "/child", SrcName: "tank/src/child", SrcLast: "@child", SrcType: "filesystem"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/target tank/target_base\nzfs send -P -L -c -e -I tank/src@base tank/src@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s -o origin=tank/target_base@base tank/target\nzfs send -P -L -c -e tank/src/child@child\nzfs recv -v -u -x mountpoint -o canmount=noauto -s tank/target/child\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestPlanTreeUsesVerifiedChildOrigin(t *testing.T) {
	steps, err := PlanTree(TreeRequest{
		Source: "tank/src", Target: "tank/target", Intermediate: true, Flags: opt.Default(),
		TargetRows: []zfs.ListRow{{Name: "tank/target@base"}, {Name: "tank/target/child@child-base"}},
		Pairs: []*match.Pair{
			{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other"},
			{DSSuffix: "/child", SrcName: "tank/src/child", SrcOrigin: "tank/original/child@child-base", SrcLast: "@child-new"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := Format(steps); got != "zfs rename -fp tank/target tank/target_base\nzfs send -P -L -c -e -I tank/src@base tank/src@new\nzfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s -o origin=tank/target_base@base tank/target\nzfs send -P -L -c -e -I tank/original/child@child-base tank/src/child@child-new\nzfs recv -v -u -x mountpoint -o canmount=noauto -s -o origin=tank/target_base/child@child-base tank/target/child\n" {
		t.Fatalf("format=%q", got)
	}
}

func TestExecuteRenamesThenPipesInPlanOrder(t *testing.T) {
	steps, err := Plan(Request{
		Source: "tank/src", Target: "tank/target", Match: "@base",
		SourceLast: "@new", TargetLast: "@other", Intermediate: true, Flags: opt.Default(),
	})
	if err != nil {
		t.Fatal(err)
	}
	fake := &zfs.Fake{}
	if err := Execute(context.Background(), fake, TreeRequest{Source: "tank/src", Target: "tank/target", SyncDirection: "PULL"}, steps); err != nil {
		t.Fatal(err)
	}
	if len(fake.Renames) != 1 || fake.Renames[0].OldDataset != "tank/target" || fake.Renames[0].NewDataset != "tank/target_base" {
		t.Fatalf("renames=%v", fake.Renames)
	}
	if len(fake.Pipes) != 1 || fake.Pipes[0].Direction != "PULL" {
		t.Fatalf("pipes=%v", fake.Pipes)
	}
}

func TestRotateRejectsNoNewSourceSnapshot(t *testing.T) {
	_, err := Plan(Request{Source: "s", Target: "t", Match: "@base", SourceLast: "@base", TargetLast: "@other"})
	if err == nil {
		t.Fatal("expected up-to-date source error")
	}
}

func TestRotateRejectsUnverifiedRollbackLineage(t *testing.T) {
	_, err := PlanTree(TreeRequest{
		Source: "tank/src", Target: "tank/target", TargetRows: []zfs.ListRow{{Name: "tank/target@other"}},
		Pairs: []*match.Pair{{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", SrcLast: "@new", TgtLast: "@other"}},
	})
	if err == nil {
		t.Fatal("expected unverified lineage error")
	}
}

func TestRotateRejectsPreservationCollision(t *testing.T) {
	_, err := PlanTree(TreeRequest{
		Source: "tank/src", Target: "tank/target", PreservationExists: true,
		Pairs: []*match.Pair{{DSSuffix: "", SrcName: "tank/src", TgtName: "tank/target", Match: "@base", SrcLast: "@new", TgtLast: "@other"}},
	})
	if err == nil {
		t.Fatal("expected preservation collision")
	}
}
