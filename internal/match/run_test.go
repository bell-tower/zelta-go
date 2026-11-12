package match

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

func runMatch(t *testing.T, opts map[string]string, args ...string) (*app.TestRuntime, error) {
	t.Helper()
	tr := app.NewTest("match", opts)
	src, tgt := divergentTree()
	fakeLists(tr, map[string]*listing{"apool/treetop": src, "bpool/backups": tgt})
	if args == nil {
		args = []string{"apool/treetop", "bpool/backups"}
	}
	err := Run(context.Background(), tr.Runtime, args)
	return tr, err
}

var spaces = regexp.MustCompile(`\s+`)

func normalized(s string) []string {
	var out []string
	for l := range strings.SplitSeq(strings.TrimSuffix(s, "\n"), "\n") {
		out = append(out, strings.TrimSpace(spaces.ReplaceAllString(l, " ")))
	}
	return out
}

// TestRunDivergentTree checks the exact lines the shellspec suite expects
// from "zelta match" after make_tree_divergence (040_zelta_tests_spec.sh).
func TestRunDivergentTree(t *testing.T) {
	tr, err := runMatch(t, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"DS_SUFFIX MATCH SRC_LAST TGT_LAST INFO",
		"[treetop] @start @start @start up-to-date",
		"/sub1 @start @start @start up-to-date",
		"/sub1/child - - - syncable (full)",
		"/sub1/kid - - - no source (target only)",
		"/sub2 - @two @two blocked sync: target diverged",
		"/sub2/orphan @start @start @start up-to-date",
		"/sub3 @start @two @start syncable (incremental)",
		"/sub3/space name @start @start @blocker blocked sync: target diverged",
		"/sub4 @start @start @start up-to-date",
		"/sub4/encrypted @start @start @start up-to-date",
		"/sub4/zvol - @start - blocked sync: no target snapshots",
		"5 up-to-date, 2 syncable, 4 blocked",
		"11 total datasets compared",
	}
	got := normalized(tr.Out.String())
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("output:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	lines := strings.Split(tr.Out.String(), "\n")
	infoPad := strings.Repeat(" ", len("blocked sync: no target snapshots")-len("INFO"))
	if lines[0] != "DS_SUFFIX         MATCH   SRC_LAST  TGT_LAST  INFO"+infoPad {
		t.Errorf("header padding = %q", lines[0])
	}
	if lines[1] != "[treetop]         @start  @start    @start    up-to-date"+strings.Repeat(" ", 33-10) {
		t.Errorf("row padding = %q", lines[1])
	}
	if tr.Err.Len() != 0 {
		t.Errorf("stderr = %q", tr.Err.String())
	}
}

func TestRunScripting(t *testing.T) {
	tr, err := runMatch(t, map[string]string{"SCRIPTING_MODE": "1", "PARSABLE": "1", "PROPLIST": "relname,match,srcnext,srclast,tgtlast"})
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(tr.Out.String(), "\n"), "\n")
	if len(lines) != 11 {
		t.Fatalf("lines = %#v", lines)
	}
	for i, want := range map[int]string{
		0: "\t@start\t\t@start\t@start",
		2: "/sub1/child\t\t\t\t",
		6: "/sub3\t@start\t@two\t@two\t@start",
		4: "/sub2\t\t@two\t@two\t@two",
	} {
		if lines[i] != want {
			t.Errorf("line %d = %q want %q", i, lines[i], want)
		}
	}
	if strings.Contains(tr.Out.String(), "up-to-date") {
		t.Errorf("scripting mode printed summary")
	}
}

func TestRunColumns(t *testing.T) {
	tr, err := runMatch(t, map[string]string{"PROPLIST": "ds_suffix,count,size,snaps,written,name,first,next,status"})
	if err != nil {
		t.Fatal(err)
	}
	got := normalized(tr.Out.String())
	if got[0] != "DS_SUFFIX NUM_MATCHES XFER_NUM XFER_SIZE SRC_SNAPS TGT_SNAPS SRC_WRITTEN TGT_WRITTEN SRC_NAME TGT_NAME SRC_FIRST TGT_FIRST SRC_NEXT TGT_NEXT INFO" {
		t.Errorf("header = %q", got[0])
	}
	if got[3] != "/sub1/child - - 0B - - 0B 0B apool/treetop/sub1/child - - - - - syncable (full)" {
		t.Errorf("child = %q", got[3])
	}
	if got[7] != "/sub3 1 1 0B 2 1 0B 0B apool/treetop/sub3 bpool/backups/sub3 @start @start @two - syncable (incremental)" {
		t.Errorf("sub3 = %q", got[7])
	}

	tr, _ = runMatch(t, map[string]string{"PROPLIST": "all", "SCRIPTING_MODE": "1"})
	if f := strings.Split(strings.Split(tr.Out.String(), "\n")[0], "\t"); len(f) != 19 {
		t.Errorf("all columns = %d", len(f))
	}
	if !strings.Contains(tr.Fake.Calls[0], "ivsetguid") {
		t.Errorf("match_ivset should list ivsetguid: %s", tr.Fake.Calls[0])
	}
}

func TestRunSizes(t *testing.T) {
	src := newListing("p/s").add("@b", "2", 3<<20).add("@a", "1", 1<<20).add("", "10", 5<<20)
	tgt := newListing("p/t").add("@a", "1", 0).add("", "20", 0)
	run := func(opts map[string]string) string {
		tr := app.NewTest("match", opts)
		tr.Fake.On("zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg 'p/s'", src.result())
		tr.Fake.On("zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg 'p/t'", tgt.result())
		if err := Run(context.Background(), tr.Runtime, []string{"p/s", "p/t"}); err != nil {
			t.Fatal(err)
		}
		return normalized(tr.Out.String())[1]
	}
	if got := run(map[string]string{"PROPLIST": "xfersize,srcwritten,tgtwritten"}); got != "3M 5M 0B" {
		t.Errorf("human sizes = %q", got)
	}
	if got := run(map[string]string{"PROPLIST": "xfersize,srcwritten,tgtwritten", "PARSABLE": "1"}); got != "3145728 5242880 0" {
		t.Errorf("parsable sizes = %q", got)
	}
}

func TestRunTimeAndSingleOperand(t *testing.T) {
	tr := app.NewTest("match", map[string]string{"CHECK_TIME": "1", "SCRIPTING_MODE": "1"})
	tr.Fake.On(srcListCmd, newListing("apool/treetop").add("@a", "1", 0).add("", "0", 0).result())
	if err := Run(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil {
		t.Fatal(err)
	}
	out := tr.Out.String()
	if !strings.HasPrefix(out, "\t\t@a\t\tsyncable (full)\n") {
		t.Errorf("single operand = %q", out)
	}
	if strings.Contains(out, "TARGET_LIST_TIME") {
		t.Errorf("no target time expected")
	}

	tr = app.NewTest("match", nil)
	tr.Fake.On(srcListCmd, newListing("apool/treetop").add("@a", "1", 0).add("", "0", 0).result())
	Run(context.Background(), tr.Runtime, []string{"apool/treetop"})
	if got := normalized(tr.Out.String()); strings.Join(got, "|") != "DS_SUFFIX MATCH SRC_LAST TGT_LAST INFO|[treetop] - @a - syncable (full)|1 syncable" {
		t.Errorf("single operand table = %q", got)
	}
}

func TestPrintCheckTime(t *testing.T) {
	tr := app.NewTest("match", map[string]string{"CHECK_TIME": "1", "SCRIPTING_MODE": "1"})
	res := &Result{Source: newTree(tr.Endpoint("a")), Target: newTree(tr.Endpoint("b")), Pairs: []*Pair{{Suffix: "", Info: "up-to-date"}}}
	res.Source.ListTime, res.Target.ListTime = 0.25, 1
	cols, _ := parseColumns("info")
	printResult(tr.Runtime, res, cols)
	if got := tr.Out.String(); got != "up-to-date\nSOURCE_LIST_TIME:\t0.25\nTARGET_LIST_TIME:\t1\n" {
		t.Errorf("time output = %q", got)
	}
}

func TestRunErrors(t *testing.T) {
	var ue *app.UsageError
	if _, err := runMatch(t, map[string]string{"PROPLIST": "match,bogus_col"}); !errors.As(err, &ue) || ue.Msg != "bad property list: invalid property 'boguscol'" {
		t.Errorf("bad proplist err = %v", err)
	}
	if _, err := runMatch(t, map[string]string{"PROPLIST": "match,"}); !errors.As(err, &ue) || !strings.Contains(ue.Msg, "invalid property ''") {
		t.Errorf("empty property err = %v", err)
	}
	if _, err := runMatch(t, nil, []string{}...); !errors.As(err, &ue) || ue.Msg != "no datasets defined" {
		t.Errorf("no operands err = %v", err)
	}
	if _, err := runMatch(t, map[string]string{"DEPTH": "-1"}); err == nil || err.Error() != "depth of '-1' invalid; must be positive" || app.Code(err) != 1 {
		t.Errorf("depth err = %v", err)
	}
	if _, err := runMatch(t, nil, "host:"); err == nil || err.Error() != "invalid endpoint 'host:'" {
		t.Errorf("endpoint err = %v", err)
	}

	tr := app.NewTest("match", nil)
	tr.Fake.On(srcListCmd, execx.Fail(1, "cannot open 'apool/treetop': dataset does not exist"))
	tr.Fake.On(tgtListCmd, execx.Fail(1, "cannot open 'bpool/backups': dataset does not exist"))
	if err := Run(context.Background(), tr.Runtime, []string{"apool/treetop", "bpool/backups"}); err != nil {
		t.Errorf("missing datasets exit 0 like zelta 1.2: %v", err)
	}
	if tr.Err.String() != "error: datasets inaccessible or do not exist\n" {
		t.Errorf("stderr = %q", tr.Err.String())
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tr = app.NewTest("match", nil)
	if err := Run(ctx, tr.Runtime, []string{"a"}); err == nil {
		t.Errorf("expected context error")
	}
}

func TestRunDryRun(t *testing.T) {
	tr, err := runMatch(t, map[string]string{"DRYRUN": "1"})
	if err != nil || len(tr.Fake.Calls) != 0 {
		t.Fatalf("err=%v calls=%v", err, tr.Fake.Calls)
	}
	if got := tr.Out.String(); got != "+ "+srcListCmd+"\n+ "+tgtListCmd+"\n" {
		t.Errorf("dry run = %q", got)
	}
}

func TestDepthRun(t *testing.T) {
	tr, err := runMatch(t, map[string]string{"DEPTH": "1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := normalized(tr.Out.String()); len(got) != 3 || got[2] != "1 up-to-date" {
		t.Errorf("depth 1 output = %q", got)
	}
}

func TestColumnsCatalog(t *testing.T) {
	cols := Columns()
	if len(cols) != 19 || cols[0].Name != "ds_suffix" || cols[18].Name != "info" {
		t.Errorf("columns = %d", len(cols))
	}
	cols[0].Name = "changed"
	if Columns()[0].Name != "ds_suffix" {
		t.Errorf("Columns must return a copy")
	}
	names := strings.Join(ColumnNames(), ",")
	for _, want := range []string{"all", "ds_suffix", "relname", "last", "count", "status"} {
		if !strings.Contains(","+names+",", ","+want+",") {
			t.Errorf("ColumnNames missing %q", want)
		}
	}
	got, bad := parseColumns("last")
	if bad != "" || len(got) != 2 || got[0].Name != "src_last" || got[1].Name != "tgt_last" {
		t.Errorf("last = %v %q", got, bad)
	}
	got, _ = parseColumns("")
	if len(got) != 5 {
		t.Errorf("default columns = %d", len(got))
	}
	if humanValue("", &Column{Name: "x", kind: kindText}, "l", false) != "-" || humanValue("0", &Column{kind: kindNum}, "l", false) != "-" {
		t.Errorf("placeholders")
	}
	if rawValue(&Pair{}, &Column{Name: "nope"}) != "" {
		t.Errorf("unknown column")
	}
}

var listOptRe = regexp.MustCompile(`^zfs list -Hpr -o (\S+) .* '([^']+)'$`)

// fakeLists answers any "zfs list" for the given datasets, rendering the
// listing's name,guid,written,creation,used rows in the requested columns.
func fakeLists(tr *app.TestRuntime, lists map[string]*listing) {
	tr.Fake.OnFunc(func(s string) bool { return listOptRe.MatchString(s) }, func(s string) execx.Result {
		m := listOptRe.FindStringSubmatch(s)
		l := lists[m[2]]
		if l == nil {
			return execx.Fail(1, "cannot open '"+m[2]+"': dataset does not exist")
		}
		src := map[string]int{"name": 0, "guid": 1, "written": 2, "creation": 3, "used": 4}
		var out []string
		for _, line := range l.lines {
			f := strings.Split(line, "\t")
			var cells []string
			for p := range strings.SplitSeq(m[1], ",") {
				if i, ok := src[p]; ok {
					cells = append(cells, f[i])
				} else {
					cells = append(cells, "-")
				}
			}
			out = append(out, strings.Join(cells, "\t"))
		}
		return execx.Out(out...)
	})
}
