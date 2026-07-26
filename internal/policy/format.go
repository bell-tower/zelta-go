package policy

import (
	"fmt"
	"strings"
)

// FormatTable renders dry-run SOURCE/TARGET pairs.
// noHeader (-H): single space, no header. Else column-aligned with two spaces.
func FormatTable(jobs []Job, noHeader bool) string {
	if len(jobs) == 0 {
		return ""
	}
	maxSrc := 6
	for _, j := range jobs {
		if n := len(j.SourceEP()); n > maxSrc {
			maxSrc = n
		}
	}
	var b strings.Builder
	if !noHeader {
		fmt.Fprintf(&b, "%-*s  %s\n", maxSrc, "SOURCE", "TARGET")
	}
	for _, j := range jobs {
		src := j.SourceEP()
		if noHeader {
			fmt.Fprintf(&b, "%s %s\n", src, j.Target)
		} else {
			fmt.Fprintf(&b, "%-*s  %s\n", maxSrc, src, j.Target)
		}
	}
	return b.String()
}
