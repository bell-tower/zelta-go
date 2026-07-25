package prune

import "testing"

func TestParseDuration(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"30days", 30 * 86400},
		{"1month", 2592000},
		{"1mo", 2592000},
		{"2weeks", 2 * 604800},
		{"1year", 31557600},
		{"90", 90},
		{"1h", 3600},
		{"5mi", 300},
		{"1d", 86400},
	} {
		got, err := ParseDuration(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%s=%d want %d", tc.in, got, tc.want)
		}
	}
	if _, err := ParseDuration("1m"); err == nil {
		t.Fatal("1m must be ambiguous")
	}
	if _, err := ParseDuration("junk"); err == nil {
		t.Fatal("junk must fail")
	}
}

func TestParseSize(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int64
	}{
		{"1K", 1024},
		{"1KB", 1024},
		{"2M", 2 << 20},
		{"1G", 1 << 30},
		{"512", 512},
	} {
		got, err := ParseSize(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.in, err)
		}
		if got != tc.want {
			t.Fatalf("%s=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseGrid(t *testing.T) {
	g, err := parseGrid("30x1 day, 52x1 week, 1 year")
	if err != nil {
		t.Fatal(err)
	}
	if len(g) != 3 {
		t.Fatalf("terms=%d", len(g))
	}
	if g[0].Count != 30 || g[0].Interval != 86400 {
		t.Fatalf("term0=%+v", g[0])
	}
	if g[2].Count != -1 {
		t.Fatalf("tail=%+v", g[2])
	}
	if _, err := parseGrid("nx1day"); err == nil {
		t.Fatal("bad count must fail")
	}
}

func TestGridKeeps(t *testing.T) {
	terms, err := parseGrid("3x1day,1week")
	if err != nil {
		t.Fatal(err)
	}
	g := newGrid(terms)
	anchor := int64(100 * 86400)
	// first snap in each daily bucket kept
	if !g.keeps(anchor-86400, anchor) {
		t.Fatal("day1 should keep")
	}
	if g.keeps(anchor-86400, anchor) {
		t.Fatal("same bucket again → drop")
	}
	if !g.keeps(anchor-2*86400, anchor) {
		t.Fatal("day2 should keep")
	}
	// tail bucket: one per week
	if !g.keeps(anchor-10*86400, anchor) {
		t.Fatal("week bucket should keep")
	}
	if g.keeps(anchor-11*86400, anchor) {
		t.Fatal("same week → drop")
	}
}
