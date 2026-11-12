// Package units converts between human-readable and numeric sizes, durations,
// and numbers using the same conventions as the original Awk implementation.
package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

const sizeSuffixes = "KMGTPEZ"

// ParseSize converts a size such as "128K", "1.5G", or "4096" into bytes.
// An optional trailing "B" is accepted ("10GB"). It returns false when the
// value is not a valid size.
func ParseSize(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	s = strings.TrimSuffix(strings.TrimSuffix(s, "B"), "b")
	power := 0
	if n := len(s); n > 0 {
		if i := strings.IndexByte(sizeSuffixes, upper(s[n-1])); i >= 0 {
			power = i + 1
			s = s[:n-1]
		}
	}
	if !isDecimal(s) {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	f *= math.Pow(1024, float64(power))
	if f > math.MaxInt64 {
		return 0, false
	}
	return int64(f), true
}

// DurationError reports an unusable duration string.
type DurationError struct {
	Value string
	Msg   string
}

func (e *DurationError) Error() string { return e.Msg }

var durationUnits = []struct {
	name    string
	seconds float64
}{
	{"seconds", 1},
	{"minutes", 60},
	{"hours", 3600},
	{"days", 86400},
	{"weeks", 604800},
	{"months", 2592000},
	{"years", 31557600},
}

// ParseDuration converts a relative duration such as "30days", "8h",
// "30 minutes", or "1mo" into seconds. Units may be abbreviated to any
// unambiguous prefix; the bare unit "m" is rejected because it could mean
// minutes or months. A bare number is a count of seconds.
func ParseDuration(s string) (int64, error) {
	orig := s
	s = strings.TrimLeft(s, "+-")
	s = strings.Join(strings.Fields(s), "")
	i := strings.IndexFunc(s, isLetter)
	num, unit := s, ""
	if i >= 0 {
		num, unit = s[:i], s[i:]
	}
	if num == "" || !isDecimal(num) {
		return 0, &DurationError{orig, fmt.Sprintf("invalid duration '%s'", orig)}
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, &DurationError{orig, fmt.Sprintf("invalid duration '%s'", orig)}
	}
	if unit == "" {
		return int64(f), nil
	}
	if unit == "m" || unit == "M" {
		return 0, &DurationError{orig, "ambiguous duration unit 'm'; use 'mi' or 'mo'"}
	}
	unit = strings.ToLower(unit)
	for _, u := range durationUnits {
		if strings.HasPrefix(u.name, unit) {
			return int64(f * u.seconds), nil
		}
	}
	return 0, &DurationError{orig, fmt.Sprintf("invalid duration '%s'", orig)}
}

// IsBareNumber reports whether s is an unsigned decimal number with no unit.
func IsBareNumber(s string) bool {
	return isDecimal(strings.TrimSpace(s))
}

// HumanBytes formats a byte count the way Zelta has always printed sizes:
// the integer part of the largest binary unit, e.g. 1536 -> "1K".
func HumanBytes(n float64) string {
	suffix := "B"
	for i := 0; i < len("KMGTPE") && n >= 1024; i++ {
		n /= 1024
		suffix = string("KMGTPE"[i])
	}
	return strconv.FormatInt(int64(n), 10) + suffix
}

// HumanBytesPrecise formats a byte count with one decimal for units above
// bytes, as used by zprune summaries, e.g. 1536 -> "1.5K".
func HumanBytesPrecise(n float64) string {
	units := []string{"B", "K", "M", "G", "T", "P", "E"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d%s", int64(n), units[i])
	}
	return fmt.Sprintf("%.1f%s", n, units[i])
}

// FormatNumber prints a float the way Awk prints numbers: integral values
// without a fraction and everything else with "%.6g".
func FormatNumber(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'g', 6, 64)
}

// Atoi converts the leading numeric portion of s to an int, as Awk does when
// a string is used in numeric context. Non-numeric input yields 0.
func Atoi(s string) int {
	return int(Atof(s))
}

// Atof converts the leading numeric portion of s to a float64, as Awk does.
func Atof(s string) float64 {
	s = strings.TrimLeft(s, " \t\n")
	end := 0
	seenDigit, seenDot, seenExp := false, false, false
	for end < len(s) {
		c := s[end]
		switch {
		case c >= '0' && c <= '9':
			seenDigit = true
		case (c == '+' || c == '-') && (end == 0 || s[end-1] == 'e' || s[end-1] == 'E'):
		case c == '.' && !seenDot && !seenExp:
			seenDot = true
		case (c == 'e' || c == 'E') && seenDigit && !seenExp:
			seenExp = true
		default:
			goto done
		}
		end++
	}
done:
	for end > 0 {
		if f, err := strconv.ParseFloat(s[:end], 64); err == nil {
			return f
		}
		end--
	}
	return 0
}

// IsNumeric reports whether s looks like a number to Awk (an optionally
// signed decimal, possibly with a fraction or exponent, and nothing else).
func IsNumeric(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	_, err := strconv.ParseFloat(s, 64)
	return err == nil && !strings.ContainsAny(s, "xXnNiI")
}

func isDecimal(s string) bool {
	if s == "" {
		return false
	}
	dot := false
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c >= '0' && c <= '9':
		case c == '.' && !dot && i > 0 && i < len(s)-1:
			dot = true
		default:
			return false
		}
	}
	return true
}

func isLetter(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }

func upper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 'a' + 'A'
	}
	return c
}
