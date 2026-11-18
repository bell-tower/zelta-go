package main

import (
	"reflect"
	"strings"
	"testing"

	"zelta.space/zelta"
)

func vs(s string) []verdict {
	m := map[rune]verdict{'d': doomed, 'k': kept, 'g': guarded}
	var out []verdict
	for _, c := range s {
		out = append(out, m[c])
	}
	return out
}

func TestMix(t *testing.T) {
	for _, tc := range []struct {
		run  string
		want cell
	}{
		{"dddd", cell{doomed, doomed, 8}},
		{"ddkk", cell{doomed, kept, 4}},
		{"dkkk", cell{doomed, kept, 2}},
		// The older verdict fills the left of the cell.
		{"kkkd", cell{kept, doomed, 6}},
		// Only the two most common verdicts show.
		{"dddkkg", cell{doomed, kept, 5}},
		// A lone snapshot never rounds away.
		{"dddddddddk", cell{doomed, kept, 7}},
	} {
		if got := mix(vs(tc.run)); got != tc.want {
			t.Errorf("mix(%s) = %+v, want %+v", tc.run, got, tc.want)
		}
	}
}

func TestCells(t *testing.T) {
	v := vs(strings.Repeat("d", 1122))
	if got := len(cells(v, 1000)); got != 1000 {
		t.Errorf("1122 snapshots in 1000 cells: got %d", got)
	}
	if got := len(cells(vs("ddk"), 1000)); got != 3 {
		t.Errorf("3 snapshots: got %d cells", got)
	}
	got := cells(vs("ddddkkkkgggg"), 3)
	want := []cell{{doomed, doomed, 8}, {kept, kept, 8}, {guarded, guarded, 8}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("cells = %+v", got)
	}
}

func TestBlockCapsAt1000(t *testing.T) {
	r := row{name: "pool/ds", v: vs(strings.Repeat("dk", 700))}
	lines, per := block(style{}, r, 100, 40)
	n := 0
	for _, l := range lines {
		n += len([]rune(l))
	}
	if n != 1000 || per != 1.4 {
		t.Errorf("block: %d cells, %v per cell", n, per)
	}
	lines, per = block(style{}, row{v: vs("dk")}, 100, 40)
	if len(lines) != 1 || per != 1 || len([]rune(lines[0])) != 6 {
		t.Errorf("small block widens: %q %v", lines, per)
	}
}

func TestRows(t *testing.T) {
	p := &zelta.PrunePlan{Datasets: []zelta.PruneDataset{{
		Name: "pool/ds", Destroy: []string{"@a"}, Keep: []string{"@b"},
		Snapshots: []zelta.PruneSnapshot{{Name: "@a", Used: 5}, {Name: "@b"}, {Name: "@c"}},
	}, {Name: "pool/ds/sub", Suffix: "/sub"}}}
	got, empty := rows(p)
	if len(got) != 1 || got[0].name != "pool/ds" || !reflect.DeepEqual(got[0].v, vs("dkg")) || got[0].doomed != 1 || got[0].reclaim != 5 || empty != 1 {
		t.Errorf("rows = %+v, %d empty", got, empty)
	}
}

func TestCommand(t *testing.T) {
	fs := newFields("bk:pool/ds")
	fs[fGrid].val = "30x1 day, 1 year"
	fs[fGuard].val = "unsynced"
	fs[fExclude].val = "/tmp, @hourly*"
	got := command(fs, "pool/ds", "bk:pool/ds")
	want := `zprune --match-endpoint=bk:pool/ds --prune-grid='30x1 day, 1 year' --prune-guard=unsynced --exclude=/tmp --exclude='@hourly*' pool/ds`
	if got != want {
		t.Errorf("command:\n got %s\nwant %s", got, want)
	}
	if got := command(newFields(""), "pool/ds", ""); got != "zprune --no-prune-guard pool/ds" {
		t.Errorf("unguarded command: %s", got)
	}
}

func TestOptions(t *testing.T) {
	fs := newFields("")
	fs[fNum].val, fs[fDepth].val = " 7 ", "2"
	o := options(fs, "")
	want := map[string]string{"PRUNE_NUM": "7", "DEPTH": "2"}
	if !reflect.DeepEqual(o.Settings, want) || o.GuardMode != zelta.GuardNone {
		t.Errorf("options = %+v", o)
	}
	if o := options(newFields("bk:pool"), "bk:pool"); o.GuardMode != zelta.GuardDefault || o.Guard != "bk:pool" {
		t.Errorf("guarded options = %+v", o)
	}
}

func TestParseKeys(t *testing.T) {
	got := parseKeys([]byte("7d\x1b[A\x1b[Z\t\r\x7f\x15\x1b[1;5C\x1b"))
	want := []string{"7", "d", kUp, kBackTab, kTab, kEnter, kBack, kClear, kEsc}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("keys = %q", got)
	}
}

func TestSize(t *testing.T) {
	for b, want := range map[int64]string{512: "512B", 1536: "1.50K", 1023 * 1024: "1023K", 68300 << 20: "66.7G"} {
		if got := size(b); got != want {
			t.Errorf("size(%d) = %s, want %s", b, got, want)
		}
	}
}
