package shellwords

import (
	"os/exec"
	"reflect"
	"testing"
)

func TestSplit(t *testing.T) {
	tests := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"-L -c -e", []string{"-L", "-c", "-e"}},
		{"  ssh   -p 2202 ", []string{"ssh", "-p", "2202"}},
		{"-o 'com.x:note=a b'", []string{"-o", "com.x:note=a b"}},
		{`-o "a b"`, []string{"-o", "a b"}},
		{`a\ b c`, []string{"a b", "c"}},
		{`"x\"y" 'it'\''s'`, []string{`x"y`, "it's"}},
		{`"\a"`, []string{`\a`}},
		{`''`, []string{""}},
		{`a''b`, []string{"ab"}},
		{"a\tb\nc", []string{"a", "b", "c"}},
		{`"$HOME"`, []string{"$HOME"}},
		{"a\\\nb", []string{"ab"}},
	}
	for _, tt := range tests {
		got, err := Split(tt.in)
		if err != nil {
			t.Errorf("Split(%q) error: %v", tt.in, err)
			continue
		}
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("Split(%q) = %#v want %#v", tt.in, got, tt.want)
		}
	}
	for _, bad := range []string{`'open`, `"open`, `trail\`} {
		if _, err := Split(bad); err == nil {
			t.Errorf("Split(%q) expected error", bad)
		}
	}
}

func TestMustSplit(t *testing.T) {
	if got := MustSplit("'a b' c"); !reflect.DeepEqual(got, []string{"a b", "c"}) {
		t.Errorf("MustSplit = %#v", got)
	}
	if got := MustSplit("'open x"); !reflect.DeepEqual(got, []string{"'open", "x"}) {
		t.Errorf("MustSplit fallback = %#v", got)
	}
}

func TestQuote(t *testing.T) {
	tests := map[string]string{
		"":                 "''",
		"pool/ds@snap":     "pool/ds@snap",
		"-Hpr":             "-Hpr",
		"name,guid":        "name,guid",
		"a b":              "'a b'",
		"it's":             `'it'\''s'`,
		"$x":               "'$x'",
		"pool/ds#bm":       "pool/ds#bm",
		"readonly=on":      "readonly=on",
		"user@[::1]":       "'user@[::1]'",
		"@a%b":             "@a%b",
		"semi;colon":       "'semi;colon'",
		"compression=zstd": "compression=zstd",
	}
	for in, want := range tests {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %q want %q", in, got, want)
		}
	}
}

func TestDoubleQuote(t *testing.T) {
	tests := map[string]string{
		"plain":           `"plain"`,
		`a "b"`:           `"a \"b\""`,
		"$HOME `x`":       "\"\\$HOME \\`x\\`\"",
		`'pool/my\ ds'`:   `"'pool/my\ ds'"`,
		`end\`:            `"end\\"`,
		`back\\slash`:     `"back\\\slash"`,
		`'it'\''s'`:       `"'it'\''s'"`,
		"zfs send | recv": `"zfs send | recv"`,
	}
	for in, want := range tests {
		if got := DoubleQuote(in); got != want {
			t.Errorf("DoubleQuote(%q) = %q want %q", in, got, want)
		}
	}
}

// TestRoundTripShell verifies that quoted words survive a real POSIX shell.
func TestRoundTripShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("sh not available")
	}
	words := []string{"plain", "a b", "it's", `back\slash`, "$HOME", "`id`", "tab\there", "", "x;y", `"dq"`}
	for _, w := range words {
		for name, quoted := range map[string]string{"single": Quote(w), "double": DoubleQuote(w)} {
			out, err := exec.Command(sh, "-c", "printf '%s' "+quoted).Output()
			if err != nil {
				t.Fatalf("%s quote of %q: %v", name, w, err)
			}
			if string(out) != w {
				t.Errorf("%s quote of %q round-tripped to %q", name, w, out)
			}
		}
	}
	if got := Join([]string{"echo", "a b", "c"}); got != "echo 'a b' c" {
		t.Errorf("Join = %q", got)
	}
}
