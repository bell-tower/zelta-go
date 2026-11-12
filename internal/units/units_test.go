package units

import (
	"strings"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{"0", 0, true},
		{"4096", 4096, true},
		{"1K", 1024, true},
		{"1k", 1024, true},
		{"128K", 131072, true},
		{"1.5K", 1536, true},
		{"2M", 2 << 20, true},
		{"10G", 10 << 30, true},
		{"10GB", 10 << 30, true},
		{"10Gb", 10 << 30, true},
		{"1T", 1 << 40, true},
		{"1P", 1 << 50, true},
		{"1E", 1 << 60, true},
		{"3B", 3, true},
		{"", 0, false},
		{"G", 0, false},
		{"1X", 0, false},
		{"1.2.3", 0, false},
		{"-1", 0, false},
		{"1 G", 0, false},
		{".5G", 0, false},
		{"10GiB", 0, false},
		{"20Z", 0, false},
	}
	for _, tt := range tests {
		got, ok := ParseSize(tt.in)
		if ok != tt.ok || (ok && got != tt.want) {
			t.Errorf("ParseSize(%q) = %d,%v want %d,%v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseDuration(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		err  string
	}{
		{"30", 30, ""},
		{"30s", 30, ""},
		{"30sec", 30, ""},
		{"2mi", 120, ""},
		{"2min", 120, ""},
		{"2 minutes", 120, ""},
		{"8h", 28800, ""},
		{"1hour", 3600, ""},
		{"1d", 86400, ""},
		{"30days", 2592000, ""},
		{"1 day", 86400, ""},
		{"1w", 604800, ""},
		{"1mo", 2592000, ""},
		{"1mon", 2592000, ""},
		{"1y", 31557600, ""},
		{"1 year", 31557600, ""},
		{"1.5h", 5400, ""},
		{"+1d", 86400, ""},
		{"-1d", 86400, ""},
		{"1m", 0, "ambiguous duration unit 'm'"},
		{"1M", 0, "ambiguous duration unit 'm'"},
		{"1x", 0, "invalid duration '1x'"},
		{"days", 0, "invalid duration"},
		{"", 0, "invalid duration"},
		{"1dayz", 0, "invalid duration"},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.in)
		if tt.err != "" {
			if err == nil || !strings.Contains(err.Error(), tt.err) {
				t.Errorf("ParseDuration(%q) err = %v, want %q", tt.in, err, tt.err)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ParseDuration(%q) = %d,%v want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestHumanBytes(t *testing.T) {
	tests := map[float64]string{
		0:          "0B",
		1023:       "1023B",
		1024:       "1K",
		1536:       "1K",
		1048576:    "1M",
		5 << 30:    "5G",
		1 << 40:    "1T",
		1 << 50:    "1P",
		1 << 60:    "1E",
		1 << 62:    "4E",
		1048575:    "1023K",
		2147483648: "2G",
	}
	for in, want := range tests {
		if got := HumanBytes(in); got != want {
			t.Errorf("HumanBytes(%v) = %q want %q", in, got, want)
		}
	}
}

func TestHumanBytesPrecise(t *testing.T) {
	tests := map[float64]string{
		0:       "0B",
		1023:    "1023B",
		1536:    "1.5K",
		1 << 20: "1.0M",
		3 << 29: "1.5G",
	}
	for in, want := range tests {
		if got := HumanBytesPrecise(in); got != want {
			t.Errorf("HumanBytesPrecise(%v) = %q want %q", in, got, want)
		}
	}
}

func TestFormatNumber(t *testing.T) {
	tests := map[float64]string{
		0:           "0",
		3:           "3",
		-2:          "-2",
		0.03:        "0.03",
		0.1 + 0.2:   "0.3",
		12.5:        "12.5",
		1234567.891: "1.23457e+06",
		1e20:        "1e+20",
	}
	for in, want := range tests {
		if got := FormatNumber(in); got != want {
			t.Errorf("FormatNumber(%v) = %q want %q", in, got, want)
		}
	}
}

func TestAtoiAtof(t *testing.T) {
	tests := map[string]float64{
		"":         0,
		"12":       12,
		" 7":       7,
		"3abc":     3,
		"-4":       -4,
		"1.5":      1.5,
		"2e3":      2000,
		"2e":       2,
		"abc":      0,
		"+9":       9,
		"1.2.3":    1.2,
		"0x10":     0,
		"5 apples": 5,
	}
	for in, want := range tests {
		if got := Atof(in); got != want {
			t.Errorf("Atof(%q) = %v want %v", in, got, want)
		}
	}
	if Atoi("42.9") != 42 {
		t.Errorf("Atoi truncation failed")
	}
}

func TestIsNumeric(t *testing.T) {
	for _, s := range []string{"0", "12", "-3", "1.5", " 4 ", "1e5"} {
		if !IsNumeric(s) {
			t.Errorf("IsNumeric(%q) = false", s)
		}
	}
	for _, s := range []string{"", "abc", "1a", "NaN", "Inf", "0x1"} {
		if IsNumeric(s) {
			t.Errorf("IsNumeric(%q) = true", s)
		}
	}
	if !IsBareNumber("1715200000") || IsBareNumber("8h") || IsBareNumber("") {
		t.Errorf("IsBareNumber mismatch")
	}
}
