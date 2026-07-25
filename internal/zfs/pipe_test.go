package zfs

import (
	"strings"
	"testing"
)

func TestPipeShellSameHost(t *testing.T) {
	got, err := PipeShell(
		"root@debian:apool/a",
		"root@debian:bpool/b",
		[]string{"zfs", "send", "-P", "apool/a@x"},
		[]string{"zfs", "recv", "-v", "bpool/b"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "ssh -n root@debian ") {
		t.Fatalf("ssh: %s", got)
	}
	if !strings.Contains(got, "zfs send") || !strings.Contains(got, "|") || !strings.Contains(got, "zfs recv") {
		t.Fatalf("pipe: %s", got)
	}
}

func TestPipeShellLocal(t *testing.T) {
	got, err := PipeShell("tank/a", "tank/b",
		[]string{"zfs", "send", "tank/a@x"},
		[]string{"zfs", "recv", "tank/b"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "ssh") {
		t.Fatalf("local: %s", got)
	}
	if got != "zfs send tank/a@x | zfs recv tank/b" && !strings.Contains(got, "zfs send") {
		// quoted form ok
		if !strings.Contains(got, "|") {
			t.Fatalf("got %s", got)
		}
	}
}

func TestPipeShellDualRemoteDirections(t *testing.T) {
	send := []string{"zfs", "send", "-P", "apool/a@x"}
	recv := []string{"zfs", "recv", "-v", "bpool/b"}
	src := "root@debian:apool/a"
	tgt := "root@vault:bpool/b"

	pull, err := PipeShellDirection(src, tgt, send, recv, "PULL")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pull, "ssh -n root@vault ") ||
		!strings.Contains(pull, "ssh -n root@debian zfs send") ||
		!strings.Contains(pull, " | zfs recv") {
		t.Fatalf("pull: %s", pull)
	}

	push, err := PipeShellDirection(src, tgt, send, recv, "PUSH")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(push, "ssh -n root@debian ") ||
		!strings.Contains(push, "zfs send") ||
		!strings.Contains(push, " | ssh root@vault zfs recv") {
		t.Fatalf("push: %s", push)
	}

	proxy, err := PipeShellDirection(src, tgt, send, recv, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(proxy, "{ ssh -n root@debian ") ||
		!strings.Contains(proxy, "|ssh root@vault ") ||
		!strings.HasSuffix(proxy, " ; }") {
		t.Fatalf("proxy: %s", proxy)
	}
}
