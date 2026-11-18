package backup

import (
	"strings"
	"testing"

	"zelta.space/internal/execx"
)

func TestCloneTwoOperands(t *testing.T) {
	f := newFx(t, "clone", map[string]string{"VERB": "clone", "LOG_LEVEL": "3"})
	f.props("apool/src", ". type filesystem", "/c type filesystem").missing("apool/copy").
		list("apool/src", "/c@a 3", "/c 4", "@a 1", ". 9").
		on("zfs clone -p -o readonly=off 'apool/src@a' 'apool/copy'", execx.Out()).
		on("zfs clone -p -o readonly=off 'apool/src/c@a' 'apool/copy/c'", execx.Out("cannot mount 'apool/copy/c': encryption key not loaded for apool/src"))
	if err := f.backup("apool/src", "apool/copy"); err != nil {
		t.Fatalf("%v %s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(), "cloned 2/2 datasets to apool/copy")
	contains(t, "stderr", f.tr.Err.String(), "cloning: 'apool/src@a'", "to mount apool/copy/c load encryption key in apool/src")

	f = newFx(t, "clone", map[string]string{"VERB": "clone", "CLONE_DEFAULT": "-p -o readonly=on"})
	f.props("apool/src", ". type filesystem").missing("apool/copy").list("apool/src", "@a 1", ". 9").
		on("zfs clone -p -o readonly=on 'apool/src@a' 'apool/copy'", execx.Fail(1, "cannot create"))
	if err := f.backup("apool/src", "apool/copy"); err == nil || err.Error() != "error creating clone 'apool/copy'" {
		t.Errorf("err = %v", err)
	}

	f = newFx(t, "clone", map[string]string{"VERB": "clone", "LOG_LEVEL": "3", "DRYRUN": "1"})
	f.propsOn("ssh -n me@h ", "apool/src", "", ". type filesystem").missingOn("ssh -n me@h ", `apool/new\ copy`, "").
		listOn("ssh -n me@h ", "apool/src", "name,guid", "", "@a 1", ". 9")
	f.backup("me@h:apool/src", "me@h:apool/new copy")
	f.noUnmatched()
	contains(t, "remote dry run", f.tr.Out.String(), `+ ssh -n me@h zfs clone -p -o readonly=off 'apool/src@a' 'apool/new\ copy'`, "would clone 1/1 datasets to apool/new copy")
	contains(t, "remote info", f.tr.Err.String(), "would clone: 'apool/src@a'")
}

func TestCloneNoSnapshots(t *testing.T) {
	f := newFx(t, "clone", map[string]string{"VERB": "clone", "SNAP_MODE": "0"})
	f.props("apool/src", ". type filesystem", "/c type filesystem").missing("apool/copy").list("apool/src", "/c 4", "@a 1", ". 9").
		on("zfs clone -p -o readonly=off 'apool/src@a' 'apool/copy'", execx.Out())
	f.backup("apool/src", "apool/copy")
	eqLines(t, "stdout", f.tr.Out.String(), "cloned 1/2 datasets to apool/copy", "no source snapshot: apool/copy/c")
}

func TestCloneFourOperands(t *testing.T) {
	f := newFx(t, "clone", map[string]string{"VERB": "clone", "SEND_INTR": "0"})
	f.props("apool/src", ". type filesystem").list("apool/src", "@a 1", ". 9").
		missing("bpool/clone").
		on("zfs clone -p -o readonly=off 'apool/src@a' 'apool/clone'", execx.Out()).
		on("zfs snapshot -r 'apool/clone"+defaultSnap+"'", execx.Out()).
		list("apool/clone", defaultSnap+" 21", ". 20").
		on("{ zfs send -P -L -c -e -i 'apool/src@a' 'apool/clone"+defaultSnap+"'|zfs recv -v "+fullRecvTop+" -o origin='bpool/src@a' 'bpool/clone' ; }", received("incremental", 100, 1))
	f.tr.Fake.Once(getCmd("", "apool/clone", ""), execx.Fail(1, "cannot open 'apool/clone': dataset does not exist"))
	f.props("apool/clone", ". type filesystem", ". origin apool/src@a")
	if err := f.backup("apool/src", "apool/clone", "bpool/src", "bpool/clone"); err != nil {
		t.Fatalf("%v %s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(),
		"cloned 1/1 datasets to apool/clone",
		"snapshotting: "+defaultSnap,
		"syncing 1 datasets",
		"100B sent, 1 streams received in 0.5 seconds")
}

func TestTargetOrigin(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"ORIGIN_ID": "bpool/base", "DRYRUN": "1"})
	f.props("apool/clone", ". type filesystem", ". origin apool/base@o", "/c type filesystem", "/c origin apool/base/c@o", "/plain type filesystem").
		missing("bpool/clone").list("apool/clone", "/plain@n 7", "/plain 8", "/c@n 5", "/c 6", "@n 3", ". 9")
	if err := f.backup("apool/clone", "bpool/clone"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(),
		"would sync 3 datasets",
		"+ { zfs send -P -L -c -e -I 'apool/base@o' 'apool/clone@n'|zfs recv -v "+fullRecvTop+" -o origin='bpool/base@o' 'bpool/clone' ; }",
		"+ { zfs send -P -L -c -e -I 'apool/base/c@o' 'apool/clone/c@n'|zfs recv -v "+fullRecvFS+" -o origin='bpool/base/c@o' 'bpool/clone/c' ; }",
		"+ { zfs send -P -L -c -e 'apool/clone/plain@n'|zfs recv -v "+fullRecvFS+" 'bpool/clone/plain' ; }")

	f = newFx(t, "backup", map[string]string{"ORIGIN_ID": "bpool/base"})
	f.props("apool/plain", ". type filesystem").missing("bpool/clone").list("apool/plain", "@n 3", ". 9")
	if err := f.backup("apool/plain", "bpool/clone"); err == nil || err.Error() != "source has no clone origin: apool/plain" {
		t.Errorf("err = %v", err)
	}
}

func TestBookmarks(t *testing.T) {
	f := incrementalOn(t, "", "", map[string]string{"BOOKMARK_MODE": "1"})
	f.on("{ zfs send -P "+incr+"|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1, 1)).
		on("zfs list -Ho name 'bpool/tgt@b'", execx.Out("bpool/tgt@b")).
		on("zfs bookmark 'apool/src@b' 'apool/src#testhost_b'", execx.Out())
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	contains(t, "stdout", f.tr.Out.String(), "1 snapshot bookmarked")

	f = incrementalOn(t, "", "", map[string]string{"BOOKMARK_MODE": "1", "BOOKMARK_PREFIX": "bk-", "DRYRUN": "1"})
	f.backup("apool/src", "bpool/tgt")
	contains(t, "dry run", f.tr.Out.String(), "+ zfs list -Ho name 'bpool/tgt@b'", "+ zfs bookmark 'apool/src@b' 'apool/src#bk-b'")
	notContains(t, "dry run", f.tr.Out.String(), "bookmarked")

	for _, tt := range []struct {
		confirm, mark execx.Result
		msg           string
	}{
		{execx.Out(), execx.Out(), "error: cannot confirm replicated snapshot: bpool/tgt@b"},
		{execx.Fail(1, "cannot open"), execx.Out(), "error: cannot confirm replicated snapshot: bpool/tgt@b: cannot open"},
		{execx.Out("bpool/tgt@b"), execx.Fail(1, "permission denied"), "error: cannot bookmark replicated snapshot: apool/src@b: permission denied"},
		{execx.Out("bpool/tgt@b"), execx.Fail(1), "error: cannot bookmark replicated snapshot: apool/src@b"},
	} {
		f = incrementalOn(t, "", "", map[string]string{"BOOKMARK_MODE": "1"})
		f.on("{ zfs send -P "+incr+"|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1, 1)).
			on("zfs list -Ho name 'bpool/tgt@b'", tt.confirm).
			on("zfs bookmark 'apool/src@b' 'apool/src#testhost_b'", tt.mark)
		if code := exitCode(f.backup("apool/src", "bpool/tgt")); code != 1 {
			t.Errorf("code = %d", code)
		}
		contains(t, "stderr", f.tr.Err.String(), tt.msg)
		contains(t, "stdout", f.tr.Out.String(), "0 snapshots bookmarked")
	}
}

func TestFilteredIntermediate(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"EXCLUDE": "@h*"})
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "@d2 6", "@h2 5", "@d1 4", "@h1 3", "@a 1", ". 9").list("bpool/tgt", "@a 1", ". 8").
		on("{ zfs send -P -L -c -e -i 'apool/src@a' 'apool/src@d1'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 10, 1)).
		on("{ zfs send -P -L -c -e -i 'apool/src@d1' 'apool/src@d2'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 10, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v %s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(), "syncing 1 datasets", "dataset up-to-date", "20B sent, 2 streams received in 1 seconds")

	// A new target first gets a full stream of the oldest selected snapshot.
	f = newFx(t, "backup", map[string]string{"INCLUDE": "@d*", "DRYRUN": "1"})
	f.props("apool/src", ". type filesystem").missing("bpool/tgt").list("apool/src", "@d2 6", "@h1 3", "@d1 1", ". 9")
	f.backup("apool/src", "bpool/tgt")
	eqLines(t, "stdout", f.tr.Out.String(), "would sync 1 datasets",
		"+ { zfs send -P -L -c -e 'apool/src@d1'|zfs recv -v "+fullRecvTop+" 'bpool/tgt' ; }",
		"+ { zfs send -P -L -c -e -i 'apool/src@d1' 'apool/src@d2'|zfs recv -v -s 'bpool/tgt' ; }")

	// A failed step stops the dataset.
	f = newFx(t, "backup", map[string]string{"EXCLUDE": "@h*"})
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "@d2 6", "@d1 4", "@a 1", ". 9").list("bpool/tgt", "@a 1", ". 8").
		on("{ zfs send -P -L -c -e -i 'apool/src@a' 'apool/src@d1'|zfs recv -v -s 'bpool/tgt' ; }", execx.Fail(1, "cannot receive incremental stream: x"))
	if exitCode(f.backup("apool/src", "bpool/tgt")) != 2 {
		t.Errorf("expected exit 2")
	}
	if n := len(f.tr.Fake.CallsMatching("zfs send")); n != 1 {
		t.Errorf("sends after failure = %d", n)
	}
	if strings.Count(f.tr.Err.String(), "error:") != 1 {
		t.Errorf("stderr = %s", f.tr.Err.String())
	}
}
