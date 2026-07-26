package conf

import (
	"os"
	"path/filepath"
	"testing"
)

func writeEnv(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "zelta.env")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadEnvFileBasics(t *testing.T) {
	vals, warns, err := LoadEnvFile(writeEnv(t, `
# comment line
   # indented comment
LOG_LEVEL=4
DRYRUN=1  # trailing comment
SNAP_NAME='zelta_daily'
QUOTED="some value"
BARE_KEY_WITH_ZELTA=ok
=novalue
NOEQUALSLINE tolerated
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(warns) != 0 {
		t.Fatalf("warnings: %v", warns)
	}
	want := map[string]string{
		"LOG_LEVEL":           "4",
		"DRYRUN":              "1",
		"SNAP_NAME":           "zelta_daily",
		"QUOTED":              "some value",
		"BARE_KEY_WITH_ZELTA": "ok",
	}
	for k, w := range want {
		if vals[k] != w {
			t.Errorf("%s = %q, want %q", k, vals[k], w)
		}
	}
}

func TestLoadEnvFilePrefixAndRejects(t *testing.T) {
	vals, warns, err := LoadEnvFile(writeEnv(t, `
ZELTA_RESUME=0
ZELTA_ETC=/hack
ZELTA_ENV=/hack
ZELTA_AWK=mawk
EMPTY=
`))
	if err != nil {
		t.Fatal(err)
	}
	if vals["RESUME"] != "0" {
		t.Errorf("RESUME = %q", vals["RESUME"])
	}
	if len(warns) != 3 {
		t.Fatalf("want 3 reject warnings, got %v", warns)
	}
	if _, ok := vals["ETC"]; ok {
		t.Error("ETC must be rejected")
	}
	if _, ok := vals["EMPTY"]; ok {
		t.Error("empty value counts as unset")
	}
}

func TestLoadEnvFileMissing(t *testing.T) {
	vals, warns, err := LoadEnvFile(filepath.Join(t.TempDir(), "nope.env"))
	if err != nil || len(vals) != 0 || len(warns) != 0 {
		t.Fatalf("missing file: vals=%v warns=%v err=%v", vals, warns, err)
	}
}

func TestEtcDirEnvOverride(t *testing.T) {
	t.Setenv("ZELTA_ETC", "/tmp/custom-etc")
	if EtcDir() != "/tmp/custom-etc" {
		t.Fatalf("EtcDir = %s", EtcDir())
	}
}

func TestConfigPathOverride(t *testing.T) {
	t.Setenv("ZELTA_CONFIG", "/tmp/custom-zelta.conf")
	if ConfigPath() != "/tmp/custom-zelta.conf" {
		t.Fatalf("ConfigPath = %s", ConfigPath())
	}
}

func TestEnvPathOverride(t *testing.T) {
	t.Setenv("ZELTA_ENV", "/dev/null")
	if EnvPath() != "/dev/null" {
		t.Fatalf("EnvPath = %s", EnvPath())
	}
}
