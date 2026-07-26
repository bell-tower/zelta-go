package prune

import (
	"context"
	"strings"
	"testing"

	"git.belltower.it/djbell/zelta-go/zfs"
)

// source list: newest-first snaps (zfs -S createtxg), 7 cols incl. clones.
const srcList = `apool/treetop@new	90	0	1000000	0	1000	-
apool/treetop@mid	80	0	900000	0	1000	-
apool/treetop@old	70	1000	800000	0	2000	-
apool/treetop	1	0	900000	1M	1000	-
apool/treetop/child@new	91	0	1000000	0	1000	-
apool/treetop/child@old	71	1000	800000	0	2000	-
apool/treetop/child	2	0	900000	1M	1000	-
`

func fakeExec(t *testing.T, tgt string) *zfs.Fake {
	t.Helper()
	return &zfs.Fake{Lists: map[string]string{
		"apool/treetop": strings.TrimSuffix(srcList, "\n") + "\n",
		"bpool/tgt":     tgt,
	}}
}

func TestPruneNoGuard(t *testing.T) {
	now := int64(1000000 + 100) // after newest snap
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source:    "apool/treetop",
		PruneNum:  1,
		PruneTime: "0",
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// num=1 keeps first snap after match (@new). @mid,@old prune → contiguous range.
	out := res.Output
	// oracle range: "@oldest%newest" (e.g. @snap1%zelta_…)
	if !strings.Contains(out, "apool/treetop@old%mid") {
		t.Fatalf("range:\n%s", out)
	}
	if strings.Contains(out, "@new") {
		t.Fatalf("@new must be kept:\n%s", out)
	}
	if !strings.Contains(out, "apool/treetop/child@old") {
		t.Fatalf("child:\n%s", out)
	}
}

func TestPruneTime(t *testing.T) {
	now := int64(950000)
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source:    "apool/treetop",
		PruneNum:  0,
		PruneTime: "100000s", // keep creation >= 850000 → @new (1e6) @mid (9e5); @old (8e5) pruned
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output
	if !strings.Contains(out, "apool/treetop@old") {
		t.Fatalf("old should prune:\n%s", out)
	}
	if strings.Contains(out, "@new") || strings.Contains(out, "@mid") {
		t.Fatalf("new/mid kept:\n%s", out)
	}
}

func TestPruneGuardUnsynced(t *testing.T) {
	// Oracle 1.2.0 quirk: unsynced prunes nothing, even with a full guard target.
	tgt := "bpool/tgt@new\t90\t0\t1000000\t0\t1000\n" +
		"bpool/tgt@mid\t80\t0\t900000\t0\t1000\n" +
		"bpool/tgt@old\t70\t1000\t800000\t0\t2000\n" +
		"bpool/tgt\t1\t0\t1000000\t1M\t1000\n"
	res, err := Run(context.Background(), fakeExec(t, tgt), Request{
		Source:      "apool/treetop",
		GuardTarget: "bpool/tgt",
		PruneGuard:  GuardUnsynced,
		PruneNum:    0,
		PruneTime:   "0",
		Now:         1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "" {
		t.Fatalf("unsynced keeps everything (oracle quirk):\n%s", res.Output)
	}
}

func TestPruneGuardLatest(t *testing.T) {
	// guard match at @mid (guid 80) → only @old analyzed (older than match).
	// num=1 keeps first snap after match going older (@old) → nothing pruned.
	tgt := "bpool/tgt@mid\t80\t0\t900000\t0\t1000\nbpool/tgt\t1\t0\t900000\t1M\t1000\n"
	res, err := Run(context.Background(), fakeExec(t, tgt), Request{
		Source:      "apool/treetop",
		GuardTarget: "bpool/tgt",
		PruneGuard:  GuardLatest,
		PruneNum:    1,
		PruneTime:   "0",
		Now:         1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output
	if strings.Contains(out, "treetop@old") {
		t.Fatalf("num=1 keeps @old:\n%s", out)
	}
	// num=0 → @old prunes
	res, err = Run(context.Background(), fakeExec(t, tgt), Request{
		Source:      "apool/treetop",
		GuardTarget: "bpool/tgt",
		PruneGuard:  GuardLatest,
		PruneNum:    0,
		PruneTime:   "0",
		Now:         1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "apool/treetop@old") {
		t.Fatalf("num=0 prunes @old:\n%s", res.Output)
	}
	if strings.Contains(res.Output, "treetop@mid") || strings.Contains(res.Output, "treetop@new") {
		t.Fatalf("newer than match never analyzed:\n%s", res.Output)
	}
}

func TestPruneNoRangesVisual(t *testing.T) {
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source:   "apool/treetop",
		PruneNum: 1, PruneTime: "0",
		Now:      1000100,
		NoRanges: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "%") {
		t.Fatalf("no-ranges:\n%s", res.Output)
	}
	// oldest first
	idxOld := strings.Index(res.Output, "treetop@old")
	idxMid := strings.Index(res.Output, "treetop@mid")
	if idxOld < 0 || idxMid < 0 || idxOld > idxMid {
		t.Fatalf("order:\n%s", res.Output)
	}

	res2, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source: "apool/treetop", PruneNum: 1, PruneTime: "0",
		Now: 1000100, Visual: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res2.Output, "❌") || !strings.Contains(res2.Output, "🔹") {
		t.Fatalf("visual:\n%s", res2.Output)
	}
}

func TestPruneDefaults(t *testing.T) {
	// all unset → num=30 time=30days: nothing pruned here (3 snaps < 30)
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source:   "apool/treetop",
		PruneNum: -1,
		Now:      1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Output != "" {
		t.Fatalf("defaults should keep all:\n%s", res.Output)
	}
}

func TestPruneFilteredSnapsKept(t *testing.T) {
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source: "apool/treetop", PruneNum: 0, PruneTime: "0",
		Now:     1000100,
		Exclude: []string{"@old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Output
	if strings.Contains(out, "treetop@old") {
		t.Fatalf("excluded snap must be kept:\n%s", out)
	}
	if !strings.Contains(out, "treetop@mid") {
		t.Fatalf("mid should prune:\n%s", out)
	}
}
