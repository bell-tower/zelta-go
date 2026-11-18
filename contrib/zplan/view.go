package main

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const labelW = 17

func (a *app) view(w, h int) []string {
	st := a.st
	var out []string

	hd := line{max: w}
	hd.add(st, "1", "zplan ")
	hd.add(st, "", a.endpoint)
	if a.guard != "" {
		hd.add(st, "2", " guarded by ")
		hd.add(st, "", a.guard)
	} else {
		hd.add(st, "2", " (no guard endpoint)")
	}
	if a.busy || a.planGen != a.gen {
		hd.max = 0
		if hd.w > w-11 {
			hd = line{max: w - 11}
			hd.add(st, "1", "zplan ")
			hd.add(st, "", a.endpoint)
		}
		hd.pad(w - 10)
		hd.add(st, "2", "planning…")
	}
	out = append(out, hd.String(), "")

	for i, f := range a.fields {
		out = append(out, a.fieldLine(i, f, w))
	}
	out = append(out, "")

	cmd := wrap("$ "+command(a.fields, a.endpoint, a.guard), w, 3)
	vizH := h - len(out) - 2 - len(cmd)
	var viz []string
	per := 1.0
	switch {
	case a.planGen == 0 && a.err == nil:
		viz = []string{st.sgr("2", "listing snapshots…")}
	case len(a.rows) == 0:
		viz = []string{st.sgr("2", "no snapshots")}
	case len(a.rows) == 1:
		viz, per = block(st, a.rows[0], w, vizH)
	case a.zoom:
		viz, per = block(st, a.rows[a.sel], w, vizH)
	default:
		n := vizH
		if len(a.rows) > n {
			n--
		}
		a.page = max(1, n)
		if a.sel >= 0 {
			a.scroll = min(a.scroll, a.sel)
			a.scroll = max(a.scroll, a.sel-a.page+1)
		}
		a.scroll = max(0, min(a.scroll, len(a.rows)-a.page))
		viz, per = bars(st, a.rows, a.scroll, a.sel, w, n)
		if rest := len(a.rows) - a.scroll - len(viz); rest > 0 {
			viz = append(viz, st.sgr("2", fmt.Sprintf("… %d more datasets", rest)))
		}
	}
	out = append(out, a.summary(w, per))
	out = append(out, viz...)
	bottom := h - 1 - len(cmd)
	for len(out) < bottom {
		out = append(out, "")
	}
	out = out[:bottom]
	for _, c := range cmd {
		out = append(out, st.sgr("2", c))
	}
	help := "↑↓ field  ←→ preset  type to edit  ^U clear  Enter print & quit  Esc quit"
	switch {
	case a.zoom:
		help = "↑↓ dataset  ← back  Tab fields  Enter print & quit  Esc back"
	case a.sel >= 0:
		help = "↑↓ PgUp PgDn dataset  → zoom  Tab fields  Enter print & quit  Esc quit"
	}
	out = append(out, st.sgr("2", fitRight(help, w)))
	return out
}

func (a *app) fieldLine(i int, f field, w int) string {
	st := a.st
	focused := i == a.focus && a.sel < 0
	l := line{max: w}
	mark := "  "
	if focused {
		mark = "▸ "
	}
	l.add(st, "", mark)
	code := ""
	if focused {
		code = "1"
	}
	l.add(st, code, f.label)
	l.pad(labelW + 2)

	val := f.val
	switch {
	case f.cycleOnly:
		val = "‹ " + val + " ›"
	case focused:
		val += "_"
	}
	if strings.TrimSpace(f.val) == "" && !f.cycleOnly {
		def := ""
		if retentionDefault(a.fields) {
			def = map[int]string{fNum: "default 30", fTime: "default 30days"}[i]
		}
		l.add(st, "", val)
		if def != "" {
			if val != "" {
				l.add(st, "", " ")
			}
			l.add(st, "2", def)
		}
	} else if focused {
		l.add(st, "7", val)
	} else {
		l.add(st, "", val)
	}
	if focused && f.hint != "" && w-l.w > 8 {
		l.add(st, "2", "  "+f.hint)
	}
	return l.String()
}

func (a *app) summary(w int, per float64) string {
	st := a.st
	l := line{max: w}
	if a.err != nil {
		l.add(st, "31", fit(a.err.Error(), w))
		return l.String()
	}
	rs, where := a.rows, ""
	if a.zoom {
		rs, where = a.rows[a.sel:a.sel+1], a.rows[a.sel].name
	}
	total, n := 0, 0
	var reclaim int64
	for _, r := range rs {
		total += len(r.v)
		n += r.doomed
		reclaim += r.reclaim
	}
	ds := "datasets"
	if len(a.rows) == 1 {
		ds = "dataset"
	}
	l.add(st, "1", fmt.Sprintf("destroy %d", n))
	if where != "" {
		l.add(st, "", fmt.Sprintf(" of %d snapshots in ", total))
		l.add(st, "1", where)
	} else {
		l.add(st, "", fmt.Sprintf(" of %d snapshots in %d %s", total, len(a.rows), ds))
	}
	if a.empty > 0 && where == "" {
		l.add(st, "2", fmt.Sprintf(" (+%d without)", a.empty))
	}
	if n > 0 {
		l.add(st, "", ", frees ≥ "+size(reclaim))
	}
	var leg line
	for _, x := range []struct {
		v    verdict
		name string
	}{{doomed, "destroy"}, {kept, "keep"}, {guarded, "guard"}} {
		leg.raw(" "+st.swatch(x.v), 2)
		leg.add(st, "", " "+x.name)
	}
	if per > 1.05 {
		leg.add(st, "2", fmt.Sprintf("  cell ≈ %.3g", per))
	}
	if l.w+leg.w+2 <= w {
		l.pad(w - leg.w)
		l.raw(leg.String(), leg.w)
	}
	return l.String()
}

// wrap breaks text at spaces into at most n lines of width runes,
// indenting continuations; the last line is cut if it doesn't fit.
func wrap(text string, width, n int) []string {
	var out []string
	cur := ""
	for word := range strings.SplitSeq(text, " ") {
		switch {
		case cur == "":
			cur = word
		case utf8.RuneCountInString(cur)+1+utf8.RuneCountInString(word) <= width:
			cur += " " + word
		default:
			out = append(out, cur)
			cur = "    " + word
		}
	}
	out = append(out, cur)
	if len(out) > n {
		out = append(out[:n-1], strings.Join(out[n-1:], " "))
	}
	for i := range out {
		out[i] = fitRight(out[i], width)
	}
	return out
}

// fitRight shortens text to width runes, eliding at the end.
func fitRight(text string, width int) string {
	if utf8.RuneCountInString(text) <= width {
		return text
	}
	return string([]rune(text)[:width-1]) + "…"
}
