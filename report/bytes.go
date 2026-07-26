package report

import (
	"fmt"
	"strconv"
	"strings"
)

// FormatBytes renders a size for match columns.
// Human mode uses ZFS-ish units (0B, 512K, 1.5M); parsable is a plain integer.
func FormatBytes(n int64, parsable bool) string {
	if parsable {
		return strconv.FormatInt(n, 10)
	}
	if n == 0 {
		return "0B"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	units := []struct {
		suf string
		div int64
	}{
		{"P", 1024 * 1024 * 1024 * 1024 * 1024},
		{"T", 1024 * 1024 * 1024 * 1024},
		{"G", 1024 * 1024 * 1024},
		{"M", 1024 * 1024},
		{"K", 1024},
	}
	var s string
	for _, u := range units {
		if n >= u.div {
			if n%u.div == 0 {
				s = strconv.FormatInt(n/u.div, 10) + u.suf
			} else {
				s = strings.TrimRight(strings.TrimRight(
					fmt.Sprintf("%.1f", float64(n)/float64(u.div)), "0"), ".") + u.suf
			}
			break
		}
	}
	if s == "" {
		s = strconv.FormatInt(n, 10) + "B"
	}
	if neg {
		return "-" + s
	}
	return s
}
