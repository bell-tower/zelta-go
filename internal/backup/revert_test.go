package backup

import (
	"maps"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/execx"
)

const (
	revRoot     = "apool/source"
	revChild    = "apool/source/child with space"
	revTreeList = "zfs list -Hpr -o name -t filesystem,volume,snapshot 'apool/source'"
	revSnapshot = "zfs snapshot -r 'apool/source@zelta_2026-01-02_03.04.05'"
)

// revertFixture mirrors the fake zfs of the 051 revert preflight spec.
func revertFixture(t *testing.T, kase string, opts map[string]string) *fx {
	f := newFx(t, "revert", opts)
	rows := []string{". type filesystem", ". readonly 0", "/child with space type filesystem", "/child with space readonly 0"}
	extra := strings.HasPrefix(kase, "best-effort") || kase == "root-failure"
	if extra {
		for _, d := range []string{"/child with space/dependent", "/child with space2", "/zhealthy"} {
			rows = append(rows, d+" type filesystem", d+" readonly 0")
		}
	}
	depth := ""
	if opts["DEPTH"] != "" {
		depth = "0"
	}
	f.propsOn("", revRoot, depth, rows...)

	names := []string{revRoot, revRoot + "@old", revChild}
	list := []string{"@old 2", ". 1"}
	switch {
	case strings.HasPrefix(kase, "coherent"):
		names = append(names, revChild+"@old")
		list = append(list, "/child with space@old 4")
	case strings.HasPrefix(kase, "no-snapshot"):
	default:
		names = append(names, revChild+"@new")
		list = append(list, "/child with space@new 5")
	}
	list = append(list, "/child with space 3")
	if extra {
		list = append(list, "/child with space/dependent@old 6", "/child with space/dependent 7",
			"/child with space2@old 8", "/child with space2 9", "/zhealthy@old 10", "/zhealthy 11")
	}
	if kase == "list-failure" {
		f.on(revTreeList, execx.Fail(1))
	} else {
		f.on(revTreeList, execx.Out(names...))
	}
	f.listOn("", revRoot, "name,guid", opts["DEPTH"], list...)
	f.on("zfs rename -fp 'apool/source' 'apool/source_old'", execx.Out())
	f.tr.Fake.OnFunc(func(s string) bool { return strings.HasPrefix(s, "zfs clone ") }, func(s string) execx.Result {
		if kase == "root-failure" {
			return execx.Fail(1, "cannot clone root: permission denied")
		}
		if strings.Contains(s, "child with space@old") && (!strings.HasPrefix(kase, "coherent") || strings.Contains(kase, "failclone")) {
			return execx.Fail(1, "cannot open snapshot: dataset does not exist")
		}
		return execx.Out()
	})
	f.on(revSnapshot, execx.Out())
	return f
}

func TestRevertPreflightRejects(t *testing.T) {
	tests := []struct {
		kase string
		opts map[string]string
		ep   string
		err  string
	}{
		{"missing", nil, revRoot + "@old", "error: cannot revert tree: missing snapshot 'apool/source/child with space@old'; revert eligible children individually\n"},
		{"no-snapshot", nil, revRoot + "@old", "error: cannot revert tree: missing snapshot"},
		{"dryrun", map[string]string{"DRYRUN": "1"}, revRoot + "@old", "missing snapshot"},
		{"list-failure", nil, revRoot + "@old", "error: cannot validate revert tree; no datasets renamed\n"},
		{"coherent", map[string]string{"DRYRUN": "1", "DEPTH": "1"}, revRoot + "@old", "unselected dataset 'apool/source/child with space'"},
		{"no-snapshot", nil, revRoot, "error: cannot revert tree: no snapshot for 'apool/source/child with space'"},
		{"coherent", map[string]string{"DRYRUN": "1", "EXCLUDE": "*/child*"}, revRoot + "@old", "unselected dataset"},
	}
	for _, tt := range tests {
		opts := map[string]string{"LOG_LEVEL": "0"}
		maps.Copy(opts, tt.opts)
		f := revertFixture(t, tt.kase, opts)
		err := f.run(Run, tt.ep)
		code := finish(f, err)
		if code == 0 {
			t.Errorf("%s: expected failure", tt.kase)
		}
		contains(t, tt.kase+" stderr", f.tr.Err.String(), tt.err)
		if strings.HasSuffix(tt.err, "\n") && f.tr.Err.String() != tt.err {
			t.Errorf("%s stderr = %q", tt.kase, f.tr.Err.String())
		}
		if m := f.mutations(); len(m) != 0 {
			t.Errorf("%s: mutations before validation: %v", tt.kase, m)
		}
	}
}

func TestRevertCoherent(t *testing.T) {
	f := revertFixture(t, "coherent-real", map[string]string{"LOG_LEVEL": "0"})
	if err := f.run(Run, revRoot+"@old"); err != nil {
		t.Fatalf("err = %v %s", err, f.tr.Err.String())
	}
	eqSlice(t, "mutations", f.mutations(),
		"zfs rename -fp 'apool/source' 'apool/source_old'",
		"zfs clone -p -o readonly=off 'apool/source_old@old' 'apool/source'",
		"zfs clone -p -o readonly=off 'apool/source_old/child with space@old' 'apool/source/child with space'",
		revSnapshot)

	f = revertFixture(t, "coherent", map[string]string{"DRYRUN": "1"})
	if err := f.run(Run, revRoot+"@old"); err != nil {
		t.Fatalf("dry run err = %v", err)
	}
	eqSlice(t, "dry-run mutations", f.mutations())
	eqLines(t, "dry-run stdout", f.tr.Out.String(),
		"would rename 'apool/source' to 'apool/source_old'",
		"+ zfs rename -fp 'apool/source' 'apool/source_old'",
		"+ zfs clone -p -o readonly=off 'apool/source_old@old' 'apool/source'",
		"+ zfs clone -p -o readonly=off 'apool/source_old/child with space@old' 'apool/source/child with space'",
		"would clone 2/2 datasets to apool/source",
		"would snapshot: @zelta_2026-01-02_03.04.05",
		"+ zfs snapshot -r 'apool/source@zelta_2026-01-02_03.04.05'",
		"would recommend: zelta rotate 'apool/source' 'TARGET'")

	f = revertFixture(t, "coherent-real", nil)
	f.run(Run, revRoot+"@old")
	eqLines(t, "stdout", f.tr.Out.String(),
		"renaming 'apool/source' to 'apool/source_old'",
		"cloned 2/2 datasets to apool/source",
		"snapshotting: @zelta_2026-01-02_03.04.05",
		"to retain replica history, run: zelta rotate 'apool/source' 'TARGET'")
}

func TestRevertForce(t *testing.T) {
	// Dry run with --force shows the otherwise blocked plan.
	f := revertFixture(t, "force-long", map[string]string{"DRYRUN": "1", "REVERT_FORCE": "1"})
	if err := f.run(Run, revRoot+"@old"); err != nil {
		t.Fatalf("err = %v", err)
	}
	contains(t, "stdout", f.tr.Out.String(), "'apool/source_old/child with space@old'")
	contains(t, "stderr", f.tr.Err.String(), "forcing best-effort revert")

	// A failed child clone is reported and the result is incomplete.
	f = revertFixture(t, "failure-force", map[string]string{"LOG_LEVEL": "0", "REVERT_FORCE": "1"})
	code := finish(f, f.run(Run, revRoot+"@old"))
	if code != 1 {
		t.Errorf("code = %d", code)
	}
	eqLines(t, "stderr", f.tr.Err.String(),
		"error: error creating clone 'apool/source/child with space'",
		"error: revert incomplete: 1 cloned, 1 failed, 0 skipped; original tree preserved at 'apool/source_old'")

	// Siblings continue but dependents of a failed parent are skipped.
	f = revertFixture(t, "best-effort", map[string]string{"LOG_LEVEL": "0", "REVERT_FORCE": "1"})
	if code := finish(f, f.run(Run, revRoot+"@old")); code != 1 {
		t.Errorf("code = %d", code)
	}
	contains(t, "stderr", f.tr.Err.String(),
		"skipping clone 'apool/source/child with space/dependent': parent 'apool/source/child with space' was not restored",
		"revert incomplete: 3 cloned, 1 failed, 1 skipped; original tree preserved at 'apool/source_old'")
	eqSlice(t, "mutations", f.mutations(),
		"zfs rename -fp 'apool/source' 'apool/source_old'",
		"zfs clone -p -o readonly=off 'apool/source_old@old' 'apool/source'",
		"zfs clone -p -o readonly=off 'apool/source_old/child with space@old' 'apool/source/child with space'",
		"zfs clone -p -o readonly=off 'apool/source_old/child with space2@old' 'apool/source/child with space2'",
		"zfs clone -p -o readonly=off 'apool/source_old/zhealthy@old' 'apool/source/zhealthy'")

	// A failed root clone skips every child instead of creating parents.
	f = revertFixture(t, "root-failure", map[string]string{"LOG_LEVEL": "0", "REVERT_FORCE": "1"})
	finish(f, f.run(Run, revRoot+"@old"))
	contains(t, "stderr", f.tr.Err.String(), "revert incomplete: 0 cloned, 1 failed, 4 skipped")
	eqSlice(t, "mutations", f.mutations(), "zfs rename -fp 'apool/source' 'apool/source_old'", "zfs clone -p -o readonly=off 'apool/source_old@old' 'apool/source'")

	f = revertFixture(t, "no-snapshot", map[string]string{"LOG_LEVEL": "0", "REVERT_FORCE": "1"})
	finish(f, f.run(Run, revRoot+"@old"))
	contains(t, "stderr", f.tr.Err.String(), "skipping clone 'apool/source/child with space': no source snapshot", "revert incomplete: 1 cloned, 0 failed, 1 skipped")

	f = revertFixture(t, "coherent-force", map[string]string{"LOG_LEVEL": "0", "REVERT_FORCE": "1"})
	if err := f.run(Run, revRoot+"@old"); err != nil {
		t.Fatal(err)
	}
	contains(t, "mutations", strings.Join(f.mutations(), "\n"), revSnapshot)

	f = revertFixture(t, "guidance-failure", map[string]string{"REVERT_FORCE": "1"})
	finish(f, f.run(Run, revRoot+"@old"))
	contains(t, "stdout", f.tr.Out.String(), "cloned 1/2 datasets")
	notContains(t, "stdout", f.tr.Out.String(), "to retain replica history")
	contains(t, "stderr", f.tr.Err.String(), "revert incomplete:")

	f = revertFixture(t, "no-snapshot-dryrun", map[string]string{"DRYRUN": "1", "REVERT_FORCE": "1"})
	if code := finish(f, f.run(Run, revRoot+"@old")); code != 1 {
		t.Errorf("code = %d", code)
	}
	notContains(t, "stdout", f.tr.Out.String(), "would recommend:")
	contains(t, "stderr", f.tr.Err.String(), "revert would be incomplete: 1 planned, 1 skipped; original tree would be preserved at 'apool/source_old'")
	eqSlice(t, "mutations", f.mutations())
}

func TestRevertNonForcedCloneFailure(t *testing.T) {
	f := revertFixture(t, "coherent-failclone", nil)
	code := finish(f, f.run(Run, revRoot+"@old"))
	if code != 1 {
		t.Errorf("code = %d", code)
	}
	contains(t, "stderr", f.tr.Err.String(), "warning: unexpected 'zfs clone' output: cannot open snapshot: dataset does not exist", "error: error creating clone 'apool/source/child with space'")
}
