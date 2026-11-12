package prune

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

// row is one object in a fake "zfs list" listing.
type row struct {
	name                          string
	guid                          string
	written, creation, referenced int64
	clones                        string
}

var listRe = regexp.MustCompile(`^(?:ssh -n \S+ )?zfs list -Hpr -o (\S+) .* '([^']+)'$`)

// fakeZFS answers "zfs list -Hpr -o PROPS ... 'DS'" for the given datasets,
// rendering the requested columns.
func fakeZFS(tr *app.TestRuntime, lists map[string][]row) {
	tr.Fake.OnFunc(listRe.MatchString, func(s string) execx.Result {
		m := listRe.FindStringSubmatch(s)
		rows, ok := lists[m[2]]
		if !ok {
			return execx.Fail(1, "cannot open '"+m[2]+"': dataset does not exist")
		}
		var out []string
		for _, r := range rows {
			var cells []string
			for p := range strings.SplitSeq(m[1], ",") {
				switch p {
				case "name":
					cells = append(cells, r.name)
				case "guid":
					cells = append(cells, r.guid)
				case "written":
					cells = append(cells, fmt.Sprint(r.written))
				case "creation":
					cells = append(cells, fmt.Sprint(r.creation))
				case "referenced":
					cells = append(cells, fmt.Sprint(r.referenced))
				case "clones":
					c := r.clones
					if c == "" {
						c = "-"
					}
					cells = append(cells, c)
				default:
					cells = append(cells, "0")
				}
			}
			out = append(out, strings.Join(cells, "\t"))
		}
		return execx.Out(out...)
	})
}

// nameFixture is the listing used by 072_prune_name_spec.sh.
func nameFixture() map[string][]row {
	r := func(name, guid string, creation int64, clones string) row {
		return row{name: name, guid: guid, creation: creation, clones: clones}
	}
	return map[string][]row{
		"apool/treetop": {
			r("apool/treetop", "1", 1209600, ""),
			r("apool/treetop@autosnap_new", "120", 1209600, ""),
			r("apool/treetop@manual_guard", "110", 1123200, ""),
			r("apool/treetop@autosnap_clone", "109", 1036800, "apool/clone"),
			r("apool/treetop@autosnap_old2", "108", 950400, ""),
			r("apool/treetop@manual_between", "107", 864000, ""),
			r("apool/treetop@autosnap_old1", "106", 777600, ""),
			r("apool/treetop@autosnap_old0", "105", 691200, ""),
			r("apool/treetop@auto-old", "104", 604800, ""),
			r("apool/treetop@zfsnap_old", "103", 518400, ""),
			r("apool/treetop@zelta.1", "102", 432000, ""),
			r("apool/treetop@zeltaX1", "101", 345600, ""),
		},
		"bpool/backups": {
			r("bpool/backups", "10", 1209600, ""),
			r("bpool/backups@manual_guard", "110", 1123200, ""),
		},
	}
}

func runPrune(t *testing.T, opts map[string]string, lists map[string][]row, args ...string) (*app.TestRuntime, error) {
	t.Helper()
	tr := app.NewTest("prune", opts)
	fakeZFS(tr, lists)
	err := Run(context.Background(), tr.Runtime, args)
	return tr, err
}

func TestPruneNameSelector(t *testing.T) {
	tests := []struct {
		name string
		opts map[string]string
		args []string
		want string
	}{
		{"sanoid", map[string]string{"NO_RANGES": "1", "PRUNE_NUM": "0", "PRUNE_NAME": "sanoid"}, []string{"apool/treetop"},
			"apool/treetop@autosnap_old0\napool/treetop@autosnap_old1\napool/treetop@autosnap_old2\napool/treetop@autosnap_new\n"},
		{"truenas", map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "truenas"}, []string{"apool/treetop"}, "apool/treetop@auto-old\n"},
		{"zfsnap", map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "zfsnap"}, []string{"apool/treetop"}, "apool/treetop@zfsnap_old\n"},
		{"glob", map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "zelta.?"}, []string{"apool/treetop"}, "apool/treetop@zelta.1\n"},
		{"guard", map[string]string{"NO_RANGES": "1", "PRUNE_NUM": "0", "PRUNE_NAME": "sanoid", "MATCH_ENDPOINT": "bpool/backups"}, []string{"apool/treetop"},
			"apool/treetop@autosnap_old0\napool/treetop@autosnap_old1\napool/treetop@autosnap_old2\n"},
		{"guard operand", map[string]string{"NO_RANGES": "1", "PRUNE_NUM": "0", "PRUNE_NAME": "sanoid"}, []string{"apool/treetop", "bpool/backups"},
			"apool/treetop@autosnap_old0\napool/treetop@autosnap_old1\napool/treetop@autosnap_old2\n"},
		{"filters", map[string]string{"PRUNE_NUM": "1", "PRUNE_NAME": "sanoid", "INCLUDE": "@autosnap_old*", "EXCLUDE": "@autosnap_old2"}, []string{"apool/treetop"},
			"apool/treetop@autosnap_old0\n"},
		{"ranges", map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "sanoid"}, []string{"apool/treetop"},
			"apool/treetop@autosnap_new\napool/treetop@autosnap_old2\napool/treetop@autosnap_old0%autosnap_old1\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tr, err := runPrune(t, tt.opts, nameFixture(), tt.args...)
			if err != nil {
				t.Fatal(err)
			}
			if tr.Out.String() != tt.want {
				t.Errorf("output:\n%s\nwant:\n%s", tr.Out.String(), tt.want)
			}
		})
	}
}

func TestPruneKeptInfo(t *testing.T) {
	tr, err := runPrune(t, map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "sanoid", "LOG_LEVEL": "3"}, nameFixture(), "apool/treetop")
	if err != nil {
		t.Fatal(err)
	}
	want := "listing source: apool/treetop\nkeeping: apool/treetop@autosnap_clone%manual_guard,apool/treetop@manual_between,apool/treetop@zeltaX1%auto-old\n"
	if tr.Err.String() != want {
		t.Errorf("kept:\n%s\nwant:\n%s", tr.Err.String(), want)
	}
}

// TestPruneGridAnchor mirrors 071_prune_grid_anchor_spec.sh: grid buckets
// are measured from the latest match, not from the current time.
func TestPruneGridAnchor(t *testing.T) {
	lists := map[string][]row{
		"apool/treetop": {
			{name: "apool/treetop", guid: "1", creation: 1209600},
			{name: "apool/treetop@zelta_match", guid: "100", creation: 1209600},
			{name: "apool/treetop@zelta_s3", guid: "101", creation: 950400},
			{name: "apool/treetop@zelta_s6", guid: "102", creation: 691200},
			{name: "apool/treetop@zelta_s8", guid: "103", creation: 518400},
			{name: "apool/treetop@zelta_s12", guid: "104", creation: 172800},
		},
		"bpool/backups": {
			{name: "bpool/backups", guid: "10", creation: 1209600},
			{name: "bpool/backups@zelta_match", guid: "100", creation: 1209600},
		},
	}
	tr := app.NewTest("prune", map[string]string{"NO_RANGES": "1", "PRUNE_NUM": "0", "PRUNE_TIME": "0", "PRUNE_GRID": "1200x6day", "SYSTIME": `printf "%s\n" 1468800`})
	fakeZFS(tr, lists)
	tr.Fake.On(`printf "%s\n" 1468800`, execx.Out("1468800"))
	if err := Run(context.Background(), tr.Runtime, []string{"apool/treetop", "bpool/backups"}); err != nil {
		t.Fatal(err)
	}
	if tr.Out.String() != "apool/treetop@zelta_s8\n" {
		t.Errorf("grid output = %q", tr.Out.String())
	}

	// Without a match the grid is anchored to the newest snapshot, and the
	// oldest and newest are always kept.
	tr, _ = runPrune(t, map[string]string{"NO_RANGES": "1", "PRUNE_GRID": "1week"}, lists, "apool/treetop")
	if tr.Out.String() != "apool/treetop@zelta_s6\n" {
		t.Errorf("unanchored grid = %q", tr.Out.String())
	}
}

func TestGridKeeps(t *testing.T) {
	grid, err := parseGrid("2x1 day | 1x 1week, 1 year")
	if err != nil || len(grid) != 3 || grid[0].count != 2 || grid[1].interval != 604800 || grid[2].count != -1 {
		t.Fatalf("grid = %+v %v", grid, err)
	}
	b := map[[2]int64]bool{}
	day := int64(86400)
	checks := []struct {
		age  int64
		want bool
	}{
		{0, true},          // day bucket 0
		{day / 2, false},   // same bucket
		{day + 1, true},    // day bucket 1
		{2 * day, true},    // week term, bucket 0
		{5 * day, false},   // same week bucket
		{9 * day, true},    // first yearly bucket starts after 9 days
		{100 * day, false}, // same year bucket
		{400 * day, true},  // second year bucket
		{-1, false},        // newer than the anchor
	}
	for _, c := range checks {
		if got := gridKeeps(grid, b, 1000*day-c.age, 1000*day); got != c.want {
			t.Errorf("age %d: %v want %v", c.age, got, c.want)
		}
	}
	bounded, _ := parseGrid("1x1d")
	if gridKeeps(bounded, map[[2]int64]bool{}, 0, 2*day) || gridKeeps(nil, b, 0, 1) {
		t.Errorf("snapshots beyond a bounded grid are not kept")
	}
	for _, bad := range []string{"x1d", "ax1d", "-1x1d", "+1x1d", "1x0", "1xfoo1", "0"} {
		if _, err := parseGrid(bad); err == nil {
			t.Errorf("parseGrid(%q) accepted", bad)
		}
	}
	if _, err := parseGrid("1m"); err == nil || !strings.Contains(err.Error(), "ambiguous duration unit 'm'") {
		t.Errorf("ambiguous grid err = %v", err)
	}
	if g, err := parseGrid(" , 1d,"); err != nil || len(g) != 1 {
		t.Errorf("empty terms are skipped: %v %v", g, err)
	}
}

// dailies builds n daily snapshots ending at app.TestNow, newest first.
func dailies(ds string, n int, written int64) []row {
	now := app.TestNow.Unix()
	rows := []row{{name: ds, guid: "1", creation: now - int64(n)*86400}}
	for i := range n {
		rows = append(rows, row{name: fmt.Sprintf("%s@d%02d", ds, n-1-i), guid: fmt.Sprint(1000 + n - i), creation: now - int64(i)*86400 - 60, written: written, referenced: written / 2})
	}
	return rows
}

func TestPruneDefaultRetention(t *testing.T) {
	tr, err := runPrune(t, nil, map[string][]row{"p/ds": dailies("p/ds", 45, 0)}, "p/ds")
	if err != nil {
		t.Fatal(err)
	}
	// Snapshots from the last 30 days and the newest 30 are kept; the
	// oldest 15 (d00..d14) are one contiguous range.
	if tr.Out.String() != "p/ds@d00%d14\n" {
		t.Errorf("default output = %q", tr.Out.String())
	}
	tr, _ = runPrune(t, map[string]string{"PRUNE_TIME": "40days"}, map[string][]row{"p/ds": dailies("p/ds", 45, 0)}, "p/ds")
	if tr.Out.String() != "p/ds@d00%d04\n" {
		t.Errorf("time-only output = %q", tr.Out.String())
	}
	tr, _ = runPrune(t, map[string]string{"PRUNE_NUM": "42"}, map[string][]row{"p/ds": dailies("p/ds", 45, 0)}, "p/ds")
	if tr.Out.String() != "p/ds@d00%d02\n" {
		t.Errorf("count-only output = %q", tr.Out.String())
	}
}

func TestPruneGuards(t *testing.T) {
	src := dailies("p/src", 6, 0)
	tgt := []row{{name: "p/tgt", guid: "9"}}
	for _, r := range src[1:] {
		if strings.HasSuffix(r.name, "@d00") || strings.HasSuffix(r.name, "@d02") || strings.HasSuffix(r.name, "@d03") {
			tgt = append(tgt, row{name: strings.Replace(r.name, "p/src", "p/tgt", 1), guid: r.guid})
		}
	}
	lists := map[string][]row{"p/src": src, "p/tgt": tgt}
	opts := func(guard string) map[string]string {
		return map[string]string{"PRUNE_NUM": "0", "NO_RANGES": "1", "PRUNE_GUARD": guard, "MATCH_ENDPOINT": "p/tgt"}
	}
	tests := map[string]string{
		"latest":   "p/src@d00\np/src@d01\np/src@d02\n",
		"":         "p/src@d00\np/src@d01\np/src@d02\n",
		"unsynced": "p/src@d00\np/src@d02\n",
		"none":     "p/src@d00\np/src@d01\np/src@d02\n",
		"NONE":     "p/src@d00\np/src@d01\np/src@d02\n",
	}
	for guard, want := range tests {
		tr, err := runPrune(t, opts(guard), lists, "p/src")
		if err != nil {
			t.Fatal(err)
		}
		if tr.Out.String() != want {
			t.Errorf("guard %q = %q want %q", guard, tr.Out.String(), want)
		}
	}
	if _, err := runPrune(t, opts("bogus"), lists, "p/src"); err == nil || err.Error() != "invalid prune-guard mode: bogus" {
		t.Errorf("invalid guard err = %v", err)
	}
	if _, err := runPrune(t, opts("latest"), lists, "p/src", "p/tgt"); err == nil || err.Error() != "cannot use --match-endpoint with a second positional operand" {
		t.Errorf("conflict err = %v", err)
	}
}

func TestPruneSizeSelection(t *testing.T) {
	lists := map[string][]row{"p/ds": dailies("p/ds", 6, 100)}
	// Each snapshot writes 100 bytes and references 50: selecting oldest
	// first, two snapshots reach an estimate of 200-50 >= 120.
	tr, err := runPrune(t, map[string]string{"PRUNE_SIZE": "120", "NO_RANGES": "1"}, lists, "p/ds")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Out.String() != "p/ds@d00\np/ds@d01\n" {
		t.Errorf("size output = %q", tr.Out.String())
	}
	tr, _ = runPrune(t, map[string]string{"PRUNE_SIZE": "1G"}, lists, "p/ds")
	if tr.Out.String() != "p/ds@d00%d05\n" {
		t.Errorf("unreachable size selects everything eligible: %q", tr.Out.String())
	}
	tr, _ = runPrune(t, map[string]string{"PRUNE_SIZE": "0"}, lists, "p/ds")
	if tr.Out.String() != "p/ds@d00%d05\n" {
		t.Errorf("zero size = %q", tr.Out.String())
	}
}

func TestPruneProtections(t *testing.T) {
	rows := dailies("p/ds", 5, 0)
	rows[2].clones = "p/clone"                                                            // d03
	rows = append(rows[:3], append([]row{{name: "p/ds#bm", guid: "77"}}, rows[3:]...)...) // a bookmark between d03 and d02
	tr, err := runPrune(t, map[string]string{"PRUNE_NUM": "0", "LOG_LEVEL": "3"}, map[string][]row{"p/ds": rows}, "p/ds")
	if err != nil {
		t.Fatal(err)
	}
	if tr.Out.String() != "p/ds@d04\np/ds@d00%d02\n" {
		t.Errorf("clone protection = %q", tr.Out.String())
	}
	if tr.Err.String() != "listing source: p/ds\nkeeping: p/ds@d03\n" {
		t.Errorf("kept = %q", tr.Err.String())
	}
}

func TestPruneVisualAndSendRange(t *testing.T) {
	tr, err := runPrune(t, map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "sanoid", "PRUNE_VISUAL": "1", "MATCH_ENDPOINT": "bpool/backups"}, nameFixture(), "apool/treetop")
	if err != nil {
		t.Fatal(err)
	}
	if want := "apool/treetop\n🔹🔹🔹🔹❌❌🔹❌🔹🔹🔹\n"; tr.RawOut.String() != want || tr.Out.Len() != 0 {
		t.Errorf("visual = %q want %q", tr.RawOut.String(), want)
	}

	lists := nameFixture()
	lists["bpool/backups"] = append(lists["bpool/backups"], row{name: "bpool/backups/only", guid: "11"})
	tr, _ = runPrune(t, map[string]string{"PRUNE_VISUAL": "1"}, lists, "apool/treetop", "bpool/backups")
	if !strings.HasSuffix(tr.RawOut.String(), "apool/treetop/only\n\n") {
		t.Errorf("target-only visual = %q", tr.RawOut.String())
	}

	tr, _ = runPrune(t, map[string]string{"SEND_RANGE": "@autosnap_old2", "NO_RANGES": "1", "EXCLUDE": "@manual_guard"}, nameFixture(), "apool/treetop")
	if tr.Out.String() != "apool/treetop@autosnap_clone\napool/treetop@autosnap_new\n" {
		t.Errorf("send range = %q", tr.Out.String())
	}
	tr, _ = runPrune(t, map[string]string{"SEND_RANGE": "@nope"}, nameFixture(), "apool/treetop")
	if tr.Out.Len() != 0 {
		t.Errorf("unknown send range = %q", tr.Out.String())
	}
}

func TestPruneErrors(t *testing.T) {
	var ue *app.UsageError
	cases := []struct {
		opts map[string]string
		args []string
		want string
		use  bool
	}{
		{map[string]string{"PRUNE_NUM": "abc"}, []string{"p/ds"}, "invalid --prune-num: abc", true},
		{map[string]string{"PRUNE_TIME": "1m"}, []string{"p/ds"}, "ambiguous duration unit 'm'; use 'mi' or 'mo'", false},
		{map[string]string{"PRUNE_TIME": "abc"}, []string{"p/ds"}, "invalid --prune-time: abc", false},
		{map[string]string{"PRUNE_TIME": "2 fortnights"}, []string{"p/ds"}, "invalid duration '2 fortnights'", false},
		{map[string]string{"PRUNE_SIZE": "lots"}, []string{"p/ds"}, "invalid --prune-size: lots", true},
		{map[string]string{"PRUNE_GRID": "ax1d"}, []string{"p/ds"}, "invalid --prune-grid term: ax1d", true},
		{map[string]string{"DEPTH": "x"}, []string{"p/ds"}, "depth of 'x' invalid; must be positive", false},
		{nil, nil, "no datasets defined", true},
		{nil, []string{"host:"}, "invalid endpoint 'host:'", false},
	}
	for _, c := range cases {
		_, err := runPrune(t, c.opts, map[string][]row{"p/ds": dailies("p/ds", 2, 0)}, c.args...)
		if err == nil || err.Error() != c.want || errors.As(err, &ue) != c.use {
			t.Errorf("%v: err = %v (usage=%v) want %q", c.opts, err, errors.As(err, &ue), c.want)
		}
	}

	tr, err := runPrune(t, nil, map[string][]row{}, "p/missing")
	if err != nil || tr.Err.String() != "error: datasets inaccessible or do not exist\n" {
		t.Errorf("missing dataset: %v %q", err, tr.Err.String())
	}

	// Dry runs only print the list commands and skip option validation.
	tr, err = runPrune(t, map[string]string{"DRYRUN": "1", "PRUNE_GRID": "bad"}, nil, "p/ds", "p/tgt")
	if err != nil || len(tr.Fake.Calls) != 0 {
		t.Fatalf("dry run: %v %v", err, tr.Fake.Calls)
	}
	want := "+ zfs list -Hpr -o name,guid,written,creation,used,referenced,clones -t all -Screatetxg 'p/ds'\n" +
		"+ zfs list -Hpr -o name,guid,written,creation,used,referenced -t all -Screatetxg 'p/tgt'\n"
	if tr.Out.String() != want {
		t.Errorf("dry run = %q", tr.Out.String())
	}

	tr = app.NewTest("prune", map[string]string{"SYSTIME": "false"})
	tr.Fake.On("false", execx.Fail(1))
	if n, err := clock(context.Background(), tr.Runtime); err != nil || n != app.TestNow.Unix() {
		t.Errorf("SYSTIME fallback = %d %v", n, err)
	}
	if _, ok, err := duration("x"); ok || err != nil {
		t.Errorf("duration without number")
	}
}

func TestRanges(t *testing.T) {
	e := func(idx ...int) []entry {
		var out []entry
		for _, i := range idx {
			out = append(out, entry{fmt.Sprintf("@s%d", i), i})
		}
		return out
	}
	tests := []struct {
		in   []entry
		want string
	}{
		{nil, ""},
		{e(1), "@s1"},
		{e(1, 2, 3), "@s3%s1"},
		{e(1, 3, 4, 7), "@s7,@s4%s3,@s1"},
		{e(2, 3, 5, 6), "@s6%s5,@s3%s2"},
	}
	for _, tt := range tests {
		if got := strings.Join(ranges(tt.in), ","); got != tt.want {
			t.Errorf("ranges(%v) = %q want %q", tt.in, got, tt.want)
		}
	}
}
