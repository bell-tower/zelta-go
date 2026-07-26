package policy

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

func TestResolveTargetPrefixes(t *testing.T) {
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
	got := FormatTable(jobs, true)
	want := strings.Join([]string{
		"host1.example:pool/one/two/three tank/Backups/three",
		"host1.example:pool/leaf tank/Backups/leaf",
		"host2.example:a/b/c tank/Backups/host2.example/b/c",
		"localhost:zroot/jail/x remote:tank/X",
		"host3:p/q tank/Backups/host3/q",
		"host3:p/q other:tank/B/q",
		"",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestPrefixNegativeAndLarge(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"p2.yaml": "BACKUP_ROOT: tank/B\nADD_DATASET_PREFIX: -1\nS:\n  h:\n  - one/two/three\n",
		"p3.yaml": "BACKUP_ROOT: tank/B\nADD_DATASET_PREFIX: 99\nS:\n  h:\n  - one/two/three\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "p2.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTable(jobs, true); got != "h:one/two/three tank/B\n" {
		t.Fatalf("prefix -1: %q", got)
	}
	jobs, _, err = Load(filepath.Join(dir, "p3.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTable(jobs, true); got != "h:one/two/three tank/B/one/two/three\n" {
		t.Fatalf("prefix 99: %q", got)
	}
}

func TestImportFanout(t *testing.T) {
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
	got := FormatTable(jobs, true)
	want := "" +
		"app2.cloud0:zroot vault1.den0:data1/Backups/app2.cloud0/zroot\n" +
		"app2.cloud0:zroot vault1.nyc1:rust12/Backups/app2.cloud0/zroot\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestFilterAndHeader(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": `
BACKUP_ROOT: tank/B
S:
  host.a:
  - pool/one
  - pool/two
  host.b:
  - pool/one
`,
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := Filter(jobs, []string{"host.a"})
	if len(f) != 2 {
		t.Fatalf("host.a: %d", len(f))
	}
	f = Filter(jobs, []string{"one"})
	if len(f) != 2 {
		t.Fatalf("leaf one: %d", len(f))
	}
	f = Filter(jobs, []string{"nope"})
	if len(f) != 0 {
		t.Fatalf("nope: %d", len(f))
	}
	out := FormatTable(jobs[:1], false)
	if !strings.HasPrefix(out, "SOURCE") {
		t.Fatalf("header: %q", out)
	}
	if !strings.Contains(out, "  ") {
		t.Fatalf("expected two-space cols: %q", out)
	}
}

func TestCLIOverrideBackupRoot(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/A\nS:\n  h:\n  - p/q\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), map[string]string{
		"BACKUP_ROOT": "tank/CLI",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTable(jobs, true); got != "h:p/q tank/CLI/q\n" {
		t.Fatalf("got %q", got)
	}
}

func TestUnknownOption(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "NOT_A_REAL_OPTION: 1\nS:\n  h:\n  - p\n",
	})
	_, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err == nil || !strings.Contains(err.Error(), "unknown option: NOT_A_REAL_OPTION") {
		t.Fatalf("err=%v", err)
	}
}

func TestLegacyAliases(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": "BACKUP_ROOT: tank/B\nHOST_PREFIX: 1\nPREFIX: 0\nS:\n  h:\n  - a/b\n",
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := FormatTable(jobs, true); got != "h:a/b tank/B/h/b\n" {
		t.Fatalf("got %q", got)
	}
}

func TestImportLoopAndDepth(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"a.yaml": "import: b.yaml\n",
		"b.yaml": "import: a.yaml\n",
	})
	_, _, err := Load(filepath.Join(dir, "a.yaml"), nil)
	if err == nil || !strings.Contains(err.Error(), "recursive import") {
		t.Fatalf("loop err=%v", err)
	}
}

func TestFormatCommands(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": `
BACKUP_ROOT: tank/B
SNAP_MODE: ALWAYS
SEND_INTR: 0
S:
  h.example:
  - pool/a/b
`,
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := FormatCommands(jobs)
	if !strings.HasPrefix(out, "+ ") {
		t.Fatalf("prefix: %q", out)
	}
	if !strings.Contains(out, "zelta backup") {
		t.Fatalf("command: %q", out)
	}
	if !strings.Contains(out, "ZELTA_SNAP_MODE") {
		t.Fatalf("expected SNAP_MODE in prefix: %q", out)
	}
	if !strings.Contains(out, "ZELTA_SEND_INTR") {
		t.Fatalf("expected SEND_INTR in prefix: %q", out)
	}
	if !strings.Contains(out, "'h.example:pool/a/b'") {
		t.Fatalf("source ep quoting: %q", out)
	}
	if !strings.Contains(out, "'tank/B/b'") {
		t.Fatalf("target path: %q", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
}

func TestFormatCommandsSkipsPolicyScope(t *testing.T) {
	dir := writeTree(t, map[string]string{
		"zelta.yaml": `
BACKUP_ROOT: tank/B
JOBS: 4
SEND_INTR: 1
S:
  h:
  - p/q
`,
	})
	jobs, _, err := Load(filepath.Join(dir, "zelta.yaml"), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := FormatCommands(jobs)
	if strings.Contains(out, "ZELTA_JOBS") {
		t.Fatalf("JOBS should be skipped: %q", out)
	}
	if !strings.Contains(out, "ZELTA_SEND_INTR") {
		t.Fatalf("SEND_INTR should be present: %q", out)
	}
}

func TestDQEscape(t *testing.T) {
	if dq(`a"b`) != `"a\"b"` {
		t.Fatalf("dq quote: %q", dq(`a"b`))
	}
	if dq(`a$b`) != `"a\$b"` {
		t.Fatalf("dq dollar: %q", dq(`a$b`))
	}
	if dq(`a\b`) != `"a\\b"` {
		t.Fatalf("dq backslash: %q", dq(`a\b`))
	}
	if dq(`a`+"`"+"b") != `"a\`+"`"+`b"` {
		t.Fatalf("dq backtick: %q", dq("a`b"))
	}
}

func TestSHQEscape(t *testing.T) {
	if shq(`hello`) != `'hello'` {
		t.Fatalf("shq simple: %q", shq(`hello`))
	}
	if shq(`it's`) != `'it'\''s'` {
		t.Fatalf("shq quote: %q", shq(`it's`))
	}
}

func TestMissingTargetWarn(t *testing.T) {
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
