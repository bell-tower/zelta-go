package backup

import (
	"testing"
	"time"
)

func TestParseSnapMode(t *testing.T) {
	cases := []struct {
		in   string
		want SnapMode
	}{
		{"", SnapIfNeeded},
		{"IF_NEEDED", SnapIfNeeded},
		{"0", SnapNever},
		{"never", SnapNever},
		{"ALWAYS", SnapAlways},
		{"1", SnapAlways},
		{"bogus", SnapIfNeeded},
	}
	for _, tc := range cases {
		if got := ParseSnapMode(tc.in); got != tc.want {
			t.Fatalf("ParseSnapMode(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseSyncDirection(t *testing.T) {
	cases := []struct {
		in   string
		want SyncDirection
		pipe string
	}{
		{"", DirectionPull, "PULL"},
		{"pull", DirectionPull, "PULL"},
		{"PUSH", DirectionPush, "PUSH"},
		{"0", DirectionProxy, ""},
		{"no", DirectionProxy, ""},
		{"proxy", DirectionProxy, ""},
	}
	for _, tc := range cases {
		got := ParseSyncDirection(tc.in)
		if got != tc.want {
			t.Fatalf("ParseSyncDirection(%q)=%q want %q", tc.in, got, tc.want)
		}
		if p := got.pipeArg(); p != tc.pipe {
			t.Fatalf("pipeArg(%q)=%q want %q", tc.in, p, tc.pipe)
		}
	}
	if SyncDirection("").Normalize() != DirectionPull {
		t.Fatal("zero SyncDirection should normalize to PULL")
	}
}

func TestParseSnapTime(t *testing.T) {
	d, err := ParseSnapTime("1h")
	if err != nil || d != time.Hour {
		t.Fatalf("1h: %v %v", d, err)
	}
	d, err = ParseSnapTime("90")
	if err != nil || d != 90*time.Second {
		t.Fatalf("90: %v %v", d, err)
	}
	d, err = ParseSnapTime("")
	if err != nil || d != 0 {
		t.Fatalf("empty: %v %v", d, err)
	}
	if _, err := ParseSnapTime("bad"); err == nil {
		t.Fatal("want error for bad")
	}
}

func TestParseSnapSize(t *testing.T) {
	n, err := ParseSnapSize("200")
	if err != nil || n != 200 {
		t.Fatalf("200: %v %v", n, err)
	}
	n, err = ParseSnapSize("")
	if err != nil || n != 0 {
		t.Fatalf("empty: %v %v", n, err)
	}
	if _, err := ParseSnapSize("x"); err == nil {
		t.Fatal("want error")
	}
}
