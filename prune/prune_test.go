package prune

import (
	"context"
	"github.com/bell-tower/zelta-go/endpoint"
	"strings"
	"testing"
	"time"

	"github.com/bell-tower/zelta-go/zfs"
)

func pt(d time.Duration) *time.Duration { return &d }

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
		Source:    endpoint.MustParse("apool/treetop"),
		PruneNum:  1,
		PruneTime: pt(0),
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	// num=1 keeps first snap after match (@new). @mid,@old prune → contiguous range.
	out := res.Format()
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
		Source:    endpoint.MustParse("apool/treetop"),
		PruneNum:  0,
		PruneTime: pt(100000 * time.Second), // keep creation >= 850000 → @new (1e6) @mid (9e5); @old (8e5) pruned
		Now:       now,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Format()
	if !strings.Contains(out, "apool/treetop@old") {
		t.Fatalf("old should prune:\n%s", out)
	}
	if strings.Contains(out, "@new") || strings.Contains(out, "@mid") {
		t.Fatalf("new/mid kept:\n%s", out)
	}
}

func TestPruneGuardUnsynced(t *testing.T) {
	// Snapshots present on the guard by both GUID and name are eligible.
	tgt := "bpool/tgt@new\t90\t0\t1000000\t0\t1000\n" +
		"bpool/tgt@mid\t80\t0\t900000\t0\t1000\n" +
		"bpool/tgt@old\t70\t1000\t800000\t0\t2000\n" +
		"bpool/tgt\t1\t0\t1000000\t1M\t1000\n"
	res, err := Run(context.Background(), fakeExec(t, tgt), Request{
		Source:      endpoint.MustParse("apool/treetop"),
		GuardTarget: endpoint.MustParse("bpool/tgt"),
		PruneGuard:  GuardUnsynced,
		PruneNum:    0,
		PruneTime:   pt(0),
		Now:         1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Format(), "apool/treetop@old%mid") {
		t.Fatalf("unsynced should prune guarded history:\n%s", res.Format())
	}
}

func TestPruneGuardLatest(t *testing.T) {
	// guard match at @mid (guid 80) → only @old analyzed (older than match).
	// num=1 keeps first snap after match going older (@old) → nothing pruned.
	tgt := "bpool/tgt@mid\t80\t0\t900000\t0\t1000\nbpool/tgt\t1\t0\t900000\t1M\t1000\n"
	res, err := Run(context.Background(), fakeExec(t, tgt), Request{
		Source:      endpoint.MustParse("apool/treetop"),
		GuardTarget: endpoint.MustParse("bpool/tgt"),
		PruneGuard:  GuardLatest,
		PruneNum:    1,
		PruneTime:   pt(0),
		Now:         1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Format()
	if strings.Contains(out, "treetop@old") {
		t.Fatalf("num=1 keeps @old:\n%s", out)
	}
	// num=0 → @old prunes
	res, err = Run(context.Background(), fakeExec(t, tgt), Request{
		Source:      endpoint.MustParse("apool/treetop"),
		GuardTarget: endpoint.MustParse("bpool/tgt"),
		PruneGuard:  GuardLatest,
		PruneNum:    0,
		PruneTime:   pt(0),
		Now:         1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Format(), "apool/treetop@old") {
		t.Fatalf("num=0 prunes @old:\n%s", res.Format())
	}
	if strings.Contains(res.Format(), "treetop@mid") || strings.Contains(res.Format(), "treetop@new") {
		t.Fatalf("newer than match never analyzed:\n%s", res.Format())
	}
}

func TestPruneNoRangesVisual(t *testing.T) {
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source:   endpoint.MustParse("apool/treetop"),
		PruneNum: 1, PruneTime: pt(0),
		Now:      1000100,
		NoRanges: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Format(), "%") {
		t.Fatalf("no-ranges:\n%s", res.Format())
	}
	// oldest first
	idxOld := strings.Index(res.Format(), "treetop@old")
	idxMid := strings.Index(res.Format(), "treetop@mid")
	if idxOld < 0 || idxMid < 0 || idxOld > idxMid {
		t.Fatalf("order:\n%s", res.Format())
	}

	res2, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source: endpoint.MustParse("apool/treetop"), PruneNum: 1, PruneTime: pt(0),
		Now: 1000100, Visual: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res2.Format(), "❌") || !strings.Contains(res2.Format(), "🔹") {
		t.Fatalf("visual:\n%s", res2.Format())
	}
}

func TestPruneDefaults(t *testing.T) {
	// all unset → num=30 time=30days: nothing pruned here (3 snaps < 30)
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source:   endpoint.MustParse("apool/treetop"),
		PruneNum: -1,
		Now:      1000100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Format() != "" {
		t.Fatalf("defaults should keep all:\n%s", res.Format())
	}
}

func TestPruneFilteredSnapsKept(t *testing.T) {
	res, err := Run(context.Background(), fakeExec(t, ""), Request{
		Source: endpoint.MustParse("apool/treetop"), PruneNum: 0, PruneTime: pt(0),
		Now:     1000100,
		Exclude: []string{"@old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	out := res.Format()
	if strings.Contains(out, "treetop@old") {
		t.Fatalf("excluded snap must be kept:\n%s", out)
	}
	if !strings.Contains(out, "treetop@mid") {
		t.Fatalf("mid should prune:\n%s", out)
	}
}
