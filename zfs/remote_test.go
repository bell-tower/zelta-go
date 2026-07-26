package zfs

import (
	"reflect"
	"strings"
	"testing"
)

func TestSSHConfigArgv(t *testing.T) {
	c := SSHConfig{
		IdentityFile: "/key",
		Port:         2202,
		Options:      []string{"StrictHostKeyChecking=accept-new"},
	}
	got, err := c.Argv("root@host", "zfs list tank", RoleDefault)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"ssh", "-n",
		"-o", "BatchMode=yes", "-o", "ConnectTimeout=30",
		"-i", "/key", "-p", "2202",
		"-o", "StrictHostKeyChecking=accept-new",
		"root@host", "zfs list tank",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}

	recv, err := c.Argv("host", "zfs recv tank", RoleRecv)
	if err != nil {
		t.Fatal(err)
	}
	if recv[1] == "-n" {
		t.Fatalf("recv must not use -n: %#v", recv)
	}
}

func TestSSHConfigShell(t *testing.T) {
	c := SSHConfig{Port: 22}
	s, err := c.Shell("h", "zfs send tank@1", RoleSend)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(s, "'h'") || !strings.Contains(s, "zfs send") {
		t.Fatalf("unexpected shell: %s", s)
	}
	if !strings.Contains(s, "-n") {
		t.Fatalf("send shell should include -n: %s", s)
	}
}

func TestCommandRemotePrefixes(t *testing.T) {
	c := CommandRemote{
		Command: "ssh -p 2202",
		Send:    "mbuffer -s 128k | ssh -p 2202",
		Recv:    "ssh -p 2202",
	}
	def := c.prefix(RoleDefault)
	if def != "ssh -p 2202 -n" {
		t.Fatalf("default prefix: %q", def)
	}
	if c.prefix(RoleSend) != "mbuffer -s 128k | ssh -p 2202" {
		t.Fatalf("send prefix: %q", c.prefix(RoleSend))
	}
	if c.prefix(RoleRecv) != "ssh -p 2202" {
		t.Fatalf("recv prefix: %q", c.prefix(RoleRecv))
	}

	argv, err := c.Argv("host", "zfs list x", RoleDefault)
	if err != nil {
		t.Fatal(err)
	}
	if len(argv) != 3 || argv[0] != "sh" || argv[1] != "-c" {
		t.Fatalf("CommandRemote must use sh -c: %#v", argv)
	}
	if !strings.Contains(argv[2], "ssh -p 2202 -n") || !strings.Contains(argv[2], "'host'") {
		t.Fatalf("shell line: %s", argv[2])
	}
}

func TestCommandRemoteExplicitDefault(t *testing.T) {
	c := CommandRemote{Default: "ssh -n -o BatchMode=yes"}
	if c.prefix(RoleDefault) != "ssh -n -o BatchMode=yes" {
		t.Fatal(c.prefix(RoleDefault))
	}
	if c.prefix(RoleSend) != "ssh -n -o BatchMode=yes" {
		t.Fatal(c.prefix(RoleSend))
	}
}

func TestRealRemoteOverride(t *testing.T) {
	r := &Real{Remote: CommandRemote{Command: "ssh -p 9"}}
	if _, ok := r.remote().(CommandRemote); !ok {
		t.Fatal("expected CommandRemote")
	}
	r2 := &Real{SSH: SSHConfig{Port: 1}}
	if _, ok := r2.remote().(SSHConfig); !ok {
		t.Fatal("expected SSHConfig fallback")
	}
}
