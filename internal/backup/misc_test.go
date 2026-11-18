package backup

import (
	"context"
	"errors"
	"strings"
	"testing"

	"zelta.space/internal/app"
	"zelta.space/internal/execx"
)

func TestHelpers(t *testing.T) {
	if src.String() != "SRC" || tgt.String() != "TGT" {
		t.Errorf("side names")
	}
	var d *dataset
	if d.prop("x") != "" {
		t.Errorf("nil dataset prop")
	}
	if parentOf("pool") != "pool" || parentOf("pool/a/b") != "pool/a" {
		t.Errorf("parentOf")
	}
	if p, ok := parentRestored("/a/b", map[string]bool{"": true, "/a": true}); !ok || p != "" {
		t.Errorf("parentRestored = %q %v", p, ok)
	}
	if quoteName(true, "a b") != `'a\ b'` || quoteName(false, "a b") != "'a b'" {
		t.Errorf("quoteName")
	}
	if splitList("") != nil || len(splitList("a,b")) != 2 {
		t.Errorf("splitList")
	}
	r := &run{}
	r.addFailedProp("a")
	r.addFailedProp("a")
	r.addFailedProp("b")
	if strings.Join(r.sum.failedProps, ",") != "a,b" {
		t.Errorf("failedProps = %v", r.sum.failedProps)
	}
}

func TestInnerError(t *testing.T) {
	for err, want := range map[error]string{
		app.Fail("boom"):         "error: boom\n",
		app.Usage("bad usage"):   "error: bad usage\n",
		errors.New("plain"):      "error: plain\n",
		&app.ExitError{Code: 2}:  "",
		&app.UsageError{Msg: ""}: "",
		error(nil):               "",
	} {
		tr := app.NewTest("backup", nil)
		innerError(tr.Runtime, err)
		if tr.Err.String() != want {
			t.Errorf("innerError(%v) = %q want %q", err, tr.Err.String(), want)
		}
	}
}

func TestCloneShuffleFailures(t *testing.T) {
	// A dry run cannot plan the backup of a clone that does not exist yet.
	f := newFx(t, "clone", map[string]string{"VERB": "clone", "DRYRUN": "1"})
	f.props("apool/src", ". type filesystem").missing("apool/clone").list("apool/src", "@a 1", ". 9")
	err := f.backup("apool/src", "apool/clone", "bpool/src", "bpool/clone")
	if err == nil || err.Error() != "source dataset 'apool/clone' does not exist" {
		t.Errorf("err = %v", err)
	}
	contains(t, "stdout", f.tr.Out.String(), "+ zfs clone -p -o readonly=off 'apool/src@a' 'apool/clone'")

	// Clone failures stop before the backup.
	f = newFx(t, "clone", map[string]string{"VERB": "clone"})
	f.props("apool/src", ". type filesystem").missing("apool/clone").list("apool/src", "@a 1", ". 9").
		on("zfs clone -p -o readonly=off 'apool/src@a' 'apool/clone'", execx.Fail(1))
	if err := f.backup("apool/src", "apool/clone", "bpool/src", "bpool/clone"); err == nil || err.Error() != "error creating clone 'apool/clone'" {
		t.Errorf("err = %v", err)
	}

	// The backup target must not be a pool mismatch for the clone step.
	f = newFx(t, "clone", map[string]string{"VERB": "clone"})
	f.props("apool/src", ". type filesystem").list("apool/src", "@a 1", ". 9").
		on("zfs clone -p -o readonly=off 'apool/src@a' 'apool/clone'", execx.Out()).
		props("bpool/clone", ". type filesystem")
	f.tr.Fake.Once(getCmd("", "apool/clone", ""), execx.Fail(1, "cannot open 'apool/clone': dataset does not exist"))
	f.props("apool/clone", ". type filesystem", ". origin apool/src@a")
	f.on("zfs snapshot -r 'apool/clone"+defaultSnap+"'", execx.Fail(1, "permission denied")).list("apool/clone", ". 20").list("bpool/clone", ". 30")
	err = f.backup("apool/src", "apool/clone", "bpool/src", "bpool/clone")
	if err == nil || !strings.Contains(err.Error(), "has no snapshots") {
		t.Errorf("err = %v", err)
	}
}

func TestSendCheckConnectionWarning(t *testing.T) {
	f := incrementalOn(t, "", "", map[string]string{"SEND_CHECK": "1", "DRYRUN": "0"})
	f.on("zfs send -nv "+incr, execx.Out("ssh: connect to host x: Connection refused"))
	f.on("{ zfs send -P "+incr+"|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1, 1))
	f.backup("apool/src", "bpool/tgt")
	contains(t, "stderr", f.tr.Err.String(), "warning: ssh: connect to host x: Connection refused")

	// Send checking is skipped with an explicit override.
	f = incrementalOn(t, "", "", map[string]string{"SEND_CHECK": "1", "SEND_OVERRIDE": "-w"})
	f.on("{ zfs send -P -w -I 'apool/src@a' 'apool/src@b'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1, 1))
	f.backup("apool/src", "bpool/tgt")
	f.noUnmatched()
	if len(f.tr.Fake.CallsMatching("send -nv")) != 0 {
		t.Errorf("send check ran with an override")
	}
}

func TestListSourceTreeFallback(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"EXCLUDE": "@h*", "DRYRUN": "1"})
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("bpool/tgt", "@a 1", ". 8")
	calls := 0
	f.tr.Fake.OnFunc(func(s string) bool { return s == listCmd("", "apool/src", "name,guid", "") }, func(string) execx.Result {
		calls++
		if calls > 1 {
			return execx.Fail(1, "garbage")
		}
		return execx.Out("apool/src@b\t2", "apool/src@a\t1", "apool/src\t9")
	})
	f.backup("apool/src", "bpool/tgt")
	// The failed re-list yields an empty tree, so no steps are planned.
	notContains(t, "stdout", f.tr.Out.String(), "zfs send")
}

func TestTrackBookmarkWithoutSnapshot(t *testing.T) {
	tr := app.NewTest("backup", map[string]string{"BOOKMARK_MODE": "1"})
	r := newRun(context.TODO(), tr.Runtime, nil)
	r.resetState()
	r.trackBookmark("")
	if len(r.marks) != 0 {
		t.Errorf("bookmark tracked without a snapshot")
	}
}
