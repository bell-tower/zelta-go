package command

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLocalRendering(t *testing.T) {
	c := New(Words("zfs", "list", "-Hpr", "-o", "name,guid")...).With(Name("sink/source"))
	if got, want := c.String(), "zfs list -Hpr -o name,guid 'sink/source'"; got != want {
		t.Errorf("String = %q want %q", got, want)
	}
	if got, want := c.Argv(), []string{"zfs", "list", "-Hpr", "-o", "name,guid", "sink/source"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Argv = %#v", got)
	}
	o := New(Word("zfs"), Word("recv"), Word("-o"), LitName("origin=", "pool/a b@s"), Name("pool/t"))
	if got, want := o.String(), "zfs recv -o origin='pool/a b@s' 'pool/t'"; got != want {
		t.Errorf("String = %q want %q", got, want)
	}
	if got := o.Argv()[3]; got != "origin=pool/a b@s" {
		t.Errorf("Argv lit name = %q", got)
	}
	f := New(Word("zfs"), Word("recv")).With(Fragment("-o 'note=a b' -u")...)
	if got, want := f.String(), "zfs recv -o 'note=a b' -u"; got != want {
		t.Errorf("String = %q want %q", got, want)
	}
}

func TestRemoteRendering(t *testing.T) {
	r := NewRemote("ssh -n", "user@host")
	c := New(Words("zfs", "list", "-Hpr")...).With(Name("pool/my ds")).On(r)
	if got, want := c.String(), `ssh -n user@host zfs list -Hpr 'pool/my\ ds'`; got != want {
		t.Errorf("String = %q want %q", got, want)
	}
	if got, want := c.Argv(), []string{"ssh", "-n", "user@host", "zfs", "list", "-Hpr", `pool/my\ ds`}; !reflect.DeepEqual(got, want) {
		t.Errorf("Argv = %#v", got)
	}
	s := Cmd{Remote: r, Script: "zfs send 'a' | zfs recv 'b'"}
	if got, want := s.String(), `ssh -n user@host "zfs send 'a' | zfs recv 'b'"`; got != want {
		t.Errorf("String = %q want %q", got, want)
	}
	if got := s.Argv(); len(got) != 4 || got[3] != "zfs send 'a' | zfs recv 'b'" {
		t.Errorf("Argv = %#v", got)
	}
	odd := New(Word("echo"), Name("we'ird"), Word("a b")).On(r)
	if got, want := odd.Argv()[3:], []string{"echo", `'we'\''ird'`, "'a b'"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Argv odd = %#v", got)
	}
}

func TestPipelineString(t *testing.T) {
	p := Pipeline{New(Words("zfs", "send")...), New(Words("zfs", "recv")...)}
	if got, want := p.String(), "{ zfs send|zfs recv ; }"; got != want {
		t.Errorf("String = %q", got)
	}
	p = append(Pipeline{p[0], Script("pv 2>>/dev/tty")}, p[1])
	if got, want := p.String(), "{ zfs send|pv 2>>/dev/tty | zfs recv ; }"; got != want {
		t.Errorf("String = %q", got)
	}
	if (Pipeline{}).String() != "" || (Pipeline{Script("x")}).String() != "x" {
		t.Errorf("degenerate pipelines")
	}
	if !(Cmd{}).IsZero() || Script("x").IsZero() {
		t.Errorf("IsZero")
	}
	if got := Script("echo hi").Argv(); !reflect.DeepEqual(got, []string{"sh", "-c", "echo hi"}) {
		t.Errorf("script argv = %#v", got)
	}
}

// fakeSSH writes an ssh stand-in that ignores options and the host, then
// runs the joined remaining words with sh, exactly as sshd would.
func fakeSSH(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "fakessh")
	script := "#!/bin/sh\nwhile [ $# -gt 0 ]; do case \"$1\" in -n) shift;; -p|-o) shift 2;; *) break;; esac; done\nshift\nexec sh -c \"$*\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func run(t *testing.T, argv []string) string {
	t.Helper()
	out, err := exec.Command(argv[0], argv[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v: %s", argv, err, out)
	}
	return string(out)
}

// TestRemoteExecutionAndDisplayAgree runs remote commands through a fake ssh
// both from their argv and from their displayed text to prove that the
// rendering is executable and preserves every word, including nesting.
func TestRemoteExecutionAndDisplayAgree(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	ssh := fakeSSH(t)
	r := NewRemote(ssh+" -n", "user@host")
	words := []Arg{Word("printf"), Word("[%s]"), Name("pool/my ds@a"), Word("x y"), Word("it's"), LitName("origin=", "p/q r@s")}
	c := New(words...).On(r)
	want := "[pool/my ds@a][x y][it's][origin=p/q r@s]"
	if got := run(t, c.Argv()); got != want {
		t.Errorf("argv exec = %q want %q", got, want)
	}
	if got := run(t, []string{"sh", "-c", c.String()}); got != want {
		t.Errorf("display exec = %q want %q", got, want)
	}

	// Nested: a remote command embedded in a script on another remote.
	inner := New(Word("printf"), Word("[%s]"), Name("pool/my ds@a")).On(NewRemote(ssh+" -n", "src"))
	outer := Cmd{Remote: NewRemote(ssh, "tgt"), Script: inner.String() + " | cat"}
	if got := run(t, outer.Argv()); got != "[pool/my ds@a]" {
		t.Errorf("nested argv exec = %q", got)
	}
	if got := run(t, []string{"sh", "-c", outer.String()}); got != "[pool/my ds@a]" {
		t.Errorf("nested display exec = %q", got)
	}
	if !strings.Contains(outer.String(), `'pool/my\ ds@a'`) {
		t.Errorf("nested display lost name quoting: %s", outer.String())
	}
}
