package cli

import (
	"io"
	"slices"
	"strings"
	"testing"
)

type fakeTTY struct {
	io.Reader
	strings.Builder
}

func (*fakeTTY) Close() error { return nil }

func TestReplay(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	h.app.CacheDir = func() string { return dir }

	h.run("backup", "-v", "pool/vm/vm_balls", "backup:pool/vm/vm_balls")
	h.run("backup", "pool/vm/vm_balls", "other_backup:tank/Backups/vm_balls")
	h.run("sync", "pool/other", "sink/other")
	h.run("backup", "-n", "pool/dry", "sink/dry")
	h.run("match", "pool/vm/vm_balls", "sink/vm_balls")

	if code := h.run("replay", "nothing"); code != 1 || !strings.Contains(h.err.String(), "no backup matches 'nothing'") {
		t.Fatalf("no match: %d %q", code, h.err.String())
	}
	if code := h.run("replay", "dry"); code != 1 {
		t.Errorf("dry runs must not be remembered")
	}

	if code := h.run("replay", "sink/other", "-q"); code != 0 || len(h.calls) != 1 {
		t.Fatalf("unique replay: %d %q", code, h.err.String())
	}
	c := h.calls[0]
	if c.engine != "backup" || c.opts.Get("VERB") != "sync" || !slices.Equal(c.args, []string{"pool/other", "sink/other"}) || c.opts.Get("LOG_LEVEL") != "1" {
		t.Errorf("replayed %s %v verb=%s level=%s", c.engine, c.args, c.opts.Get("VERB"), c.opts.Get("LOG_LEVEL"))
	}
	if !strings.Contains(h.err.String(), "replaying: zelta sync -q pool/other sink/other") {
		t.Errorf("replay notice: %q", h.err.String())
	}

	// Ambiguous without a terminal lists the choices and fails.
	if code := h.run("replay", "vm_balls"); code != 1 {
		t.Fatalf("ambiguous replay without tty must fail")
	}
	want := "1. pool/vm/vm_balls -> other_backup:tank/Backups/vm_balls\n2. pool/vm/vm_balls -> backup:pool/vm/vm_balls\n"
	if h.out.String() != want {
		t.Errorf("list:\n%s", h.out.String())
	}

	tty := &fakeTTY{Reader: strings.NewReader("2\n")}
	h.app.TTY = func() (io.ReadWriteCloser, error) { return tty, nil }
	if code := h.run("replay", "vm_balls"); code != 0 || len(h.calls) != 1 {
		t.Fatalf("chosen replay: %d %q", code, h.err.String())
	}
	if !strings.Contains(tty.String(), "Choose your fate: ") || h.calls[0].args[1] != "backup:pool/vm/vm_balls" || h.calls[0].opts.Get("LOG_LEVEL") != "3" {
		t.Errorf("chose %v; tty %q", h.calls[0].args, tty.String())
	}

	h.app.CacheDir = nil
	if code := h.run("replay", "sink/other"); code != 1 {
		t.Errorf("replay without a cache must fail")
	}
}
