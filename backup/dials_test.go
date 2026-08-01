package backup

import (
	"testing"
	"time"
)

func TestParseSnapMode(t *testing.T) {
	cases := []struct {
		in   string
		want SnapMode
		err  bool
	}{
		{"", SnapIfNeeded, false},
		{"IF_NEEDED", SnapIfNeeded, false},
		{"0", SnapNever, false},
		{"never", SnapNever, false},
		{"ALWAYS", SnapAlways, false},
		{"1", SnapAlways, false},
		{"SKIP", SnapNever, false},
		{"bogus", "", true},
	}
	for _, tc := range cases {
		got, err := ParseSnapMode(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("ParseSnapMode(%q): want error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseSnapMode(%q): unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseSnapMode(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestParseSyncDirection(t *testing.T) {
	cases := []struct {
		in   string
		want SyncDirection
		pipe string
		err  bool
	}{
		{"", DirectionPull, "PULL", false},
		{"pull", DirectionPull, "PULL", false},
		{"PUSH", DirectionPush, "PUSH", false},
		{"0", DirectionProxy, "", false},
		{"no", DirectionProxy, "", false},
		{"proxy", DirectionProxy, "", false},
		{"bogus", "", "", true},
	}
	for _, tc := range cases {
		got, err := ParseSyncDirection(tc.in)
		if tc.err {
			if err == nil {
				t.Fatalf("ParseSyncDirection(%q): want error, got %q", tc.in, got)
			}
			continue
		}
		if err != nil {
			t.Fatalf("ParseSyncDirection(%q): unexpected error %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("ParseSyncDirection(%q)=%q want %q", tc.in, got, tc.want)
		}
		if p := got.PipeArg(); p != tc.pipe {
			t.Fatalf("PipeArg(%q)=%q want %q", tc.in, p, tc.pipe)
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
