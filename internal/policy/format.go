package policy

import (
	"fmt"
	"sort"
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

// FormatCommands renders dry-run raw zelta backup commands (-n -v).
// Strips policy-internal env vars (LOG_PREFIX, LOG_MODE, LOG_LEVEL, LOG_COMMAND)
// and policy-only-scope options.
func FormatCommands(jobs []Job) string {
	if len(jobs) == 0 {
		return ""
	}
	ps := policyScopeSet()
	var b strings.Builder
	for _, j := range jobs {
		b.WriteString("+ ")
		for _, k := range sortedKeys(j.Options) {
			if ps[k] {
				continue
			}
			v := j.Options[k]
			if v == "" {
				continue
			}
			switch k {
			case "LOG_PREFIX", "LOG_MODE", "LOG_LEVEL", "LOG_COMMAND":
				continue
			}
			b.WriteString("ZELTA_")
			b.WriteString(k)
			b.WriteString("=")
			b.WriteString(dq(v))
			b.WriteString(" ")
		}
		b.WriteString("zelta backup ")
		b.WriteString(shq(j.SourceEP()))
		b.WriteString(" ")
		b.WriteString(shq(j.Target))
		b.WriteString("\n")
	}
	return b.String()
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// shq wraps s in single quotes, escaping embedded single quotes via '\”.
func shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

// dq wraps s in double quotes, escaping $ ` " \.
func dq(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '$', '`', '"', '\\':
			b.WriteByte('\\')
			b.WriteByte(s[i])
		default:
			b.WriteByte(s[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}
