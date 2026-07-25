package match

import (
	"context"
	"strings"
	"testing"

	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

func TestParseBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"", 0},
		{"-", 0},
		{"100", 100},
		{"500K", 500 * 1024},
		{"1M", 1024 * 1024},
		{"1.5K", 1536},
	}
	for _, tc := range cases {
		if got := parseBytes(tc.in); got != tc.want {
			t.Errorf("parseBytes(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestResolveListProps(t *testing.T) {
	full := strings.Join(DefaultListProps, ",")
	min := strings.Join(MinimalListProps, ",")
	got := strings.Join(resolveListProps(Request{}, DefaultCols), ",")
	if got != full {
		t.Fatalf("default props=%s", got)
	}
	got = strings.Join(resolveListProps(Request{NoWritten: true}, DefaultCols), ",")
	if got != min {
		t.Fatalf("nowritten props=%s", got)
	}
	// -p without written cols skips slow props
	got = strings.Join(resolveListProps(Request{Parsable: true}, DefaultCols), ",")
	if got != min {
		t.Fatalf("parsable default cols props=%s want min", got)
	}
	got = strings.Join(resolveListProps(Request{Parsable: true}, []string{"ds_suffix", "xfer_size"}), ",")
	if got != full {
		t.Fatalf("parsable xfer_size props=%s want full", got)
	}
}

func TestFormatListTimes(t *testing.T) {
	s := formatListTimes(0.33, 0.60)
	if !strings.Contains(s, "SOURCE_LIST_TIME:\t0.33\n") {
		t.Fatalf("src: %q", s)
	}
	if !strings.Contains(s, "TARGET_LIST_TIME:\t0.6\n") {
		t.Fatalf("tgt: %q", s)
	}
	if formatListTimes(0, 0) != "" {
		t.Fatal("zero times should be empty")
	}
}

func TestCompareFake(t *testing.T) {
	fake := &zfs.Fake{Lists: map[string]string{
		"tank/src": "tank/src\tg1\t0\t1\t1M\ntank/src@a\tg2\t0\t2\t1K\n",
		"tank/tgt": "tank/tgt\tg1\t0\t1\t1M\ntank/tgt@a\tg2\t0\t2\t1K\n",
	}}
	res, err := Compare(context.Background(), fake, Request{
		Source:    "tank/src",
		Target:    "tank/tgt",
		Scripting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.SrcRows) != 2 || len(res.TgtRows) != 2 {
		t.Fatalf("rows src=%d tgt=%d", len(res.SrcRows), len(res.TgtRows))
	}
	if len(res.Pairs) != 1 {
		t.Fatalf("pairs=%d", len(res.Pairs))
	}
	if res.Pairs[0].Info != "up-to-date" {
		t.Fatalf("info=%q", res.Pairs[0].Info)
	}
	want := "\t@a\t@a\t@a\tup-to-date\n"
	if res.Output != want {
		t.Fatalf("output=%q want=%q", res.Output, want)
	}
}

func TestCompareBasicOracle(t *testing.T) {
	src := strings.Join([]string{
		"tank/src\t100\t0\t1000\t1M",
		"tank/src@b\t102\t50\t2000\t500K",
		"tank/src@a\t101\t100\t1500\t500K",
		"tank/src/child\t200\t0\t1100\t1M",
		"tank/src/child@a\t201\t200\t1600\t200K",
	}, "\n") + "\n"
	tgt := strings.Join([]string{
		"tank/tgt\t100\t0\t1000\t1M",
		"tank/tgt@b\t102\t50\t2000\t500K",
		"tank/tgt@a\t101\t100\t1500\t500K",
	}, "\n") + "\n"
	fake := &zfs.Fake{Lists: map[string]string{
		"tank/src": src,
		"tank/tgt": tgt,
	}}

	res, err := Compare(context.Background(), fake, Request{
		Source:    "tank/src",
		Target:    "tank/tgt",
		Scripting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := "\t@b\t@b\t@b\tup-to-date\n/child\t\t@a\t\tsyncable (full)\n"
	if res.Output != want {
		t.Fatalf("got:\n%s\nwant:\n%s", res.Output, want)
	}

	hum, err := Compare(context.Background(), fake, Request{
		Source: "tank/src",
		Target: "tank/tgt",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(hum.Output, "[src]") {
		t.Fatalf("human missing leaf: %q", hum.Output)
	}
	if !strings.Contains(hum.Output, "1 up-to-date, 1 syncable") {
		t.Fatalf("human summary: %q", hum.Output)
	}
}
