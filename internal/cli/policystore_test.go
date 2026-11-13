package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/policy"
)

func TestPolicySaveRemove(t *testing.T) {
	h := newHarness(t)
	dir := t.TempDir()
	h.app.CacheDir = func() string { return dir }
	path := filepath.Join(h.env["ZELTA_ETC"], "policy.json")

	h.run("backup", "-v", "-X", "*/tmp", "pool/vm/vm_balls", "backup:pool/vm/vm_balls")
	h.run("sync", "pool/other", "sink/other")
	h.run("backup", "-R", "pool/rep", "sink/rep")

	if code := h.run("policy", "--save", "vm_balls"); code != 0 {
		t.Fatalf("save: %d %s", code, h.err.String())
	}
	if !strings.Contains(h.out.String(), "saved: pool/vm/vm_balls -> backup:pool/vm/vm_balls\n  + EXCLUDE=*/tmp\n") {
		t.Errorf("save output: %q", h.out.String())
	}
	if code := h.run("policy", "--save", "sink/other"); code != 0 {
		t.Fatalf("save sync: %d %s", code, h.err.String())
	}
	if code := h.run("policy", "--save", "rep"); code != 1 || !strings.Contains(h.err.String(), "policy jobs run 'zelta backup'") {
		t.Errorf("replicate must not be saved: %d %s", code, h.err.String())
	}

	s, err := policy.LoadStore(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Jobs) != 2 || s.Jobs[0].Options["EXCLUDE"] != "*/tmp" || s.Jobs[0].Options["LOG_LEVEL"] != "" || s.Jobs[1].Options["SEND_INTR"] != "0" {
		t.Fatalf("store: %+v", s.Jobs)
	}

	// Saving again records the new options.
	h.run("backup", "pool/vm/vm_balls", "backup:pool/vm/vm_balls")
	h.run("policy", "--save", "vm_balls")
	if !strings.Contains(h.out.String(), "updated: pool/vm/vm_balls -> backup:pool/vm/vm_balls\n  - EXCLUDE=*/tmp\n") {
		t.Errorf("update output: %q", h.out.String())
	}

	// With a store present, it is the default policy.
	if code := h.run("policy"); code != 0 || len(h.calls) != 1 || h.calls[0].opts.Get("CONFIG") != path {
		t.Errorf("default policy: %d %+v", code, h.calls)
	}

	if code := h.run("policy", "--rm", "sink/other"); code != 0 || !strings.Contains(h.out.String(), "removed: pool/other -> sink/other") {
		t.Fatalf("rm: %d %s", code, h.err.String())
	}
	s, _ = policy.LoadStore(path)
	if len(s.Jobs) != 1 {
		t.Errorf("after rm: %+v", s.Jobs)
	}

	conf := filepath.Join(t.TempDir(), "zelta.conf")
	os.WriteFile(conf, []byte("BACKUP_ROOT: sink/Backups\nsite1:\n  host1:\n    - tank/vm\n"), 0o644)
	if code := h.run("policy", "-C", conf, "--save", "vm_balls"); code != 1 || !strings.Contains(h.err.String(), "cannot change") {
		t.Errorf("saving into zelta.conf must fail: %d %s", code, h.err.String())
	}
	if code := h.run("policy", "--import", conf); code != 0 {
		t.Fatalf("import: %d %s", code, h.err.String())
	}
	s, _ = policy.LoadStore(path)
	if len(s.Jobs) != 2 || s.Jobs[1].Label() != "host1:tank/vm -> sink/Backups/vm" || s.Jobs[1].Group != "site1" || s.Jobs[1].Options["BACKUP_ROOT"] != "" {
		t.Errorf("imported: %+v", s.Jobs)
	}
}
