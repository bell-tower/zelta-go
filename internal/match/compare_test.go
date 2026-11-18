package match

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"zelta.space/internal/app"
	"zelta.space/internal/execx"
)

// listing builds "zfs list -Hpr -o name,guid,written,creation,used" output.
type listing struct {
	root  string
	lines []string
	txg   int64
}

func newListing(root string) *listing { return &listing{root: root, txg: 1000} }

// add appends an object; suffix is relative to root (e.g. "/sub1@start").
func (l *listing) add(suffix, guid string, written int64) *listing {
	l.txg--
	l.lines = append(l.lines, fmt.Sprintf("%s%s\t%s\t%d\t%d\t%d", l.root, suffix, guid, written, 1700000000+l.txg, 1024))
	return l
}

func (l *listing) result() execx.Result { return execx.Out(l.lines...) }

const (
	srcListCmd = "zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg 'apool/treetop'"
	tgtListCmd = "zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg 'bpool/backups'"
)

// divergentTree reproduces the state of the shellspec "divergent tree" test
// after make_tree_divergence, newest objects first.
func divergentTree() (src, tgt *listing) {
	src = newListing("apool/treetop").
		add("/sub2@two", "202", 0).
		add("/sub3@two", "302", 0).
		add("/sub1/child", "110", 0).
		add("@start", "1", 0).
		add("/sub1@start", "11", 0).
		add("/sub2/orphan@start", "21", 0).
		add("/sub3@start", "31", 0).
		add("/sub3/space name@start", "32", 0).
		add("/sub4@start", "41", 0).
		add("/sub4/zvol@start", "42", 0).
		add("/sub4/encrypted@start", "43", 0).
		add("/sub4/encrypted", "430", 0).
		add("/sub4/zvol", "420", 0).
		add("/sub4", "400", 0).
		add("/sub3/space name", "320", 0).
		add("/sub3", "300", 0).
		add("/sub2/orphan", "210", 0).
		add("/sub2", "200", 0).
		add("/sub1", "100", 0).
		add("", "0", 0)
	tgt = newListing("bpool/backups").
		add("/sub2@two", "999", 0).
		add("/sub3/space name@blocker", "998", 0).
		add("/sub1/kid", "9100", 0).
		add("@start", "1", 0).
		add("/sub1@start", "11", 0).
		add("/sub2/orphan@start", "21", 0).
		add("/sub3@start", "31", 0).
		add("/sub3/space name@start", "32", 0).
		add("/sub4@start", "41", 0).
		add("/sub4/encrypted@start", "43", 0).
		add("/sub4/encrypted", "8430", 0).
		add("/sub4/zvol", "8420", 0).
		add("/sub4", "8400", 0).
		add("/sub3/space name", "8320", 0).
		add("/sub3", "8300", 0).
		add("/sub2/orphan", "8210", 0).
		add("/sub2", "8200", 0).
		add("/sub1", "8100", 0).
		add("", "8000", 0)
	return src, tgt
}

func compareTree(t *testing.T, opts map[string]string, src, tgt *listing) (*Result, *app.TestRuntime) {
	t.Helper()
	tr := app.NewTest("match", opts)
	tr.Fake.On(srcListCmd, src.result())
	tr.Fake.On(tgtListCmd, tgt.result())
	req := RequestFromOptions(tr.Runtime, tr.Endpoint("apool/treetop"), tr.Endpoint("bpool/backups"))
	res, err := Compare(context.Background(), tr.Runtime, req)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Fake.Unmatched) > 0 {
		t.Fatalf("unmatched commands: %v", tr.Fake.Unmatched)
	}
	return res, tr
}

func TestCompareDivergentTree(t *testing.T) {
	src, tgt := divergentTree()
	res, _ := compareTree(t, nil, src, tgt)
	want := []string{
		"|@start|@start|@start|up-to-date",
		"/sub1|@start|@start|@start|up-to-date",
		"/sub1/child||||syncable (full)",
		"/sub1/kid||||no source (target only)",
		"/sub2||@two|@two|blocked sync: target diverged",
		"/sub2/orphan|@start|@start|@start|up-to-date",
		"/sub3|@start|@two|@start|syncable (incremental)",
		"/sub3/space name|@start|@start|@blocker|blocked sync: target diverged",
		"/sub4|@start|@start|@start|up-to-date",
		"/sub4/encrypted|@start|@start|@start|up-to-date",
		"/sub4/zvol||@start||blocked sync: no target snapshots",
	}
	if len(res.Pairs) != len(want) {
		t.Fatalf("got %d pairs", len(res.Pairs))
	}
	for i, p := range res.Pairs {
		got := strings.Join([]string{p.Suffix, p.Match, p.SrcLast, p.TgtLast, p.Info}, "|")
		if got != want[i] {
			t.Errorf("pair %d = %q want %q", i, got, want[i])
		}
	}
	if res.Summary != "5 up-to-date, 2 syncable, 4 blocked" || res.UpToDate != 5 || res.Syncable != 2 || res.Blocked != 4 {
		t.Errorf("summary = %q", res.Summary)
	}
	p := res.Pair("/sub3")
	if p.SrcNext != "@two" || p.XferNum != 1 || p.NumMatches != 1 || p.MatchIdx != 2 || p.SrcFirst != "@start" || p.SrcSnaps != 2 {
		t.Errorf("/sub3 = %+v", p)
	}
	if p := res.Pair("/sub3/space name"); p.TgtNext != "@blocker" || p.NumBlocked != 1 || p.TgtFirst != "@start" {
		t.Errorf("space name = %+v", p)
	}
	if p := res.Pair("/sub2"); p.NumBlocked != 1 || p.TgtNext != "@two" || p.Match != "" || p.SrcNext != "@two" {
		t.Errorf("/sub2 = %+v", p)
	}
	if p := res.Pair("/sub1/child"); p.Status != StatusSourceOnly || p.SrcName != "apool/treetop/sub1/child" || p.TgtName != "" {
		t.Errorf("child = %+v", p)
	}
	if res.Pair("/nope") != nil {
		t.Errorf("missing pair lookup")
	}
	if d := res.Source.Dataset("/sub3"); d == nil || len(d.Snaps) != 2 || d.Snaps[0].SnapIdx != 1 || d.Snaps[1].Creation == 0 {
		t.Errorf("source tree = %+v", d)
	}
}

func TestCompareWrittenTargetAndBookmarks(t *testing.T) {
	src := newListing("apool/treetop").add("@a", "5", 0).add("", "0", 0)
	tgt := newListing("bpool/backups").add("@a", "5", 0).add("", "1", 4096)
	res, _ := compareTree(t, nil, src, tgt)
	if p := res.Pairs[0]; p.Info != "blocked sync: target is written" || p.Match != "@a" || p.TgtWritten != "4096" {
		t.Errorf("pair = %+v", p)
	}

	// Bookmarks count as savepoints, and the newest GUID match wins even when
	// it is a bookmark, so the pair reads as diverged (as in Zelta 1.2).
	src = newListing("apool/treetop").add("#bk", "5", 0).add("@b", "6", 0).add("@a", "5", 0).add("", "0", 0)
	tgt = newListing("bpool/backups").add("@a", "5", 0).add("", "1", 0)
	res, _ = compareTree(t, nil, src, tgt)
	if p := res.Pairs[0]; p.SrcLast != "#bk" || p.SrcSnaps != 3 || p.Match != "#bk" || p.NumMatches != 2 || p.Info != "blocked sync: target diverged" {
		t.Errorf("bookmark savepoints: %+v", p)
	}

	// A source bookmark can be the match when the target still has the snapshot.
	src = newListing("apool/treetop").add("@c", "7", 10).add("#a", "5", 0).add("", "0", 0)
	tgt = newListing("bpool/backups").add("@a", "5", 0).add("", "1", 0)
	res, _ = compareTree(t, nil, src, tgt)
	if p := res.Pairs[0]; p.Match != "#a" || p.SrcNext != "@c" || p.XferSize != 10 || p.Info != "blocked sync: target diverged" {
		t.Errorf("bookmark match = %+v", p)
	}

	// A target bookmark never counts as a match.
	src = newListing("apool/treetop").add("@a", "5", 0).add("", "0", 0)
	tgt = newListing("bpool/backups").add("#a", "5", 0).add("", "1", 0)
	res, _ = compareTree(t, nil, src, tgt)
	if p := res.Pairs[0]; p.Match != "" || p.NumMatches != 0 || p.XferNum != 0 || p.Info != "blocked sync: target diverged" {
		t.Errorf("target bookmark = %+v", p)
	}
}

func TestCompareFilters(t *testing.T) {
	src, tgt := divergentTree()
	res, tr := compareTree(t, map[string]string{"EXCLUDE": "/sub4,@two,apool/treetop/sub1/child,bad*"}, src, tgt)
	var suffixes []string
	for _, p := range res.Pairs {
		suffixes = append(suffixes, p.Suffix+"="+p.Info)
	}
	got := strings.Join(suffixes, ";")
	want := "=up-to-date;/sub1=up-to-date;/sub1/kid=no source (target only);/sub2=blocked sync: target diverged;/sub2/orphan=up-to-date;/sub3=up-to-date;/sub3/space name=blocked sync: target diverged"
	if got != want {
		t.Errorf("excluded:\n %s\nwant\n %s", got, want)
	}
	if !strings.Contains(tr.Err.String(), "invalid filter pattern 'bad*'") {
		t.Errorf("missing filter warning: %q", tr.Err.String())
	}

	res, _ = compareTree(t, map[string]string{"INCLUDE": "/sub3"}, src, tgt)
	if len(res.Pairs) != 2 || res.Pairs[0].Suffix != "/sub3" || res.Pairs[1].Suffix != "/sub3/space name" {
		t.Errorf("include /sub3 = %d pairs", len(res.Pairs))
	}

	res, _ = compareTree(t, map[string]string{"INCLUDE": "@start"}, src, tgt)
	if p := res.Pair("/sub3"); p.SrcLast != "@start" || p.Info != "up-to-date" || len(res.Pairs) != 11 {
		t.Errorf("include @start = %+v", p)
	}

	res, _ = compareTree(t, map[string]string{"INCLUDE": "*/space*"}, src, tgt)
	if len(res.Pairs) != 1 || res.Pairs[0].Suffix != "/sub3/space name" || res.Pairs[0].SrcLast != "@start" {
		t.Errorf("include glob = %+v", res.Pairs)
	}

	res, _ = compareTree(t, map[string]string{"INCLUDE": "apool/treetop"}, src, tgt)
	if p := res.Pair(""); p == nil || p.SrcLast != "@two" && p.SrcLast != "@start" {
		t.Errorf("absolute include keeps top snapshots: %+v", p)
	}
}

func TestCompareDepthAndPrune(t *testing.T) {
	src, tgt := divergentTree()
	tr := app.NewTest("match", map[string]string{"DEPTH": "2"})
	tr.Fake.On(strings.Replace(srcListCmd, "-Screatetxg", "-Screatetxg -d2", 1), src.result())
	tr.Fake.On(strings.Replace(tgtListCmd, "-Screatetxg", "-Screatetxg -d2", 1), tgt.result())
	res, err := Compare(context.Background(), tr.Runtime, RequestFromOptions(tr.Runtime, tr.Endpoint("apool/treetop"), tr.Endpoint("bpool/backups")))
	if err != nil || len(tr.Fake.Unmatched) > 0 {
		t.Fatalf("err=%v unmatched=%v", err, tr.Fake.Unmatched)
	}
	for _, p := range res.Pairs {
		if strings.Count(p.Suffix, "/") > 1 {
			t.Errorf("depth 2 included %q", p.Suffix)
		}
	}
	if len(res.Pairs) != 5 {
		t.Errorf("depth 2 pairs = %d", len(res.Pairs))
	}

	req := Request{Source: tr.Endpoint("apool/treetop"), Target: tr.Endpoint("bpool/backups"), Prune: true, Exclude: "@two"}
	if got := strings.Join(req.ListProps(true), ","); got != "name,guid,written,creation,used,referenced,clones" {
		t.Errorf("prune source props = %s", got)
	}
	if got := strings.Join(req.ListProps(false), ","); got != "name,guid,written,creation,used,referenced" {
		t.Errorf("prune target props = %s", got)
	}
	req.IVSet = true
	if got := strings.Join(req.ListProps(false), ","); !strings.HasPrefix(got, "name,guid,ivsetguid,") {
		t.Errorf("ivset props = %s", got)
	}
}

func TestComparePruneKeepsFilteredSnapshots(t *testing.T) {
	tr := app.NewTest("prune", nil)
	tr.Fake.On("zfs list -Hpr -o name,guid,written,creation,used,referenced,clones -t all -Screatetxg 'pool/ds'",
		execx.Out("pool/ds@c\t3\t1\t30\t1\t1\t", "pool/ds@b\t2\t1\t20\t1\t1\tpool/clone", "pool/ds@a\t1\t1\t10\t1\t1\t", "pool/ds\t9\t0\t1\t1\t1\t"))
	res, err := Compare(context.Background(), tr.Runtime, Request{Source: tr.Endpoint("pool/ds"), Prune: true, Exclude: "@b"})
	if err != nil {
		t.Fatal(err)
	}
	d := res.Source.Dataset("")
	if len(d.Snaps) != 3 || !d.Snaps[1].Filtered || d.Snaps[1].Clones != "pool/clone" || d.Snaps[2].SnapIdx != 3 || d.Snaps[0].Referenced != 1 {
		t.Errorf("prune rows = %+v %+v %+v", d.Snaps[0], d.Snaps[1], d.Snaps[2])
	}
	if d.ByGUID("2") != nil || d.ByGUID("1") == nil || !d.HasSnapshotNamed("@b") || d.HasSnapshotNamed("@z") {
		t.Errorf("filtered snapshots must not be GUID-matchable")
	}
	if res.Pairs[0].Info != "syncable (full)" || res.Target.Listed {
		t.Errorf("no target: %+v", res.Pairs[0])
	}
	var nilDS *Dataset
	if nilDS.ByGUID("x") != nil || nilDS.HasSnapshotNamed("@x") || (*Tree)(nil).Dataset("") != nil {
		t.Errorf("nil receivers")
	}
}

func TestCompareDryRunAndEdgeCases(t *testing.T) {
	tr := app.NewTest("match", map[string]string{"DRYRUN": "1", "DEPTH": "3"})
	res, err := Compare(context.Background(), tr.Runtime, RequestFromOptions(tr.Runtime, tr.Endpoint("me@src:apool/treetop"), tr.Endpoint("bpool/backups")))
	if res != nil || err != nil || len(tr.Fake.Calls) != 0 {
		t.Fatalf("dry run executed commands: %v", tr.Fake.Calls)
	}
	want := "+ ssh -n me@src zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg -d3 'apool/treetop'\n" +
		"+ zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg -d3 'bpool/backups'\n"
	if tr.Out.String() != want {
		t.Errorf("dry run output:\n%s\nwant\n%s", tr.Out.String(), want)
	}

	tr = app.NewTest("match", map[string]string{"LOG_LEVEL": "3"})
	tr.Fake.On(srcListCmd, execx.Out("apool/treetop\t1\t0\t1\t1", "garbage line").WithStderr("cannot open 'apool/treetop': dataset does not exist", "\r"))
	res, _ = Compare(context.Background(), tr.Runtime, Request{Source: tr.Endpoint("apool/treetop"), Target: tr.Endpoint("apool/treetop"), Written: true})
	if len(res.Pairs) != 1 || tr.Fake.CallsMatching("list") == nil {
		t.Errorf("pairs = %d", len(res.Pairs))
	}
	errs := tr.Err.String()
	if !strings.Contains(errs, "warning: identical source and target") || !strings.Contains(errs, "warning: stream output unexpected: garbage line") || strings.Contains(errs, "does not exist") {
		t.Errorf("stderr = %q", errs)
	}
	if !strings.Contains(errs, "listing source: apool/treetop") {
		t.Errorf("missing info message: %q", errs)
	}

	tr = app.NewTest("backup", map[string]string{"PROPLIST": "relname,match", "PARSABLE": "1"})
	if req := RequestFromOptions(tr.Runtime, tr.Endpoint("a"), tr.Endpoint("b")); req.Written {
		t.Errorf("parsable proplist without sizes should skip written")
	}
	tr = app.NewTest("backup", map[string]string{"PROPLIST": "relname,xfersize", "PARSABLE": "1"})
	if req := RequestFromOptions(tr.Runtime, tr.Endpoint("a"), tr.Endpoint("b")); !req.Written {
		t.Errorf("size columns need written")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Compare(ctx, app.NewTest("match", nil).Runtime, Request{}); err == nil {
		t.Errorf("expected context error")
	}
	if !depthOK("", 1) || depthOK("/a", 1) || !depthOK("/a", 2) || depthOK("/a/b", 2) || !depthOK("/a/b/c", 0) {
		t.Errorf("depthOK")
	}
}

func TestIVSetMatch(t *testing.T) {
	tr := app.NewTest("match", nil)
	cmd := func(ds string) string {
		return "zfs list -Hpr -o name,guid,ivsetguid,written,creation,used -t all -Screatetxg '" + ds + "'"
	}
	tr.Fake.On(cmd("a/e"), execx.Out("a/e@s\t5\t77\t0\t1\t1", "a/e\t1\t-\t0\t1\t1"))
	tr.Fake.On(cmd("b/e"), execx.Out("b/e@s\t5\t77\t0\t1\t1", "b/e\t2\t-\t0\t1\t1"))
	res, _ := Compare(context.Background(), tr.Runtime, Request{Source: tr.Endpoint("a/e"), Target: tr.Endpoint("b/e"), IVSet: true, Written: true})
	if res.Pairs[0].MatchIVSet != "77" {
		t.Errorf("ivset = %+v", res.Pairs[0])
	}
}

func TestFilterUnits(t *testing.T) {
	f := NewFilter("", "")
	if f.Active() || !f.Dataset("p/a", "/a") || !f.Snapshot("@x", "p/a", "/a") {
		t.Errorf("empty filter")
	}
	f = NewFilter("/vm-*,@tmp?", "")
	if !f.Active() || f.Dataset("p/vm-1", "/vm-1") || f.Dataset("p/vm-1/c", "/vm-1/c") || !f.Dataset("p/vm/1", "/vm/1") {
		t.Errorf("glob exclude")
	}
	if f.Snapshot("@tmp1", "p", "") || !f.Snapshot("@tmp12", "p", "") {
		t.Errorf("snapshot glob exclude")
	}
	if re := GlobRegexp("a.b*c?", ""); !re.MatchString("a.bxxcz") || re.MatchString("axbc1") {
		t.Errorf("GlobRegexp escaping")
	}
	f = NewFilter("", "pool/keep")
	if !f.Dataset("pool/keep/child", "/child") || f.Dataset("pool/other", "/other") {
		t.Errorf("absolute include")
	}
}

func TestSendRange(t *testing.T) {
	tr := app.NewTest("backup", nil)
	tr.Fake.On("zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg 'p/ds'",
		execx.Out("p/ds@d\t4\t0\t4\t1", "p/ds#bm\t3\t0\t3\t1", "p/ds@hourly1\t3\t0\t3\t1", "p/ds@b\t2\t0\t2\t1", "p/ds@a\t1\t0\t1\t1", "p/ds\t9\t0\t1\t1"))
	res, _ := Compare(context.Background(), tr.Runtime, Request{Source: tr.Endpoint("p/ds"), Exclude: "@hourly*", Written: true})
	if got := strings.Join(res.Source.SendRange("", "@a"), ","); got != "@b,@d" {
		t.Errorf("SendRange = %q", got)
	}
	if res.Source.SendRange("", "@d") != nil || res.Source.SendRange("", "#bm") != nil || res.Source.SendRange("/x", "@a") != nil {
		t.Errorf("SendRange edge cases")
	}
}
