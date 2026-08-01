package zconf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestParseResolveTargets(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": `
BACKUP_ROOT: tank/Backups
ADD_DATASET_PREFIX: 0
SITE:
  host1.example:
  - pool/one/two/three
  - pool/leaf
  host2.example:
    options:
      ADD_HOST_PREFIX: 1
      ADD_DATASET_PREFIX: 1
    datasets:
      - a/b/c
  localhost:
  - zroot/jail/x: remote:tank/X
  host3:
    options:
      ADD_HOST_PREFIX: 1
      ADD_DATASET_PREFIX: 0
    datasets:
      - p/q
    options:
      BACKUP_ROOT: other:tank/B
      ADD_HOST_PREFIX: 0
    datasets:
      - p/q
`,
	})
	jobs, warns, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warns: %v", warns)
	}
	want := []struct {
		site, host, remote, source, target string
	}{
		{"SITE", "host1.example", "host1.example", "pool/one/two/three", "tank/Backups/three"},
		{"SITE", "host1.example", "host1.example", "pool/leaf", "tank/Backups/leaf"},
		{"SITE", "host2.example", "host2.example", "a/b/c", "tank/Backups/host2.example/b/c"},
		{"SITE", "localhost", "localhost", "zroot/jail/x", "remote:tank/X"},
		{"SITE", "host3", "host3", "p/q", "tank/Backups/host3/q"},
		{"SITE", "host3", "host3", "p/q", "other:tank/B/q"},
	}
	if len(jobs) != len(want) {
		t.Fatalf("jobs=%d want %d", len(jobs), len(want))
	}
	for i, w := range want {
		j := jobs[i]
		if j.Site != w.site || j.Host != w.host || j.SourceRemote != w.remote ||
			j.Source != w.source || j.Target != w.target {
			t.Errorf("job %d: got %+v want site=%q host=%q remote=%q source=%q target=%q",
				i, j, w.site, w.host, w.remote, w.source, w.target)
		}
		if got := j.SourceEP(); got != w.remote+":"+w.source {
			t.Errorf("job %d SourceEP: got %q want %q", i, got, w.remote+":"+w.source)
		}
	}
}

func TestParseImportSplice(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": `
RETRY: 1
AWS0:
  app2.cloud0:
    options:
      import: targets/vault1.den0.yaml
      import: rules/hostbackup.yaml
    datasets:
      import: sources/app2.cloud0.yaml
    options:
      import: targets/vault1.nyc1.yaml
      import: rules/hostbackup.yaml
    datasets:
      import: sources/app2.cloud0.yaml
`,
		"targets/vault1.den0.yaml": "BACKUP_ROOT: vault1.den0:data1/Backups\n",
		"targets/vault1.nyc1.yaml": "BACKUP_ROOT: vault1.nyc1:rust12/Backups\n",
		"rules/hostbackup.yaml":    "ADD_HOST_PREFIX: 1\n",
		"sources/app2.cloud0.yaml": "- zroot\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"app2.cloud0:zroot vault1.den0:data1/Backups/app2.cloud0/zroot",
		"app2.cloud0:zroot vault1.nyc1:rust12/Backups/app2.cloud0/zroot",
	}
	if len(jobs) != len(want) {
		t.Fatalf("jobs=%d want %d", len(jobs), len(want))
	}
	for i, w := range want {
		if got := jobs[i].SourceEP() + " " + jobs[i].Target; got != w {
			t.Errorf("job %d: got %q want %q", i, got, w)
		}
	}
}

func TestParseTabIndentRejected(t *testing.T) {
	// AWK also uses explicit space indentation; tabs are not accepted.
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nS:\n\th.example:\n\t- pool/a/b\n",
	})
	_, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err == nil || !strings.Contains(err.Error(), "configuration parse error") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

func TestParseHostPathNoSplit(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nS:\n  h:\n  - host:path\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d", len(jobs))
	}
	// "host:path" — internal colon, no space after, should not split as target
	if jobs[0].Source != "host:path" {
		t.Fatalf("source=%q", jobs[0].Source)
	}
	if jobs[0].Target != "tank/B/host:path" {
		t.Fatalf("target=%q", jobs[0].Target)
	}
}

func TestParseUnknownOption(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "NOT_A_REAL_OPTION: 1\nS:\n  h:\n  - p\n",
	})
	_, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err == nil || !strings.Contains(err.Error(), "unknown option: NOT_A_REAL_OPTION") {
		t.Fatalf("err=%v", err)
	}
}

func TestParseMissingTargetWarn(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "S:\n  h:\n  - p/q\n",
	})
	jobs, warns, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 0 {
		t.Fatalf("jobs=%d", len(jobs))
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "no target defined") {
		t.Fatalf("warns=%v", warns)
	}
}

func TestParseMixedCaseAndAliases(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "backup_root: tank/B\nsnap_time: 4h\nHOST_PREFIX: 1\nPREFIX: 0\nS:\n  h:\n  - a/b\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs=%d", len(jobs))
	}
	j := jobs[0]
	if j.Target != "tank/B/h/b" {
		t.Fatalf("target=%q", j.Target)
	}
	// snap_time canonicalized to SNAP_TIME with value intact
	if v := j.Options["SNAP_TIME"]; v != "4h" {
		t.Fatalf("SNAP_TIME=%q", v)
	}
}

func TestParseOverrideSeedsAndBlocks(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/A\nS:\n  h:\n  - p/q\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), map[string]string{
		"BACKUP_ROOT": "tank/CLI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 || jobs[0].Target != "tank/CLI/q" {
		t.Fatalf("jobs=%+v", jobs)
	}
}

func TestParseImportLoop(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.yaml": "import: b.yaml\n",
		"b.yaml": "import: a.yaml\n",
	})
	_, _, err := Load(filepath.Join(dir, "a.yaml"), nil)
	if err == nil || !strings.Contains(err.Error(), "recursive import") {
		t.Fatalf("loop err=%v", err)
	}
}
