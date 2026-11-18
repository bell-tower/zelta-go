package prune

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"testing"

	"zelta.space/internal/app"
	"zelta.space/internal/execx"
)

func buildPlan(t *testing.T, tr *app.TestRuntime) (*Plan, *Dataset) {
	t.Helper()
	p, err := Build(context.Background(), tr.Runtime, []string{"p/ds"}, false)
	if err != nil {
		t.Fatal(err)
	}
	return p, p.Datasets[0]
}

// TestReclaimFromOldest covers runs the listing measures exactly: from the
// oldest snapshot (sizeFixture: A 10 in s0–s1, B 20 in s1–s3, C 5 in s2)
// and single snapshots.
func TestReclaimFromOldest(t *testing.T) {
	tr := app.NewTest("prune", map[string]string{"PRUNE_NUM": "0"})
	fakeZFS(tr, sizeFixture())
	p, d := buildPlan(t, tr)
	for _, c := range []struct {
		names []string
		want  int64
	}{
		{[]string{"@s0"}, 0},
		{[]string{"@s0", "@s1"}, 10},
		{[]string{"@s0", "@s1", "@s2"}, 15},
		{[]string{"@s2"}, 5},
		{[]string{"@s0", "@s2"}, 5}, // two runs: s0 frees 0, s2 its used
		{nil, 0},
	} {
		lo, hi := d.ReclaimBounds(c.names)
		got, err := d.Reclaim(context.Background(), tr.Runtime, p.Source, c.names)
		if lo != c.want || hi != c.want || got != c.want || err != nil {
			t.Errorf("%q: bounds %d..%d, reclaim %d, %v; want %d", c.names, lo, hi, got, err, c.want)
		}
	}
	if n := len(tr.Fake.CallsMatching("zfs get")); n != 0 {
		t.Errorf("exact runs read written@: %d calls", n)
	}
}

// TestReclaimAfterKept covers a run after a kept snapshot (the
// TestPruneSizeAfterKept blocks): s1–s2 frees Q+S = 10, which only
// written@s0 can tell.
func TestReclaimAfterKept(t *testing.T) {
	now := app.TestNow.Unix()
	lists := map[string][]row{"p/ds": {
		{name: "p/ds", guid: "1", creation: now, written: 0, referenced: 97},
		{name: "p/ds@s2", guid: "12", creation: now - 86400, written: 7, referenced: 101},
		{name: "p/ds@s1", guid: "11", creation: now - 2*86400, written: 10, referenced: 110, used: 6},
		{name: "p/ds@s0", guid: "10", creation: now - 3*86400, written: 100, referenced: 100, clones: "p/clone"},
	}}
	tr := app.NewTest("prune", map[string]string{"PRUNE_NUM": "0"})
	fakeZFS(tr, lists)
	getRe := regexp.MustCompile(`^zfs get -Hpo name,value written@s0 (.*)$`)
	var gets []string
	fail := false
	tr.Fake.OnFunc(getRe.MatchString, func(s string) execx.Result {
		gets = append(gets, s)
		if fail {
			return execx.Fail(1)
		}
		return execx.Out("p/ds\t7")
	})
	p, d := buildPlan(t, tr)
	names := []string{"@s1", "@s2"}
	if lo, hi := d.ReclaimBounds(names); lo != 6 || hi != 17 {
		t.Errorf("bounds = %d..%d, want 6..17", lo, hi)
	}
	got, err := d.Reclaim(context.Background(), tr.Runtime, p.Source, names)
	if got != 10 || err != nil || !slices.Equal(gets, []string{"zfs get -Hpo name,value written@s0 'p/ds'"}) {
		t.Errorf("reclaim = %d, %v, gets %q; want 10 with one written@s0", got, err, gets)
	}

	fail = true
	if _, err := d.Reclaim(context.Background(), tr.Runtime, p.Source, names); err == nil || !strings.Contains(err.Error(), "written@s0") {
		t.Errorf("failed zfs get: %v", err)
	}
}
