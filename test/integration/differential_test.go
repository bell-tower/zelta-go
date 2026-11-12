//go:build integration

package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// awkZelta runs the Awk implementation from ZELTA_AWK_ROOT (a checkout of
// github.com/bell-tower/zelta) with the same isolated environment.
func awkZelta(t *testing.T, env []string, args ...string) result {
	t.Helper()
	root := os.Getenv("ZELTA_AWK_ROOT")
	env = append([]string{"ZELTA_SHARE=" + filepath.Join(root, "share/zelta"), "PATH=" + filepath.Join(root, "bin") + ":" + os.Getenv("PATH")}, env...)
	return runProg(t, filepath.Join(root, "bin", "zelta"), env, args...)
}

var (
	timestampSnap  = regexp.MustCompile(`@zelta_\d{4}-\d\d-\d\d_\d\d\.\d\d\.\d\d`)
	seconds        = regexp.MustCompile(`in [0-9.]+ seconds`)
	quotedSnapshot = regexp.MustCompile(`zfs snapshot -r '([^']*)'`)
)

func canonical(s string) string {
	s = timestampSnap.ReplaceAllString(s, "@zelta_TIME")
	s = seconds.ReplaceAllString(s, "in N seconds")
	// zelta-go quotes snapshot names in displayed "zfs snapshot" commands.
	s = quotedSnapshot.ReplaceAllString(s, "zfs snapshot -r $1")
	// The shell logger dropped backslashes; zelta-go keeps them.
	s = strings.ReplaceAll(s, `\ `, " ")
	return strings.Join(normalize(s), "\n")
}

// TestDifferential compares read-only commands between implementations.
func TestDifferential(t *testing.T) {
	if os.Getenv("ZELTA_AWK_ROOT") == "" {
		t.Skip("set ZELTA_AWK_ROOT to a zelta (Awk) checkout to compare implementations")
	}
	src, tgt := tree(t, "diff")
	makeTree(t, src)
	expectOK(t, zelta(t, nil, "backup", "--snap-name", "@start", src, tgt))
	sh(t, "zfs create -u "+src+"/sub1/child && zfs snapshot "+src+"/sub3@two && zfs snapshot -r "+src+"/sub2@two")
	for i := range 5 {
		sh(t, "zfs snapshot -r "+src+"/sub1@p"+string(rune('0'+i)))
	}
	commands := [][]string{
		{"match", src, tgt},
		{"match", "-H", src, tgt},
		{"match", "-o", "all", src, tgt},
		{"match", "-Ho", "relname,match,srcnext,srclast,tgtlast,xfernum,numsnaps", src, tgt},
		{"match", "-d2", "-X", "/sub4,@two", src, tgt},
		{"match", "--include", "/sub3", src, tgt},
		{"match", "-n", src, tgt},
		{"match", src},
		{"prune", "--prune-num=2", "--prune-time=0", src},
		{"prune", "--no-ranges", "--prune-num=1", "--prune-time=0", src, tgt},
		{"prune", "--visual", "--prune-num=1", "--prune-time=0", src},
		{"prune", "--prune-grid=1 week", src},
		{"backup", "-n", src, tgt},
		{"backup", "-n", "-i", "--snapshot", "--snap-name", "@x", src, tgt},
		{"backup", "-n", "-R", src, tgt + "_new"},
		// rotate -n differs by design: see "next snapshot" in doc/COMPATIBILITY.md.
		{"clone", "-n", src, srcPool + "/diff_clone"},
		{"revert", "-n", "-f", src},
		{"lock", "-n", src},
		{"unlock", "-n", src},
		{"failover", "-n", src, tgt},
		{"snapshot", "-n", "--snap-name", "@s", src},
		{"backup", "--bogus", src, tgt},
		{"match", "--depth", "-1", src, tgt},
	}
	for _, args := range commands {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			goRes, awkRes := zelta(t, nil, args...), awkZelta(t, nil, args...)
			if goRes.Code != awkRes.Code {
				t.Errorf("exit code: go=%d awk=%d", goRes.Code, awkRes.Code)
			}
			if g, a := canonical(goRes.Stdout), canonical(awkRes.Stdout); g != a {
				t.Errorf("stdout differs:\n--- go\n%s\n--- awk\n%s", g, a)
			}
			if !strings.HasPrefix(args[len(args)-1], "--") && args[1] != "--bogus" {
				if g, a := canonical(goRes.Stderr), canonical(awkRes.Stderr); g != a {
					t.Logf("stderr differs (informational):\n--- go\n%s\n--- awk\n%s", g, a)
				}
			}
		})
	}
}
