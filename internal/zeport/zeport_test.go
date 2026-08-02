package zeport

import (
	"reflect"
	"strconv"
	"testing"
	"time"
)

func TestStatusOf(t *testing.T) {
	cases := []struct {
		ok, old int
		want    Status
	}{
		{0, 0, StatusNone},
		{0, 3, StatusAll},
		{2, 1, StatusSome},
		{4, 0, StatusOK},
	}
	for _, c := range cases {
		if got := StatusOf(c.ok, c.old); got != c.want {
			t.Errorf("StatusOf(%d,%d)=%q want %q", c.ok, c.old, got, c.want)
		}
	}
}

func TestClassify(t *testing.T) {
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC).Unix()
	tooOld := now - int64(MaxAge/time.Second)
	fresh := strconv.FormatInt(now-3600, 10)
	stale := strconv.FormatInt(tooOld-10, 10)

	ep := Endpoint{ID: "tank/backups", Dataset: "tank/backups", Leaf: "backups", Host: "vault"}
	rows := []Row{
		{Name: "tank/backups", Changed: "-", UsedBySnapshots: "0"},
		{Name: "tank/backups/a", Changed: fresh, UsedBySnapshots: "1M"},
		{Name: "tank/backups/b", Changed: stale, UsedBySnapshots: "2M"},
		{Name: "tank/backups/empty", Changed: stale, UsedBySnapshots: "0"},
		{Name: "tank/backups/nosnapinfo", Changed: "-", UsedBySnapshots: "0"},
	}
	has := func(ds string) bool {
		return ds != "tank/backups/empty"
	}
	r := Classify(ep, tooOld, rows, has)
	if r.Status != StatusSome {
		t.Fatalf("status=%q want some", r.Status)
	}
	if r.OKCount != 2 { // a fresh + empty (no snaps)
		t.Fatalf("ok=%d want 2", r.OKCount)
	}
	if !reflect.DeepEqual(r.Old, []string{"b"}) {
		t.Fatalf("old=%v want [b]", r.Old)
	}
}

func TestClassifyAllStale(t *testing.T) {
	tooOld := int64(1_000_000)
	ep := Endpoint{ID: "p/x", Dataset: "p/x", Leaf: "x"}
	r := Classify(ep, tooOld, []Row{
		{Name: "p/x/a", Changed: "1", UsedBySnapshots: "1"},
	}, func(string) bool { return true })
	if r.Status != StatusAll || r.OldCount != 1 {
		t.Fatalf("got status=%s old=%d", r.Status, r.OldCount)
	}
}

func TestClassifyNone(t *testing.T) {
	ep := Endpoint{ID: "p/x", Dataset: "p/x", Leaf: "x"}
	r := Classify(ep, 0, []Row{
		{Name: "p/x", Changed: "-", UsedBySnapshots: "0"},
	}, nil)
	if r.Status != StatusNone {
		t.Fatalf("status=%q", r.Status)
	}
}

func TestMessageAndExpand(t *testing.T) {
	r := Result{
		Endpoint: Endpoint{ID: "host:tank/b", Host: "host", Dataset: "tank/b"},
		Status:   StatusSome,
		Old:      []string{"a", "c"},
		OldCount: 2,
		OKCount:  1,
	}
	msg := Message(r, nil)
	want := "host:tank/b some snapshots are out of date: a c"
	if msg != want {
		t.Fatalf("default msg=%q want %q", msg, want)
	}

	env := map[string]string{
		"REPORT_MESSAGE_SOME": "ALERT {oldcount}/{okcount} on {dataset} ({status}): {dslist}",
		"REPORT_COMMAND_SOME": "echo {message}",
	}
	get := func(k string) string { return env[k] }
	msg = Message(r, get)
	if msg != "ALERT 2/1 on tank/b (some): a c" {
		t.Fatalf("custom msg=%q", msg)
	}
	cmd := Command(r, msg, get)
	if cmd != "echo ALERT 2/1 on tank/b (some): a c" {
		t.Fatalf("cmd=%q", cmd)
	}
}

func TestParseListLines(t *testing.T) {
	rows := ParseListLines([]string{
		"tank/a\t1700000000\t0",
		"tank/b\t-\t1K",
		"",
	})
	if len(rows) != 2 || rows[0].Name != "tank/a" || rows[1].Changed != "-" {
		t.Fatalf("%+v", rows)
	}
}

func TestListArgv(t *testing.T) {
	a := ListArgv("tank/backups")
	if a[len(a)-1] != "tank/backups" || a[3] != "filesystem,volume" {
		t.Fatalf("%v", a)
	}
}
