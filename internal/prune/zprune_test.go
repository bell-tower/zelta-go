package prune

import (
	"context"
	"errors"
	"io"
	"maps"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

type fakeTTY struct{ io.Reader }

func (fakeTTY) Write(p []byte) (int, error) { return len(p), nil }
func (fakeTTY) Close() error                { return nil }

const (
	guardGroup  = "apool/treetop@autosnap_old2,autosnap_old0%autosnap_old1"
	sanoidGroup = "apool/treetop@autosnap_new,autosnap_old2,autosnap_old0%autosnap_old1"
)

// zpruneTest prepares a zprune run over the prune-name fixture.
func zpruneTest(opts map[string]string, answer string) *app.TestRuntime {
	tr := app.NewTest("prune", opts)
	fakeZFS(tr, nameFixture())
	tr.Fake.On("zfs list -H -r -t snapshot -o name 'apool/treetop'", execx.Out(strings.Split(strings.Repeat("x\n", 11), "\n")[:11]...))
	tr.Fake.On("zfs list -H -r -p -t snapshot -o written 'apool/treetop'", execx.Out("4096", "6144", "junk"))
	tr.TTY = func() (io.ReadWriteCloser, error) { return fakeTTY{strings.NewReader(answer)}, nil }
	return tr
}

func guardOpts(extra map[string]string) map[string]string {
	o := map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "sanoid", "MATCH_ENDPOINT": "bpool/backups"}
	maps.Copy(o, extra)
	return o
}

func preview() execx.Result {
	return execx.Out("destroy\tapool/treetop@autosnap_old0", "destroy\tapool/treetop@autosnap_old1", "destroy\tapool/treetop@autosnap_old2", "reclaim\t3072")
}

// TestZpruneQuietForce mirrors the zprune case of 072_prune_name_spec.sh.
func TestZpruneQuietForce(t *testing.T) {
	tr := zpruneTest(map[string]string{"LOG_LEVEL": "0", "PRUNE_FORCE": "1", "PRUNE_GUARD": "none", "PRUNE_NUM": "0", "PRUNE_NAME": "sanoid"}, "")
	tr.Fake.On("zfs destroy '"+sanoidGroup+"'", execx.Out(sanoidGroup))
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil {
		t.Fatal(err)
	}
	if tr.RawOut.String() != sanoidGroup+"\n" || tr.RawErr.Len() != 0 {
		t.Errorf("out=%q err=%q", tr.RawOut.String(), tr.RawErr.String())
	}
	if len(tr.Fake.CallsMatching("-nvp")) != 0 {
		t.Errorf("-qq must skip the preview")
	}
}

func TestZpruneInteractive(t *testing.T) {
	tr := zpruneTest(guardOpts(nil), "y\n")
	tr.Fake.On("zfs destroy -nvp '"+guardGroup+"'", preview())
	tr.Fake.On("zfs destroy '"+guardGroup+"'", execx.Out())
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil {
		t.Fatal(err)
	}
	want := `Preparing to prune "apool/treetop" using the following commands:
zfs destroy ` + guardGroup + `

Calculating snapshots and space to be freed...

3 snapshots (27% of 11) will be destroyed
3.0K total reclaimed (30% of 10.0K)
Destroy now? [y/N] `
	if tr.RawOut.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", tr.RawOut.String(), want)
	}
	if len(tr.Fake.CallsMatching("zfs destroy '")) != 1 {
		t.Errorf("destroy not run: %v", tr.Fake.Calls)
	}
}

func TestZpruneAbortAndDryRun(t *testing.T) {
	for _, answer := range []string{"n\n", "", "yes please\n"} {
		tr := zpruneTest(guardOpts(nil), answer)
		tr.Fake.On("zfs destroy -nvp '"+guardGroup+"'", preview())
		err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"})
		if app.Code(err) != 1 || !strings.HasSuffix(tr.RawOut.String(), "Destroy now? [y/N] Aborted.\n") {
			t.Errorf("answer %q: err=%v out=%q", answer, err, tr.RawOut.String())
		}
		if len(tr.Fake.CallsMatching("zfs destroy '")) != 0 {
			t.Errorf("aborted prune destroyed snapshots")
		}
	}
	for _, yes := range []string{"Y", "YES"} {
		tr := zpruneTest(guardOpts(nil), yes+"\r\n")
		tr.Fake.On("zfs destroy -nvp '"+guardGroup+"'", preview())
		tr.Fake.On("zfs destroy '"+guardGroup+"'", execx.Out())
		if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil {
			t.Errorf("answer %q: %v", yes, err)
		}
	}

	tr := zpruneTest(guardOpts(map[string]string{"DRYRUN": "1", "PRUNE_GUARD": ""}), "")
	tr.Opts.Unset("MATCH_ENDPOINT")
	tr.Fake.On("zfs destroy -nvp '"+sanoidGroup+"'", preview())
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tr.RawOut.String(), "Destroy now") || len(tr.Fake.CallsMatching("zfs destroy '")) != 0 {
		t.Errorf("dry run prompted or destroyed: %q", tr.RawOut.String())
	}
	if !strings.Contains(tr.RawOut.String(), "will be destroyed") || len(tr.Fake.CallsMatching("-Screatetxg")) != 1 {
		t.Errorf("dry run must still select and preview: %v", tr.Fake.Calls)
	}

	tr = zpruneTest(guardOpts(map[string]string{"DRYRUN": "1", "LOG_LEVEL": "0"}), "")
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil || tr.RawOut.Len() != 0 || len(tr.Fake.CallsMatching("destroy")) != 0 {
		t.Errorf("-nqq: %v %q %v", err, tr.RawOut.String(), tr.Fake.Calls)
	}
}

func TestZpruneGuardRequired(t *testing.T) {
	tr := zpruneTest(map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "sanoid", "PRUNE_FORCE": "1", "LOG_LEVEL": "1"}, "")
	tr.Fake.On("zfs destroy -nvp '"+sanoidGroup+"'", preview())
	err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"})
	want := "zprune: match endpoint required to destroy snapshots with prune guard enabled;\n  use --match-endpoint=GUARD or --no-prune-guard\n"
	if app.Code(err) != 1 || tr.RawErr.String() != want {
		t.Errorf("err=%v stderr=%q", err, tr.RawErr.String())
	}
	if len(tr.Fake.CallsMatching("-nvp")) != 0 {
		t.Errorf("-qf skips the preview")
	}

	tr = zpruneTest(map[string]string{"PRUNE_NUM": "0", "PRUNE_NAME": "sanoid", "PRUNE_FORCE": "1", "LOG_LEVEL": "0"}, "")
	tr.Fake.On("zfs destroy 'apool/treetop@autosnap_old2,autosnap_old0%autosnap_old1'", execx.Out())
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop", "bpool/backups"}); err != nil {
		t.Errorf("positional guard satisfies the requirement: %v %q", err, tr.RawErr.String())
	}
	for v, want := range map[string]bool{"": false, "none": true, "0": true, "no": true, "latest": false, "bad": false} {
		if guardDisabled(v) != want {
			t.Errorf("guardDisabled(%q)", v)
		}
	}
}

func TestZpruneQuietPreview(t *testing.T) {
	tr := zpruneTest(guardOpts(map[string]string{"LOG_LEVEL": "1"}), "y\n")
	tr.Fake.On("zfs destroy -nvp '"+guardGroup+"'", preview())
	tr.Fake.On("zfs destroy '"+guardGroup+"'", execx.Out())
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil {
		t.Fatal(err)
	}
	want := "3 snapshots (27% of 11) will be destroyed\n3.0K total reclaimed (30% of 10.0K)\nDestroy now? [y/N] "
	if tr.RawOut.String() != want {
		t.Errorf("-q output = %q", tr.RawOut.String())
	}
}

func TestZpruneRemote(t *testing.T) {
	tr := app.NewTest("prune", guardOpts(map[string]string{"LOG_LEVEL": "4", "PRUNE_FORCE": "1"}))
	lists := nameFixture()
	fakeZFS(tr, lists)
	tr.Fake.On(`ssh me@host "zfs destroy -nvp 'apool/treetop@autosnap_old2,autosnap_old0%autosnap_old1'"`, preview())
	tr.Fake.On(`ssh me@host "zfs destroy 'apool/treetop@autosnap_old2,autosnap_old0%autosnap_old1'"`, execx.Out().WithStderr("note"))
	tr.Fake.On(`ssh me@host "zfs list -H -r -t snapshot -o name 'apool/treetop'"`, execx.Out("a", "b"))
	tr.Fake.On(`ssh me@host "zfs list -H -r -p -t snapshot -o written 'apool/treetop'"`, execx.Out("0"))
	if err := Zprune(context.Background(), tr.Runtime, []string{"me@host:apool/treetop"}); err != nil {
		t.Fatalf("%v\n%v", err, tr.Fake.Unmatched)
	}
	out := tr.RawOut.String()
	if !strings.Contains(out, `ssh me@host "zfs destroy 'apool/treetop@autosnap_old2,autosnap_old0%autosnap_old1'"`+"\n") {
		t.Errorf("remote command display: %q", out)
	}
	if !strings.Contains(out, "3 snapshots (150% of 2) will be destroyed\n3.0K total reclaimed (0% of 0B)\n") {
		t.Errorf("remote summary: %q", out)
	}
	if tr.RawErr.String() != "debug: `ssh me@host \"zfs destroy 'apool/treetop@autosnap_old2,autosnap_old0%autosnap_old1'\"`\nnote\n" {
		t.Errorf("stderr = %q", tr.RawErr.String())
	}
	tr.Opts.Set("REMOTE_COMMAND", "", 0)
	z := &zprune{rt: tr.Runtime}
	if z.remoteCommand() != "ssh" {
		t.Errorf("remote command default")
	}
}

func TestZpruneFailures(t *testing.T) {
	tr := zpruneTest(guardOpts(nil), "y\n")
	tr.Fake.On("zfs destroy -nvp '"+guardGroup+"'", execx.Fail(1, "cannot destroy snapshot: dataset is busy"))
	err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"})
	if app.Code(err) != 1 {
		t.Errorf("preview failure err = %v", err)
	}
	if !strings.HasSuffix(tr.RawOut.String(), "Calculating snapshots and space to be freed...\n"+guardGroup+"\n") {
		t.Errorf("preview output shown on failure: %q", tr.RawOut.String())
	}
	if tr.RawErr.String() != "cannot destroy snapshot: dataset is busy\nzprune: destroy command failed: "+guardGroup+"\nzprune: destroy aborted\n" {
		t.Errorf("stderr = %q", tr.RawErr.String())
	}

	tr = zpruneTest(guardOpts(map[string]string{"PRUNE_FORCE": "1", "LOG_LEVEL": "1"}), "")
	tr.Fake.On("zfs destroy '"+guardGroup+"'", execx.Fail(1))
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); app.Code(err) != 1 || tr.RawErr.String() != "zprune: destroy command failed: "+guardGroup+"\n" {
		t.Errorf("destroy failure: %v %q", err, tr.RawErr.String())
	}

	tr = zpruneTest(guardOpts(nil), "")
	tr.TTY = func() (io.ReadWriteCloser, error) { return nil, errors.New("no tty") }
	tr.Fake.On("zfs destroy -nvp '"+guardGroup+"'", preview())
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); app.Code(err) != 1 {
		t.Errorf("missing tty must abort: %v", err)
	}

	tr = zpruneTest(map[string]string{"PRUNE_NUM": "x"}, "")
	var ue *app.UsageError
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); !errors.As(err, &ue) {
		t.Errorf("selection error must propagate: %v", err)
	}
	tr = zpruneTest(nil, "")
	if err := Zprune(context.Background(), tr.Runtime, nil); !errors.As(err, &ue) || tr.RawErr.String() != "zprune: endpoint required\n" {
		t.Errorf("endpoint required: %v %q", err, tr.RawErr.String())
	}
}

func TestZpruneNothingToDo(t *testing.T) {
	tr := zpruneTest(map[string]string{"PRUNE_NUM": "100"}, "")
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil || tr.RawOut.String() != "No snapshots to prune.\n" {
		t.Errorf("%v %q", err, tr.RawOut.String())
	}
	tr = zpruneTest(map[string]string{"PRUNE_NUM": "100", "LOG_LEVEL": "1"}, "")
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/treetop"}); err != nil || tr.RawOut.Len() != 0 {
		t.Errorf("quiet: %v %q", err, tr.RawOut.String())
	}
	tr = zpruneTest(map[string]string{"PRUNE_NUM": "1"}, "")
	if err := Zprune(context.Background(), tr.Runtime, []string{"apool/missing"}); err != nil || tr.Err.String() != "error: datasets inaccessible or do not exist\n" {
		t.Errorf("missing: %v %q", err, tr.Err.String())
	}
}

func TestZpruneVisualAndMultiDataset(t *testing.T) {
	now := app.TestNow.Unix()
	lists := map[string][]row{"p/ds": {
		{name: "p/ds", guid: "1"},
		{name: "p/ds/child", guid: "2"},
		{name: "p/ds@b", guid: "11", creation: now - 100},
		{name: "p/ds@a", guid: "10", creation: now - 200},
		{name: "p/ds/child@b", guid: "21", creation: now - 100},
		{name: "p/ds/child@a", guid: "20", creation: now - 200},
	}}
	tr := app.NewTest("prune", map[string]string{"PRUNE_SIZE": "1K", "PRUNE_GUARD": "none", "PRUNE_VISUAL": "1", "LOG_LEVEL": "4"})
	fakeZFS(tr, lists)
	tr.TTY = func() (io.ReadWriteCloser, error) { return fakeTTY{strings.NewReader("y\n")}, nil }
	tr.Fake.On("zfs destroy -nvp 'p/ds@a%b'", execx.Out("would destroy p/ds@a", "would destroy p/ds@b", "would reclaim 1024"))
	tr.Fake.On("zfs destroy -nvp 'p/ds/child@a%b'", execx.Out("would destroy p/ds/child@a", "would destroy p/ds/child@b", "would reclaim 512"))
	tr.Fake.On("zfs destroy 'p/ds@a%b'", execx.Out())
	tr.Fake.On("zfs destroy 'p/ds/child@a%b'", execx.Out())
	tr.Fake.OnPrefix("zfs list -H -r -t snapshot", execx.Out("1", "2", "3", "4"))
	tr.Fake.OnPrefix("zfs list -H -r -p", execx.Out("1024", "1024"))
	if err := Zprune(context.Background(), tr.Runtime, []string{"p/ds"}); err != nil {
		t.Fatal(err)
	}
	want := "p/ds\n❌❌\np/ds/child\n❌❌\n" +
		"Preparing to prune \"p/ds\" using the following commands:\nzfs destroy p/ds@a%b\nzfs destroy p/ds/child@a%b\n\n" +
		"Calculating snapshots and space to be freed...\n\n" +
		"Estimating 1K per dataset savings on 2 datasets.\n\n" +
		"p/ds: 2 snapshots, 1.0K\np/ds/child: 2 snapshots, 512B\n" +
		"4 snapshots (100% of 4) will be destroyed\n1.5K total reclaimed (75% of 2.0K)\nDestroy now? [y/N] "
	if tr.RawOut.String() != want {
		t.Errorf("output:\n%s\nwant:\n%s", tr.RawOut.String(), want)
	}
	if tr.RawErr.String() != "debug: `zfs destroy p/ds@a%b`\ndebug: `zfs destroy p/ds/child@a%b`\n" {
		t.Errorf("debug = %q", tr.RawErr.String())
	}
}

func TestZpruneHelpers(t *testing.T) {
	if got := group([]string{"p/a@1", "p/a@2%3", "p/b@x", "junk", "p/a@4"}); strings.Join(got, "|") != "p/a@1,2%3|p/b@x|p/a@4" {
		t.Errorf("group = %v", got)
	}
	if group(nil) != nil {
		t.Errorf("empty group")
	}
	if got := invalidCandidates([]string{"p/a@1", "p/it's@2", "nosnap", ""}); strings.Join(got, "|") != "p/it's@2|nosnap" {
		t.Errorf("invalid = %v", got)
	}
	if percent(1, 0) != 0 || percent(1, 3) != 33 || percent(2, 3) != 67 {
		t.Errorf("percent")
	}
	p := parsePreview("destroy\tp/a@1\nreclaim\t10\nnoise\nwould destroy p/b@2\nwould reclaim 5\n")
	if p.destroyed != 2 || p.reclaim != 15 || p.bytes["p/a"] != 10 || p.bytes["p/b"] != 5 || len(p.datasets) != 2 {
		t.Errorf("preview = %+v", p)
	}
	if p := parsePreview("reclaim\t7\n"); p.reclaim != 7 || len(p.bytes) != 0 {
		t.Errorf("orphan reclaim = %+v", p)
	}
}

func TestZpruneRefusesUnexpectedCandidates(t *testing.T) {
	tr := app.NewTest("prune", map[string]string{"PRUNE_NUM": "0", "PRUNE_GUARD": "none", "PRUNE_FORCE": "1"})
	tr.Fake.On("zfs list -Hpr -o name,guid,written,creation,used,referenced,clones -t all -Screatetxg 'p/x'",
		execx.Out("p/x\t1\t0\t1\t0\t0\t-", "p/x@it's\t2\t0\t1\t0\t0\t-"))
	err := Zprune(context.Background(), tr.Runtime, []string{"p/x"})
	if app.Code(err) != 1 || tr.RawErr.String() != "zprune: refusing unexpected prune output:\n\np/x@it's\nzprune: destroy aborted\n" {
		t.Errorf("err=%v stderr=%q", err, tr.RawErr.String())
	}
}

func TestDestroy(t *testing.T) {
	ctx := context.Background()
	plan := func(src, guard string) *Plan {
		tr := app.NewTest("prune", nil)
		p := &Plan{Source: tr.Endpoint(src), Datasets: []*Dataset{{Name: tr.Endpoint(src).DS,
			prune: []entry{{"@c", 1}, {"@b", 2}}, kept: []entry{{"@a", 3}}}}}
		if guard != "" {
			p.Guard = tr.Endpoint(guard)
		}
		return p
	}
	if d := plan("apool/x", "").Datasets[0]; strings.Join(d.Selected(), ",") != "@b,@c" || strings.Join(d.Protected(), ",") != "@a" {
		t.Errorf("selected %q protected %q", d.Selected(), d.Protected())
	}

	tr := app.NewTest("prune", nil)
	if _, err := Destroy(ctx, tr.Runtime, plan("apool/x", "")); err == nil || !strings.Contains(err.Error(), "match endpoint required") {
		t.Errorf("unguarded: %v", err)
	}

	tr = app.NewTest("prune", map[string]string{"PRUNE_GUARD": "none"})
	tr.Fake.On("zfs destroy 'apool/x@b%c'", execx.Out("destroyed"))
	done, err := Destroy(ctx, tr.Runtime, plan("apool/x", ""))
	if err != nil || strings.Join(done, ",") != "apool/x@b%c" || tr.Out.String() != "destroyed 'apool/x@b%c'\n" {
		t.Errorf("destroy: %q %v out=%q", done, err, tr.Out.String())
	}

	tr = app.NewTest("prune", map[string]string{"DRYRUN": "1"})
	done, err = Destroy(ctx, tr.Runtime, plan("h:apool/x", "bpool/y"))
	if err != nil || len(done) != 0 || len(tr.Fake.Calls) != 0 || tr.Out.String() != "+ ssh h \"zfs destroy 'apool/x@b%c'\"\n" {
		t.Errorf("dry run: %q %v out=%q calls=%q", done, err, tr.Out.String(), tr.Fake.Calls)
	}

	tr = app.NewTest("prune", nil)
	tr.Fake.On("zfs destroy 'apool/x@b%c'", execx.Fail(1, "cannot destroy: busy"))
	done, err = Destroy(ctx, tr.Runtime, plan("apool/x", "bpool/y"))
	if app.Code(err) != 1 || len(done) != 0 || tr.Err.String() != "warning: cannot destroy: busy\nerror: destroy command failed: apool/x@b%c\n" {
		t.Errorf("failure: %q %v err=%q", done, err, tr.Err.String())
	}

	p := plan("apool/it's", "bpool/y")
	if _, err := Destroy(ctx, app.NewTest("prune", nil).Runtime, p); err == nil || !strings.Contains(err.Error(), "refusing unexpected prune output") {
		t.Errorf("quote: %v", err)
	}
}
