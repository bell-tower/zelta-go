package backup

import "testing"

func TestHumanBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0B"},
		{1, "1B"},
		{624, "624B"},
		{1023, "1023B"},
		{1024, "1K"},
		{2496, "2K"},
		{51 * 1024, "51K"},
		{1024 * 1024, "1M"},
		{2*1024*1024*1024 + 512*1024*1024, "2G"},
		{3 * 1024 * 1024 * 1024 * 1024, "3T"},
		{-624, "-624B"},
	}
	for _, c := range cases {
		if got := HumanBytes(c.in); got != c.want {
			t.Errorf("HumanBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestStreamParser(t *testing.T) {
	p := &streamParser{}
	lines := []string{
		"incremental\tgo\tapool/treetop@zelta_2026-06-23_13.34.19.5334\t624",
		"size\t2496",
		"receiving incremental stream of apool/treetop@zelta_2026-06-23_13.34.19.5334 into cpool/treetopzelta-go@zelta_2026-06-23_13.34.19.5334",
		"snap cpool/treetopzelta-go@zelta_2026-06-23_13.34.19.5334 already exists; ignoring",
		"received 0B stream in 0.01 seconds (0B/sec)",
		"size\t512",
		"received 312B stream in 0.04 seconds (6.93K/sec)",
		"cannot receive new stream: checksum mismatch",
	}
	for _, l := range lines {
		p.Line(l)
	}
	if p.bytes != 3008 {
		t.Errorf("bytes = %d, want 3008", p.bytes)
	}
	if p.streams != 2 {
		t.Errorf("streams = %d, want 2", p.streams)
	}
	if p.secs < 0.049 || p.secs > 0.051 {
		t.Errorf("secs = %v, want ~0.05", p.secs)
	}
}

func TestStreamParserForwardsToOnLine(t *testing.T) {
	var got []string
	p := &streamParser{onLine: func(line string) { got = append(got, line) }}
	p.Line("size\t10")
	p.Line("received 1B stream in 0.01 seconds (1B/sec)")
	if len(got) != 2 || got[0] != "size\t10" {
		t.Errorf("onLine not forwarded: %v", got)
	}
}
