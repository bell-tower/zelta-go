package backup

import "strconv"

// HumanBytes renders a byte count like the upstream Awk h_num() helper:
// binary units (B, K, M, G, T, P, E), value truncated toward zero.
func HumanBytes(n int64) string {
	if n < 0 {
		return "-" + HumanBytes(-n)
	}
	suffix := "B"
	num := float64(n)
	for _, s := range []string{"K", "M", "G", "T", "P", "E"} {
		if num < 1024 {
			break
		}
		num /= 1024
		suffix = s
	}
	return strconv.FormatInt(int64(num), 10) + suffix
}
