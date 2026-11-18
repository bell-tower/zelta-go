package backup

import (
	"maps"
	"strings"
	"testing"

	"zelta.space/internal/execx"
)

// rotateFx scripts a target that diverged after @a; after the rename the
// target listing shows the rotated replica.
func rotateFx(t *testing.T, opts map[string]string, srcProps []string, srcList, tgtList, after []string) *fx {
	o := map[string]string{"VERB": "rotate"}
	maps.Copy(o, opts)
	f := newFx(t, "rotate", o)
	f.props("apool/src", srcProps...).props("bpool/tgt", ". type filesystem").list("apool/src", srcList...)
	f.tr.Fake.OnFunc(func(s string) bool { return s == listCmd("", "bpool/tgt", "name,guid", "") }, func(string) execx.Result {
		rows := tgtList
		if len(f.tr.Fake.CallsMatching("zfs rename")) > 0 {
			rows = after
		}
		var lines []string
		for _, r := range rows {
			i := strings.LastIndex(r, " ")
			suffix := strings.TrimPrefix(r[:i], ".")
			lines = append(lines, "bpool/tgt"+suffix+"\t"+r[i+1:])
		}
		return execx.Out(lines...)
	})
	return f
}

func TestRotateFromSource(t *testing.T) {
	f := rotateFx(t, nil, []string{". type filesystem"}, []string{"@b 2", "@a 1", ". 9"}, []string{"@x 5", "@a 1", ". 8"}, []string{"@b 2", "@a 1", ". 7"})
	f.on("zfs rename -fp 'bpool/tgt' 'bpool/tgt_x'", execx.Out())
	f.on("{ zfs send -P -L -c -e -i 'apool/src@a' 'apool/src@b'|zfs recv -v "+fullRecvTop+" -o origin='bpool/tgt_x@a' 'bpool/tgt' ; }", received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v %s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(),
		"rotating from source: apool/src@a",
		"renaming 'bpool/tgt' to 'bpool/tgt_x'",
		"to ensure target is up-to-date, run: zelta backup apool/src bpool/tgt",
		"dataset up-to-date",
		"1K sent, 1 streams received in 0.5 seconds")
}

func TestRotateFromOrigin(t *testing.T) {
	f := rotateFx(t, nil, []string{". type filesystem", ". origin apool/base@o"}, []string{"@n 3", ". 9"}, []string{"@x 5", "@o 1", ". 8"}, []string{"@n 3", ". 7"})
	f.on("zfs list -Ho name 'bpool/tgt@o'", execx.Out("bpool/tgt@o"))
	f.on("zfs rename -fp 'bpool/tgt' 'bpool/tgt_x'", execx.Out())
	f.on("{ zfs send -P -L -c -e -i 'apool/base@o' 'apool/src@n'|zfs recv -v "+fullRecvTop+" -o origin='bpool/tgt_x@o' 'bpool/tgt' ; }", received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v %s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	contains(t, "stdout", f.tr.Out.String(), "rotating from source origin: apool/base", "renaming 'bpool/tgt' to 'bpool/tgt_x'")

	f = rotateFx(t, map[string]string{"DRYRUN": "1"}, []string{". type filesystem", ". origin apool/base@o"}, []string{"@n 3", ". 9"}, []string{"@x 5", ". 8"}, nil)
	f.on("zfs list -Ho name 'bpool/tgt@o'", execx.Fail(1, "cannot open 'bpool/tgt@o': dataset does not exist"))
	f.backup("apool/src", "bpool/tgt")
	contains(t, "stderr", f.tr.Err.String(), "warning: 'bpool/tgt@o': is missing and will fail to clone")
	contains(t, "stdout", f.tr.Out.String(), "would rename 'bpool/tgt' to 'bpool/tgt_x'", "+ zfs rename -fp 'bpool/tgt' 'bpool/tgt_x'",
		"to ensure target is up-to-date, run: zelta backup apool/src bpool/tgt")
}

func TestRotateRefusals(t *testing.T) {
	f := rotateFx(t, map[string]string{"SNAP_MODE": "0"}, []string{". type filesystem"}, []string{"@a 1", ". 9"}, []string{"@a 1", ". 8"}, nil)
	if err := f.backup("apool/src", "bpool/tgt"); err == nil || err.Error() != "replica is up-to-date; source snapshot required for rotation: apool/src" {
		t.Errorf("err = %v", err)
	}
	f = rotateFx(t, nil, []string{". type filesystem"}, []string{"@a 1", ". 9"}, []string{"@z 7", ". 8"}, nil)
	err := f.backup("apool/src", "bpool/tgt")
	if err == nil || err.Error() != "to perform a full backup, rename the target dataset or sync to an empty target" {
		t.Errorf("err = %v", err)
	}
	eqLines(t, "stderr", f.tr.Err.String(), "error: target is not a replica of the source")

	f = rotateFx(t, nil, []string{". type filesystem"}, []string{"@b 2", "@a 1", ". 9"}, []string{"@x 5", "@a 1", ". 8"}, nil)
	f.on("zfs rename -fp 'bpool/tgt' 'bpool/tgt_x'", execx.Fail(1, "cannot rename 'bpool/tgt': dataset is busy"))
	if err := f.backup("apool/src", "bpool/tgt"); err == nil || err.Error() != "cannot rename 'bpool/tgt': dataset is busy" {
		t.Errorf("rename err = %v", err)
	}
	f = rotateFx(t, nil, []string{". type filesystem"}, []string{"@b 2", "@a 1", ". 9"}, []string{"@x 5", "@a 1", ". 8"}, nil)
	f.on("zfs rename -fp 'bpool/tgt' 'bpool/tgt_x'", execx.Fail(1, "odd"))
	if err := f.backup("apool/src", "bpool/tgt"); err == nil || err.Error() != "cannot rename 'bpool/tgt' to 'bpool/tgt_x'" {
		t.Errorf("rename failure = %v", err)
	}
	contains(t, "stderr", f.tr.Err.String(), "unexpected 'zfs rename' output: odd")
}

func TestRotatePartialFullBackup(t *testing.T) {
	f := newFx(t, "rotate", map[string]string{"VERB": "rotate", "DRYRUN": "1"})
	f.props("apool/src", ". type filesystem", "/new type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "/new@b 4", "/new 5", "@b 2", "@a 1", ". 9").list("bpool/tgt", "@x 5", "@a 1", ". 8")
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	f.noUnmatched()
	eqLines(t, "stderr", f.tr.Err.String(), "warning: insufficient snapshots; performing full backup for 1 datasets")
	contains(t, "stdout", f.tr.Out.String(),
		"+ { zfs send -P -L -c -e -i 'apool/src@a' 'apool/src@b'|zfs recv -v "+fullRecvTop+" -o origin='bpool/tgt_x@a' 'bpool/tgt' ; }",
		"+ { zfs send -P -L -c -e 'apool/src/new@b'|zfs recv -v "+fullRecvFS+" 'bpool/tgt/new' ; }")
}

func TestRebase(t *testing.T) {
	f := rotateFx(t, map[string]string{"VERB": "rebase", "DRYRUN": "1"}, []string{". type filesystem"}, []string{"@b 2", "@a 1", ". 9"}, []string{"@x 5", "@a 1", ". 8"}, nil)
	if err := f.run(Rebase, "apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v %s", err, f.tr.Err.String())
	}
	contains(t, "stdout", f.tr.Out.String(), "rebasing bpool/tgt from apool/src", "rotating from source: apool/src@a")

	for _, tt := range []struct {
		opts map[string]string
		args []string
		err  string
	}{
		{nil, []string{"one"}, "zelta rebase requires UPSTREAM and TARGET"},
		{map[string]string{"REBASE_FILE": "x"}, []string{"a", "b"}, "--rebase-file is not supported by this rebase workflow"},
		{nil, []string{"h1:a", "h2:b"}, "rebase requires UPSTREAM and TARGET on the same host"},
		{map[string]string{"USAGE": "1"}, []string{"a", "b"}, ""},
	} {
		f := newFx(t, "rebase", tt.opts)
		err := f.run(Rebase, tt.args...)
		if err == nil || err.Error() != tt.err {
			t.Errorf("rebase %v: err = %v", tt.args, err)
		}
	}

	f = newFx(t, "rebase", nil)
	f.missing("apool/src")
	if err := f.run(Rebase, "apool/src", "bpool/tgt"); err == nil || err.Error() != "rebase failed" {
		t.Errorf("err = %v", err)
	}
	contains(t, "stderr", f.tr.Err.String(), "error: source dataset 'apool/src' does not exist")
}

// After a rotate, datasets that only existed in the renamed target keep the
// summary from claiming the whole tree is up to date, as in Zelta 1.2.
func TestRotateSummaryAfterRename(t *testing.T) {
	f := rotateFx(t, nil, []string{". type filesystem"}, []string{"@b 2", "@a 1", ". 9"},
		[]string{"/kid 6", "@x 5", "@a 1", ". 8"}, []string{"@b 2", "@a 1", ". 7"})
	f.on("zfs rename -fp 'bpool/tgt' 'bpool/tgt_x'", execx.Out())
	f.on("{ zfs send -P -L -c -e -i 'apool/src@a' 'apool/src@b'|zfs recv -v "+fullRecvTop+" -o origin='bpool/tgt_x@a' 'bpool/tgt' ; }", received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	eqLines(t, "stdout", f.tr.Out.String(),
		"renaming 'bpool/tgt' to 'bpool/tgt_x'",
		"to ensure target is up-to-date, run: zelta backup apool/src bpool/tgt",
		"1K sent, 1 streams received in 0.5 seconds")
	eqLines(t, "stderr", f.tr.Err.String(), "warning: insufficient snapshots; performing full backup for 0 datasets")
}
