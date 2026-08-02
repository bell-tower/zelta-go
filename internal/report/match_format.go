package report

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bell-tower/zelta-go/match"
)

// DefaultMatchCols is the default -o list after synonym expansion.
var DefaultMatchCols = []string{"ds_suffix", "match", "src_last", "tgt_last", "info"}

// MatchFormatOpts controls match table rendering (CLI presentation).
type MatchFormatOpts struct {
	Cols      []string
	SrcLeaf   string
	Scripting bool
	Parsable  bool
	// CheckTime appends SOURCE/TARGET_LIST_TIME lines when times are non-zero.
	CheckTime   bool
	SrcListTime float64
	TgtListTime float64
}

// FormatMatch renders pairs like zelta match (-H or human).
func FormatMatch(pairs []*match.Pair, opt MatchFormatOpts) string {
	if len(pairs) == 0 && !opt.CheckTime {
		return ""
	}
	cols := opt.Cols
	if len(cols) == 0 {
		cols = DefaultMatchCols
	}
	var out string
	if len(pairs) > 0 {
		if opt.Scripting {
			out = formatMatchScripting(pairs, cols, opt.Parsable)
		} else {
			out = formatMatchHuman(pairs, cols, opt.SrcLeaf, opt.Parsable)
		}
	}
	if opt.CheckTime {
		out += formatListTimes(opt.SrcListTime, opt.TgtListTime)
	}
	return out
}

// MatchSummary is the human footer ("N up-to-date, M syncable").
func MatchSummary(pairs []*match.Pair) string {
	var up, sync, blocked int
	for _, p := range pairs {
		switch {
		case p.Info == "up-to-date":
			up++
		case strings.HasPrefix(p.Info, "syncable"):
			sync++
		case strings.HasPrefix(p.Info, "blocked") || p.Info == "no source (target only)":
			blocked++
		}
	}
	var parts []string
	if up > 0 {
		parts = append(parts, strconv.Itoa(up)+" up-to-date")
	}
	if sync > 0 {
		parts = append(parts, strconv.Itoa(sync)+" syncable")
	}
	if blocked > 0 {
		parts = append(parts, strconv.Itoa(blocked)+" blocked")
	}
	return strings.Join(parts, ", ")
}

// DatasetLeaf returns the last path component of a dataset name.
func DatasetLeaf(dataset string) string {
	if i := strings.LastIndex(dataset, "/"); i >= 0 {
		return dataset[i+1:]
	}
	return dataset
}

func formatMatchScripting(pairs []*match.Pair, cols []string, parsable bool) string {
	var b strings.Builder
	for _, p := range pairs {
		for i, c := range cols {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(matchColValue(p, c, "", true, parsable))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func formatMatchHuman(pairs []*match.Pair, cols []string, srcLeaf string, parsable bool) string {
	vals := make([][]string, len(pairs))
	widths := make([]int, len(cols))
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = strings.ToUpper(c)
		widths[i] = utf8.RuneCountInString(headers[i])
	}
	for r, p := range pairs {
		row := make([]string, len(cols))
		for i, c := range cols {
			v := matchColValue(p, c, srcLeaf, false, parsable)
			row[i] = v
			if n := utf8.RuneCountInString(v); n > widths[i] {
				widths[i] = n
			}
		}
		vals[r] = row
	}

	var b strings.Builder
	writePadded(&b, headers, widths)
	b.WriteByte('\n')
	for _, row := range vals {
		writePadded(&b, row, widths)
		b.WriteByte('\n')
	}
	sum := MatchSummary(pairs)
	if sum != "" {
		b.WriteString(sum)
		b.WriteByte('\n')
	}
	if len(pairs) > 1 && strings.Contains(sum, ",") {
		b.WriteString(strconv.Itoa(len(pairs)))
		b.WriteString(" total datasets compared\n")
	}
	return b.String()
}

func writePadded(b *strings.Builder, cells []string, widths []int) {
	for i, cell := range cells {
		if i > 0 {
			b.WriteString("  ")
		}
		b.WriteString(cell)
		pad := widths[i] - utf8.RuneCountInString(cell)
		for j := 0; j < pad; j++ {
			b.WriteByte(' ')
		}
	}
}

func matchColValue(p *match.Pair, key, srcLeaf string, scripting, parsable bool) string {
	var v string
	switch key {
	case "ds_suffix":
		v = p.DSSuffix
	case "match":
		v = p.Match
	case "match_ivset":
		v = p.MatchIVSet
	case "num_matches":
		v = strconv.Itoa(p.NumMatches)
	case "xfer_num":
		v = strconv.Itoa(p.XferNum)
	case "xfer_size":
		v = FormatBytes(p.XferSize, scripting || parsable)
	case "src_name":
		v = p.SrcName
	case "tgt_name":
		v = p.TgtName
	case "src_first":
		v = p.SrcFirst
	case "tgt_first":
		v = p.TgtFirst
	case "src_last":
		v = p.SrcLast
	case "tgt_last":
		v = p.TgtLast
	case "src_next":
		v = p.SrcNext
	case "tgt_next":
		v = p.TgtNext
	case "info":
		v = p.Info
	case "src_written":
		v = formatWritten(p.SrcWritten, scripting || parsable)
	case "tgt_written":
		v = formatWritten(p.TgtWritten, scripting || parsable)
	case "src_snaps":
		v = strconv.Itoa(p.SrcSnaps)
	case "tgt_snaps":
		v = strconv.Itoa(p.TgtSnaps)
	default:
		v = ""
	}
	if scripting {
		return v
	}
	if v == "" {
		switch key {
		case "ds_suffix":
			return "[" + srcLeaf + "]"
		default:
			return "-"
		}
	}
	return v
}

func formatWritten(w string, raw bool) string {
	if w == "" || w == "-" {
		if raw {
			return ""
		}
		return "-"
	}
	n := parseMatchBytes(w)
	if raw {
		if _, err := strconv.ParseInt(w, 10, 64); err == nil {
			return w
		}
		return strconv.FormatInt(n, 10)
	}
	return FormatBytes(n, false)
}

func parseMatchBytes(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" {
		return 0
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	// zfs-style human: 512K, 1.5M, 2G
	mult := int64(1)
	upper := strings.ToUpper(s)
	upper = strings.TrimSuffix(upper, "IB")
	upper = strings.TrimSuffix(upper, "B")
	if len(upper) == 0 {
		return 0
	}
	switch upper[len(upper)-1] {
	case 'K':
		mult = 1024
		upper = upper[:len(upper)-1]
	case 'M':
		mult = 1024 * 1024
		upper = upper[:len(upper)-1]
	case 'G':
		mult = 1024 * 1024 * 1024
		upper = upper[:len(upper)-1]
	case 'T':
		mult = 1024 * 1024 * 1024 * 1024
		upper = upper[:len(upper)-1]
	case 'P':
		mult = 1024 * 1024 * 1024 * 1024 * 1024
		upper = upper[:len(upper)-1]
	}
	f, err := strconv.ParseFloat(upper, 64)
	if err != nil {
		return 0
	}
	return int64(f * float64(mult))
}

func formatListTimes(src, tgt float64) string {
	var b strings.Builder
	if src > 0 {
		fmt.Fprintf(&b, "SOURCE_LIST_TIME:\t%s\n", formatSeconds(src))
	}
	if tgt > 0 {
		fmt.Fprintf(&b, "TARGET_LIST_TIME:\t%s\n", formatSeconds(tgt))
	}
	return b.String()
}

func formatSeconds(s float64) string {
	t := fmt.Sprintf("%.2f", s)
	t = strings.TrimRight(t, "0")
	t = strings.TrimRight(t, ".")
	if t == "" {
		return "0"
	}
	return t
}
