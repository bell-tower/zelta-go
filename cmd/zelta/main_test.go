package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"zelta.space/internal/execx"
	"zelta.space/internal/failover"
)

func TestWiring(t *testing.T) {
	a := newApp()
	if failover.Backup == nil {
		t.Fatalf("failover.Backup is not wired")
	}
	for _, name := range []string{"match", "backup", "snapshot", "clone", "revert", "rotate", "rebase", "lock", "unlock", "failover", "propsync", "prune", "report"} {
		if a.Engines[name] == nil {
			t.Errorf("engine %q not wired", name)
		}
	}
	if a.Engines["zprune"] != nil {
		t.Errorf("zelta must not wire the zprune engine")
	}
	if a.Policy == nil || a.PolicyObjects == nil || a.Columns == nil {
		t.Fatalf("policy or completion sources not wired")
	}
	if cols := a.Columns(); !slices.Contains(cols, "ds_suffix") || !slices.Contains(cols, "relname") {
		t.Errorf("columns = %v", cols)
	}
}

// TestEndToEndDryRun drives the real engines through the CLI with a fake
// executor, from argv to output.
func TestEndToEndDryRun(t *testing.T) {
	a := newApp()
	var out, errb bytes.Buffer
	a.Stdout, a.Stderr, a.Exec = &out, &errb, execx.NewFake()
	t.Setenv("ZELTA_ETC", t.TempDir())
	code := a.Main(context.Background(), []string{"zelta", "match", "-n", "-d2", "pool/a", "host:pool/b"})
	want := "+ zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg -d2 'pool/a'\n" +
		"+ ssh -n host zfs list -Hpr -o name,guid,written,creation,used -t all -Screatetxg -d2 'pool/b'\n"
	if code != 0 || out.String() != want {
		t.Errorf("match dry run: code=%d\n%s%s", code, out.String(), errb.String())
	}

	out.Reset()
	code = a.Main(context.Background(), []string{"zelta", "policy", "-n", "-C", "/nonexistent/zelta.conf"})
	if code != 1 || !strings.Contains(errb.String(), "empty or unreadable policy file") {
		t.Errorf("policy: code=%d %q", code, errb.String())
	}
}
