package prune

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseDuration mirrors oracle parse_duration (seconds).
// Units: s(econds), mi(nutes), h(ours), d(ays), w(eeks), mo(nths), y(ears);
// bare number = seconds. "m" is ambiguous (oracle stop). Whitespace stripped.
func ParseDuration(s string) (int64, error) {
	str := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' {
			return -1
		}
		return r
	}, s)
	str = strings.TrimLeft(str, "+-")
	if str == "" {
		return 0, fmt.Errorf("invalid duration '%s'", s)
	}
	i := 0
	for i < len(str) && (str[i] == '.' || (str[i] >= '0' && str[i] <= '9')) {
		i++
	}
	numPart, unit := str[:i], str[i:]
	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid duration '%s'", s)
	}
	unit = strings.ToLower(unit)
	if unit == "m" {
		return 0, fmt.Errorf("ambiguous duration unit 'm'; use 'mi' or 'mo'")
	}
	if unit == "" {
		return int64(num), nil
	}
	mult, err := durationMultiplier(unit)
	if err != nil {
		return 0, err
	}
	return int64(num * float64(mult)), nil
}

// durationMultiplier: oracle prefix match ("d" matches "days", etc.).
func durationMultiplier(unit string) (int64, error) {
	type entry struct {
		name string
		mult int64
	}
	for _, e := range []entry{
		{"seconds", 1},
		{"minutes", 60},
		{"hours", 3600},
		{"days", 86400},
		{"weeks", 604800},
		{"months", 2592000},
		{"years", 31557600},
	} {
		if strings.HasPrefix(e.name, unit) {
			return e.mult, nil
		}
	}
	return 0, fmt.Errorf("invalid duration unit '%s'", unit)
}

// ParseSize mirrors oracle parse_size (bytes; K/M/G/T… = 1024^n).
func ParseSize(s string) (int64, error) {
	str := strings.TrimSpace(s)
	if str == "" {
		return 0, fmt.Errorf("invalid size '%s'", s)
	}
	i := 0
	for i < len(str) && (str[i] == '.' || (str[i] >= '0' && str[i] <= '9')) {
		i++
	}
	num, err := strconv.ParseFloat(str[:i], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size '%s'", s)
	}
	unit := strings.ToUpper(strings.TrimSuffix(str[i:], "B"))
	if unit == "" {
		return int64(num), nil
	}
	power := strings.Index("KMGTPEZ", unit)
	if len(unit) != 1 || power < 0 {
		return 0, fmt.Errorf("invalid size '%s'", s)
	}
	mult := float64(int64(1) << (10 * (power + 1)))
	return int64(num * mult), nil
}
