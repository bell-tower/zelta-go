package backup

import (
	"strings"
	"testing"

	"zelta.space/internal/app"
)

// namedSnapshot scripts a source with @one, @two, and @three and a target
// that already has @one and @two (bell-tower/zelta#84).
func namedSnapshot(t *testing.T, verb string, opts map[string]string) *fx {
	f := newFx(t, verb, opts)
	return f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "@three 3", "@two 2", "@one 1", ". 9").
		list("bpool/tgt", "@two 2", "@one 1", ". 8")
}

func TestNamedSnapshotAlreadyOnTarget(t *testing.T) {
	f := namedSnapshot(t, "backup", nil)
	if err := f.backup("apool/src@two", "bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stdout", f.tr.Out.String(), "dataset up-to-date")
	if sends := f.tr.Fake.CallsMatching("zfs send"); len(sends) != 0 {
		t.Errorf("nothing should be sent: %v", sends)
	}
	// Without a named snapshot the target is behind and syncs to @three.
	f = namedSnapshot(t, "backup", map[string]string{"DRYRUN": "1"})
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.tr.Out.String(), "-I 'apool/src@two' 'apool/src@three'") {
		t.Errorf("stdout = %q", f.tr.Out.String())
	}
}

func TestNamedSnapshotRotateStopsBeforeRenaming(t *testing.T) {
	f := namedSnapshot(t, "rotate", map[string]string{"SNAP_MODE": "0"})
	err := f.backup("apool/src@two", "bpool/tgt")
	if app.Code(err) != 1 || !strings.Contains(err.Error(), "replica is up-to-date") {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	if renames := f.tr.Fake.CallsMatching("zfs rename"); len(renames) != 0 {
		t.Errorf("target must not be renamed: %v", renames)
	}
}

func TestNamedSnapshotFilteredStepwiseStops(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"EXCLUDE": "@h*"})
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "@three 4", "@two 3", "@h1 2", "@one 1", ". 9").
		list("bpool/tgt", "@one 1", ". 8").
		on("{ zfs send -P -L -c -e -i 'apool/src@one' 'apool/src@two'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1024, 1))
	if err := f.backup("apool/src@two", "bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	if sends := f.tr.Fake.CallsMatching("zfs send"); len(sends) != 1 {
		t.Errorf("stepwise sync must stop at @two: %v", sends)
	}
}
