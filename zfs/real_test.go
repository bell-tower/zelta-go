package zfs

import (
	"testing"

	"github.com/bell-tower/zelta-go/cmdbuild"
	"github.com/bell-tower/zelta-go/endpoint"
)

func TestShellSingleQuote(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"simple", "'simple'"},
		{"lift off", "'lift off'"},
		{"a'b", `'a'\''b'`},
	}
	for _, tc := range cases {
		if got := shellSingleQuote(tc.in); got != tc.want {
			t.Errorf("shellSingleQuote(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestShellJoin(t *testing.T) {
	got := shellJoin([]string{"zfs", "list", "apool/x/lift off"})
	want := "'zfs' 'list' 'apool/x/lift off'"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSSHTarget(t *testing.T) {
	cases := []struct {
		ep     string
		want   string
		wantOK bool
	}{
		{"apool/treetop", "", false},
		{"localhost:apool/x", "", false},
		{"debian:apool/x", "debian", true},
		{"root@debian:apool/x", "root@debian", true},
		{"space@debian:bpool/y", "space@debian", true},
	}
	for _, tc := range cases {
		ep, err := endpoint.Parse(tc.ep)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.ep, err)
		}
		got, ok := sshTarget(ep)
		if ok != tc.wantOK || got != tc.want {
			t.Errorf("sshTarget(%q)=(%q,%v) want (%q,%v)", tc.ep, got, ok, tc.want, tc.wantOK)
		}
	}
}

func TestPipeRoles(t *testing.T) {
	if !cmdbuild.StdinNull(cmdbuild.RoleSend) {
		t.Fatal("SEND should ssh -n")
	}
	if cmdbuild.StdinNull(cmdbuild.RoleRecv) {
		t.Fatal("RECV must keep stdin")
	}
	if !cmdbuild.StdinNull(cmdbuild.RoleDefault) {
		t.Fatal("DEFAULT should ssh -n")
	}
}
