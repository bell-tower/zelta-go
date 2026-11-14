//go:build integration

package integration

import (
	"strconv"
	"strings"
	"testing"
)

// churn writes size MiB of incompressible data to file in fs's mountpoint.
func churn(t *testing.T, fs, file string, size int) {
	t.Helper()
	sh(t, "dd if=/dev/urandom of=\"$(zfs get -Ho value mountpoint "+fs+")/"+file+"\" bs=64k count="+strconv.Itoa(16*size)+" 2>/dev/null")
}

func rm(t *testing.T, fs, file string) {
	t.Helper()
	sh(t, "rm \"$(zfs get -Ho value mountpoint "+fs+")/"+file+"\"")
}

// reclaim is what "zfs destroy -nvp" reports for snapshots of ds.
func reclaim(t *testing.T, ds string, snaps []string) int64 {
	t.Helper()
	out := sh(t, "zfs destroy -nvp "+ds+"@"+strings.Join(snaps, ","))
	for l := range strings.SplitSeq(out, "\n") {
		if f := strings.Fields(l); len(f) == 2 && f[0] == "reclaim" {
			n, err := strconv.ParseInt(f[1], 10, 64)
			if err == nil {
				return n
			}
		}
	}
	t.Fatalf("no reclaim in %q", out)
	return 0
}

// pruneSize returns the snapshot names selected by --prune-size.
func pruneSize(t *testing.T, ds string, size int64) []string {
	t.Helper()
	r := zelta(t, nil, "prune", "--no-ranges", "--prune-size="+strconv.FormatInt(size, 10), ds)
	expectOK(t, r)
	var snaps []string
	for l := range strings.SplitSeq(strings.TrimSpace(r.Stdout), "\n") {
		if l != "" {
			snaps = append(snaps, strings.TrimPrefix(l, ds+"@"))
		}
	}
	return snaps
}

// checkPruneSize checks that each selection frees at least the requested
// size and that leaving out its newest snapshot would not.
func checkPruneSize(t *testing.T, ds string, all []string) {
	t.Helper()
	total := reclaim(t, ds, all)
	for _, size := range []int64{1, total / 3, total / 2, total} {
		sel := pruneSize(t, ds, size)
		if len(sel) == 0 {
			t.Fatalf("%s --prune-size=%d selected nothing", ds, size)
		}
		if got := reclaim(t, ds, sel); got < size {
			t.Errorf("%s --prune-size=%d: %v frees %d", ds, size, sel, got)
		}
		if len(sel) > 1 {
			if got := reclaim(t, ds, sel[:len(sel)-1]); got >= size {
				t.Errorf("%s --prune-size=%d: %v already frees %d", ds, size, sel[:len(sel)-1], got)
			}
		}
	}
}

func TestPruneSize(t *testing.T) {
	src, _ := tree(t, "prunesize")
	fs, clone := src+"/fs", src+"/clone"
	sh(t, "zfs create "+fs)
	churn(t, fs, "a", 2)
	sh(t, "zfs snapshot "+fs+"@k0")
	// The clone keeps @k0, so later runs start after a kept snapshot.
	sh(t, "zfs clone "+fs+"@k0 "+clone)
	churn(t, fs, "b", 1)
	sh(t, "zfs snapshot "+fs+"@s1")
	churn(t, fs, "c", 1)
	sh(t, "zfs snapshot "+fs+"@s2")
	rm(t, fs, "a")
	rm(t, fs, "b")
	churn(t, fs, "d", 1)
	sh(t, "zfs snapshot "+fs+"@s3")
	rm(t, fs, "c")
	sh(t, "zfs snapshot "+fs+"@s4")
	rm(t, fs, "d")
	churn(t, fs, "e", 1)
	sh(t, "zfs snapshot "+fs+"@s5")

	// A clone's own snapshots also reference its origin's data.
	churn(t, clone, "x", 1)
	sh(t, "zfs snapshot "+clone+"@c1")
	rm(t, clone, "a")
	churn(t, clone, "y", 1)
	sh(t, "zfs snapshot "+clone+"@c2")
	rm(t, clone, "x")
	sh(t, "zfs snapshot "+clone+"@c3")
	rm(t, clone, "y")
	sh(t, "zfs snapshot "+clone+"@c4")
	sh(t, "zpool sync "+srcPool)

	checkPruneSize(t, fs, []string{"s1", "s2", "s3", "s4", "s5"})
	checkPruneSize(t, clone, []string{"c1", "c2", "c3", "c4"})

	// However large the target, a clone origin is never selected.
	if sel := pruneSize(t, fs, 1<<40); strings.Join(sel, ",") != "s1,s2,s3,s4,s5" {
		t.Errorf("--prune-size=1T selected %v", sel)
	}
}
