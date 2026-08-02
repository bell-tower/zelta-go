package match

import (
	"context"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/zfs"
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
	ctx := context.Background()
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": "tank/src\tg1\n"}}
	full := strings.Join(DefaultListProps, ",")
	min := strings.Join(MinimalListProps, ",")
	got, err := resolveListProps(ctx, fake, Request{Source: endpoint.MustParse("tank/src")}, DefaultCols)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != full {
		t.Fatalf("default props=%s", got)
	}
	got, err = resolveListProps(ctx, fake, Request{Source: endpoint.MustParse("tank/src"), NoWritten: true}, DefaultCols)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != min {
		t.Fatalf("nowritten props=%s", got)
	}
	// -p without written cols skips slow props
	got, err = resolveListProps(ctx, fake, Request{Source: endpoint.MustParse("tank/src"), Parsable: true}, DefaultCols)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != min {
		t.Fatalf("parsable default cols props=%s want min", got)
	}
	got, err = resolveListProps(ctx, fake, Request{Source: endpoint.MustParse("tank/src"), Parsable: true}, []string{"ds_suffix", "xfer_size"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != full {
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
		Source:    endpoint.MustParse("tank/src"),
		Target:    endpoint.MustParse("tank/tgt"),
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

func TestCompareCapturesEncryptionAndIVSet(t *testing.T) {
	// Snap list: name,guid,ivsetguid — encryption from DatasetContext.
	fake := &zfs.Fake{
		Lists: map[string]string{
			"tank/src": "tank/src\tg1\t-\ntank/src@base\tg2\tiv-1\n",
			"tank/tgt": "tank/tgt\tg1\t-\ntank/tgt@base\tg2\tiv-1\n",
		},
		Props: map[string]string{
			"tank/src": "tank/src\tencryption\taes-256-gcm\n",
			"tank/tgt": "tank/tgt\tencryption\taes-256-gcm\n",
		},
	}
	srcCtx, err := zfs.LoadDatasetContext(context.Background(), fake, "tank/src", "tank/src", 0)
	if err != nil {
		t.Fatal(err)
	}
	tgtCtx, err := zfs.LoadDatasetContext(context.Background(), fake, "tank/tgt", "tank/tgt", 0)
	if err != nil {
		t.Fatal(err)
	}
	res, err := Compare(context.Background(), fake, Request{
		Source: endpoint.MustParse("tank/src"), Target: endpoint.MustParse("tank/tgt"),
		Props:      []string{"name", "guid", "ivsetguid"},
		SrcContext: srcCtx,
		TgtContext: tgtCtx,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Pairs[0].MatchIVSet; got != "iv-1" {
		t.Fatalf("match ivset=%q", got)
	}
	if got := res.Pairs[0].SrcEncryption; got != "aes-256-gcm" {
		t.Fatalf("source encryption=%q", got)
	}
	if got := res.Pairs[0].TgtEncryption; got != "aes-256-gcm" {
		t.Fatalf("target encryption=%q", got)
	}
}

func TestCompareDefaultNoIVSetProbe(t *testing.T) {
	// Default match must not require encryption columns or fail on old hosts.
	fake := &zfs.Fake{Lists: map[string]string{
		"zroot":        "zroot\t100\t0\t1000\t1M\nzroot@a\t101\t0\t1500\t500K\n",
		"backup/zroot": "backup/zroot\t100\t0\t1000\t1M\nbackup/zroot@a\t101\t0\t1500\t500K\n",
	}}
	res, err := Compare(context.Background(), fake, Request{
		Source:    endpoint.MustParse("app2:zroot"),
		Target:    endpoint.MustParse("vault:backup/zroot"),
		Scripting: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Pairs) == 0 || res.Pairs[0].Info != "up-to-date" {
		t.Fatalf("pairs=%+v", res.Pairs)
	}
}

func TestSnapListPropsIVSetOnlyWhenFeatured(t *testing.T) {
	got := strings.Join(SnapListProps(zfs.Features{}, SnapListOpts{Written: true, IVSet: true}), ",")
	if got != "name,guid,written,creation,used" {
		t.Fatalf("no feature: %s", got)
	}
	got = strings.Join(SnapListProps(zfs.Features{IVSetGUID: true}, SnapListOpts{Written: true, IVSet: true}), ",")
	if got != "name,guid,written,creation,used,ivsetguid" {
		t.Fatalf("with feature: %s", got)
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
		Source:    endpoint.MustParse("tank/src"),
		Target:    endpoint.MustParse("tank/tgt"),
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
		Source: endpoint.MustParse("tank/src"),
		Target: endpoint.MustParse("tank/tgt"),
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

func TestCommands(t *testing.T) {
	req := Request{
		Source: endpoint.MustParse("root@debian:tank/src"),
		Target: endpoint.MustParse("tank/tgt"),
		Depth:  2,
	}
	lines, err := Commands(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 2 {
		t.Fatalf("lines=%v", lines)
	}
	if lines[0] != "+ zfs list -H -t snapshot -o name,guid,written,creation,used -r -d 2 root@debian:tank/src" {
		t.Fatalf("line0=%q", lines[0])
	}
	if lines[1] != "+ zfs list -H -t snapshot -o name,guid,written,creation,used -r -d 2 tank/tgt" {
		t.Fatalf("line1=%q", lines[1])
	}
	// Single endpoint (oracle -n with one operand) + minimal props.
	lines, err = Commands(Request{
		Source: endpoint.MustParse("tank/src"),
		Props:  MinimalListProps,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) != 1 || lines[0] != "+ zfs list -H -t snapshot -o name,guid tank/src" {
		t.Fatalf("lines=%v", lines)
	}
}
