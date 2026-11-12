//go:build integration

package integration

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var (
	zeltaBin string // built binary
	fakeSSH  string // ssh stand-in
	workDir  string
	srcPool  string
	tgtPool  string
	mountDir string
)

func TestMain(m *testing.M) {
	if os.Getenv("ZELTA_INTEGRATION") != "1" {
		fmt.Println("skipping integration tests: set ZELTA_INTEGRATION=1 (requires root and OpenZFS)")
		os.Exit(0)
	}
	if os.Geteuid() != 0 {
		fmt.Println("integration tests require root")
		os.Exit(1)
	}
	code := 1
	func() {
		var err error
		workDir, err = os.MkdirTemp("", "zelta-it-")
		must(err)
		defer os.RemoveAll(workDir)
		zeltaBin = filepath.Join(workDir, "bin", "zelta")
		_, file, _, _ := runtime.Caller(0)
		root := filepath.Join(filepath.Dir(file), "..", "..")
		must(os.MkdirAll(filepath.Dir(zeltaBin), 0o755))
		if bin := os.Getenv("ZELTA_TEST_BINARY"); bin != "" {
			// Test a prebuilt binary (another implementation, a release build).
			data, err := os.ReadFile(bin)
			must(err)
			must(os.WriteFile(zeltaBin, data, 0o755))
		} else {
			// No VCS stamp: git refuses a checkout owned by another user
			// (root in a CI VM), and the version comes from -ldflags anyway.
			build := exec.Command("go", "build", "-buildvcs=false", "-o", zeltaBin, "./cmd/zelta")
			build.Dir = root
			build.Stdout, build.Stderr = os.Stdout, os.Stderr
			must(build.Run())
		}
		must(os.Symlink("zelta", filepath.Join(workDir, "bin", "zprune")))
		fakeSSH = filepath.Join(workDir, "bin", "fakessh")
		must(os.WriteFile(fakeSSH, []byte(fakeSSHScript), 0o755))

		id := os.Getpid()
		srcPool, tgtPool = fmt.Sprintf("zgoit%da", id), fmt.Sprintf("zgoit%db", id)
		mountDir = filepath.Join(workDir, "mnt")
		for _, p := range []string{srcPool, tgtPool} {
			img := filepath.Join(workDir, p+".img")
			must(exec.Command("truncate", "-s", "256m", img).Run())
			out, err := exec.Command("zpool", "create", "-f", "-m", filepath.Join(mountDir, p), p, img).CombinedOutput()
			if err != nil {
				fmt.Printf("zpool create %s: %v: %s\n", p, err, out)
				return
			}
			defer exec.Command("zpool", "destroy", "-f", p).Run()
		}
		code = m.Run()
	}()
	os.Exit(code)
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

// fakeSSHScript mimics ssh(1): it skips options, takes the destination, and
// runs the remaining words with sh, exactly as sshd would.
const fakeSSHScript = `#!/bin/sh
while [ $# -gt 0 ]; do
	case "$1" in
	-n) exec </dev/null; shift ;;
	-[46AaCfGgKkMNqsTtVvXxYy]) shift ;;
	-[BbcDEeFIiJLlmOopQRSWw]) shift 2 ;;
	-*) shift ;;
	*) break ;;
	esac
done
[ $# -gt 0 ] || { echo "fakessh: no destination" >&2; exit 255; }
shift
exec sh -c "$*"
`

// result is a finished zelta invocation.
type result struct {
	Stdout, Stderr string
	Code           int
}

// zelta runs the binary with an isolated configuration directory.
func zelta(t *testing.T, env []string, args ...string) result {
	t.Helper()
	return runProg(t, zeltaBin, env, args...)
}

func runProg(t *testing.T, prog string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(prog, args...)
	etc := filepath.Join(workDir, "etc")
	os.MkdirAll(etc, 0o755)
	cmd.Env = append([]string{"PATH=" + filepath.Dir(zeltaBin) + ":" + os.Getenv("PATH"), "HOME=" + workDir, "ZELTA_ETC=" + etc, "USER=root"}, env...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("%s %v: %v", prog, args, err)
	}
	return result{out.String(), errb.String(), code}
}

// sh runs a setup command and fails the test on error.
func sh(t *testing.T, script string) string {
	t.Helper()
	out, err := exec.Command("sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", script, err, out)
	}
	return string(out)
}

// tree creates a fresh pair of dataset roots for one test.
func tree(t *testing.T, name string) (src, tgt string) {
	t.Helper()
	src, tgt = srcPool+"/"+name, tgtPool+"/"+name
	sh(t, "zfs create -u "+src)
	t.Cleanup(func() {
		exec.Command("zfs", "destroy", "-rf", src).Run()
		exec.Command("zfs", "destroy", "-rf", tgt).Run()
		for _, p := range []string{src, tgt} {
			out, _ := exec.Command("zfs", "list", "-H", "-o", "name", "-d", "1", filepath.Dir(p)).Output()
			for ds := range strings.FieldsSeq(string(out)) {
				if strings.HasPrefix(ds, p+"_") {
					exec.Command("zfs", "destroy", "-rf", ds).Run()
				}
			}
		}
	})
	return src, tgt
}

var spaces = regexp.MustCompile(`[ \t]+`)

// normalize collapses whitespace like the ShellSpec helpers do.
func normalize(s string) []string {
	var out []string
	for l := range strings.SplitSeq(strings.TrimSpace(s), "\n") {
		out = append(out, strings.TrimSpace(spaces.ReplaceAllString(l, " ")))
	}
	return out
}

func expectLines(t *testing.T, got string, want ...string) {
	t.Helper()
	lines := normalize(got)
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Errorf("output mismatch:\n--- got\n%s\n--- want\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}
}

func expectOK(t *testing.T, r result) {
	t.Helper()
	if r.Code != 0 {
		t.Fatalf("exit %d\nstdout:\n%s\nstderr:\n%s", r.Code, r.Stdout, r.Stderr)
	}
}

func snapshots(t *testing.T, ds string) []string {
	t.Helper()
	out, err := exec.Command("zfs", "list", "-H", "-o", "name", "-t", "snapshot", "-s", "createtxg", "-d", "1", ds).Output()
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(out)), "\n")
}

func exists(ds string) bool { return exec.Command("zfs", "list", "-H", "-o", "name", ds).Run() == nil }
