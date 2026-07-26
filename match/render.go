package match

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"git.belltower.it/djbell/zelta-go/report"
)

// DefaultCols is the default -o list after synonym expansion.
var DefaultCols = []string{"ds_suffix", "match", "src_last", "tgt_last", "info"}

// FormatOpts controls match table rendering.
type FormatOpts struct {
	Cols      []string
	SrcLeaf   string
	Scripting bool
	Parsable  bool
}

// Format renders pairs like zelta match (-H or human).
func Format(pairs []*Pair, opt FormatOpts) string {
	if len(pairs) == 0 {
		return ""
	}
	cols := opt.Cols
	if len(cols) == 0 {
		cols = DefaultCols
	}
	if opt.Scripting {
		return formatScripting(pairs, cols, opt.Parsable)
	}
	return formatHuman(pairs, cols, opt.SrcLeaf, opt.Parsable)
}

func formatScripting(pairs []*Pair, cols []string, parsable bool) string {
	var b strings.Builder
	for _, p := range pairs {
		for i, c := range cols {
			if i > 0 {
				b.WriteByte('\t')
			}
			b.WriteString(colValue(p, c, "", true, parsable))
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func formatHuman(pairs []*Pair, cols []string, srcLeaf string, parsable bool) string {
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
			v := colValue(p, c, srcLeaf, false, parsable)
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
	sum := summaryOf(pairs)
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

func colValue(p *Pair, key, srcLeaf string, scripting, parsable bool) string {
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
		v = report.FormatBytes(p.XferSize, scripting || parsable)
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
	n := parseBytes(w)
	if raw {
		// already numeric from list - keep as-is if plain int
		if _, err := strconv.ParseInt(w, 10, 64); err == nil {
			return w
		}
		return strconv.FormatInt(n, 10)
	}
	return report.FormatBytes(n, false)
}

func leafOf(dataset string) string {
	if i := strings.LastIndex(dataset, "/"); i >= 0 {
		return dataset[i+1:]
	}
	return dataset
}
