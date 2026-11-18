package main

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"zelta.space/zelta"
)

// verdict is what a plan does with one snapshot.
type verdict uint8

const (
	guarded verdict = iota // at or newer than the guard match
	kept                   // older than the match, kept by retention
	doomed                 // selected for destruction
	nVerdicts
)

// row is one dataset's verdicts, oldest first.
type row struct {
	name    string
	v       []verdict
	times   []time.Time
	doomed  int
	reclaim int64 // sum of used of the doomed snapshots
}

// rows returns the datasets that have snapshots and how many don't.
func rows(p *zelta.PrunePlan) (out []row, empty int) {
	for _, d := range p.Datasets {
		if len(d.Snapshots) == 0 {
			empty++
			continue
		}
		destroy, keep := set(d.Destroy), set(d.Keep)
		r := row{name: d.Name}
		if d.Suffix != "" {
			r.name = d.Suffix
		}
		for _, s := range d.Snapshots {
			r.times = append(r.times, s.Creation)
			switch {
			case destroy[s.Name]:
				r.v = append(r.v, doomed)
				r.doomed++
				r.reclaim += s.Used
			case keep[s.Name]:
				r.v = append(r.v, kept)
			default:
				r.v = append(r.v, guarded)
			}
		}
		out = append(out, r)
	}
	return out, empty
}

func set(list []string) map[string]bool {
	m := make(map[string]bool, len(list))
	for _, s := range list {
		m[s] = true
	}
	return m
}

// cell is a run of snapshots drawn as one character: the older verdict a
// fills the left eighths of the cell and b the rest.
type cell struct {
	a, b    verdict
	eighths int // 1..8; 8 means the cell is all a
}

// cells draws v in k cells, each holding an equal share of the snapshots.
func cells(v []verdict, k int) []cell {
	if k < 1 || k > len(v) {
		k = len(v)
	}
	out := make([]cell, 0, k)
	for i := range k {
		out = append(out, mix(v[i*len(v)/k:(i+1)*len(v)/k]))
	}
	return out
}

// mix shows the two most common verdicts of a run in time order.
func mix(run []verdict) cell {
	var n [nVerdicts]int
	first := [nVerdicts]int{-1, -1, -1}
	for i, v := range run {
		if first[v] < 0 {
			first[v] = i
		}
		n[v]++
	}
	a, b := verdict(0), verdict(0)
	for v := range nVerdicts {
		if n[v] > n[a] {
			a = v
		}
	}
	b = a
	for v := range nVerdicts {
		if v != a && n[v] > 0 && (b == a || n[v] > n[b]) {
			b = v
		}
	}
	if b == a {
		return cell{a, a, 8}
	}
	if first[b] < first[a] {
		a, b = b, a
	}
	e := (n[a]*8 + (n[a]+n[b])/2) / (n[a] + n[b])
	return cell{a, b, max(1, min(7, e))}
}

// style draws cells and text for a terminal, with or without color.
type style struct{ color bool }

// Palette colors only, so the terminal's theme picks the shades: light,
// dark, and solarized all keep their own red, green, and cyan.
var fg = [nVerdicts]int{guarded: 36, kept: 32, doomed: 31}

// monochrome glyphs, by verdict: destruction fades.
var mono = [nVerdicts]string{guarded: "▓", kept: "█", doomed: "░"}

var eighths = []string{"", "▏", "▎", "▍", "▌", "▋", "▊", "▉", "█"}

func (s style) cell(c cell) string {
	if !s.color {
		if c.eighths >= 4 {
			return mono[c.a]
		}
		return mono[c.b]
	}
	if c.eighths == 8 {
		return fmt.Sprintf("\x1b[%dm█\x1b[0m", fg[c.a])
	}
	return fmt.Sprintf("\x1b[%d;%dm%s\x1b[0m", fg[c.a], fg[c.b]+10, eighths[c.eighths])
}

// wide draws a cell n characters wide, ending in a thin gap so that
// neighboring snapshots stay countable.
func (s style) wide(c cell, n int) string {
	if n == 1 || c.eighths != 8 {
		return strings.Repeat(s.cell(c), n)
	}
	if !s.color {
		return strings.Repeat(mono[c.a], n-1) + " "
	}
	return fmt.Sprintf("\x1b[%dm%s▉\x1b[0m", fg[c.a], strings.Repeat("█", n-1))
}

func (s style) sgr(code, text string) string {
	if !s.color || code == "" {
		return text
	}
	return "\x1b[" + code + "m" + text + "\x1b[0m"
}

func (s style) swatch(v verdict) string { return s.cell(cell{v, v, 8}) }

// line is a screen line that knows its visible width. Text past max, if
// set, is cut.
type line struct {
	b   strings.Builder
	w   int
	max int
}

func (l *line) add(s style, code, text string) {
	if l.max > 0 && l.w+utf8.RuneCountInString(text) > l.max {
		if l.w >= l.max {
			return
		}
		text = fitRight(text, l.max-l.w)
	}
	l.b.WriteString(s.sgr(code, text))
	l.w += utf8.RuneCountInString(text)
}

func (l *line) raw(text string, width int) {
	l.b.WriteString(text)
	l.w += width
}

func (l *line) pad(to int) {
	if l.w < to {
		l.b.WriteString(strings.Repeat(" ", to-l.w))
		l.w = to
	}
}

func (l *line) String() string { return l.b.String() }

// fit shortens text to width runes, eliding from the left so the end of a
// dataset name stays readable.
func fit(text string, width int) string {
	r := []rune(text)
	if len(r) <= width {
		return text
	}
	if width < 2 {
		return string(r[len(r)-width:])
	}
	return "…" + string(r[len(r)-width+1:])
}

// bars draws one line per dataset, all at the same snapshots per cell,
// starting from the first dataset. It also returns the snapshots per cell.
func bars(s style, rs []row, first, width, height int) ([]string, float64) {
	nameW := 0
	maxN := 0
	for _, r := range rs {
		nameW = max(nameW, utf8.RuneCountInString(r.name))
		maxN = max(maxN, len(r.v))
	}
	nameW = min(nameW, max(12, width/3))
	d := len(fmt.Sprint(maxN))
	countW := 2*d + 5 // " 12 / 34 "
	room := width - nameW - countW
	scale := 1.0 // snapshots per cell
	if maxN > room {
		scale = float64(maxN) / float64(room)
	}
	wide := widen(maxN, room)
	var out []string
	for i := first; i < len(rs) && len(out) < height; i++ {
		r := rs[i]
		var l line
		l.add(s, "", fit(r.name, nameW))
		l.pad(nameW)
		l.add(s, "", fmt.Sprintf(" %*d", d, r.doomed))
		l.add(s, "2", fmt.Sprintf(" / %-*d ", d, len(r.v)))
		for _, c := range cells(r.v, int(math.Ceil(float64(len(r.v))/scale))) {
			l.raw(s.wide(c, wide), wide)
		}
		out = append(out, l.String())
	}
	return out, scale
}

// widen is how many characters each cell takes so that a few snapshots
// still make a readable bar: up to 3, when n cells fit room that many times.
func widen(n, room int) int {
	if n == 0 {
		return 1
	}
	return max(1, min(3, room/n))
}

// blockCap is how many snapshots a single dataset shows one cell each
// before cells start holding several.
const blockCap = 1000

// block draws a single dataset's snapshots as a block of cells, oldest
// first, row by row, each row labeled with the creation time of its first
// snapshot. It also returns the snapshots per cell.
func block(s style, r row, width, height int) ([]string, float64) {
	layout := "2006-01-02 "
	if n := len(r.times); n > 0 && r.times[n-1].Sub(r.times[0]) < 48*time.Hour {
		layout = "Jan 02 15:04 "
	}
	labelW := len(layout)
	width -= labelW
	cs := cells(r.v, min(blockCap, width*height))
	wide := widen(len(cs), width)
	width /= wide
	// Square up the block when it doesn't need the full width.
	w := width
	if rowsNeeded := (len(cs) + w - 1) / w; rowsNeeded > 0 {
		w = (len(cs) + rowsNeeded - 1) / rowsNeeded
	}
	var out []string
	for i := 0; i < len(cs); i += w {
		var b strings.Builder
		if len(r.times) == len(r.v) {
			b.WriteString(s.sgr("2", r.times[i*len(r.v)/len(cs)].Format(layout)))
		}
		for _, c := range cs[i:min(i+w, len(cs))] {
			b.WriteString(s.wide(c, wide))
		}
		out = append(out, b.String())
	}
	return out, float64(len(r.v)) / float64(max(1, len(cs)))
}

// size formats bytes like zfs list.
func size(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%dB", b)
	}
	f, u := float64(b), 0
	for f >= unit && u < 6 {
		f /= unit
		u++
	}
	prec := 2
	switch {
	case f >= 100:
		prec = 0
	case f >= 10:
		prec = 1
	}
	return fmt.Sprintf("%.*f%c", prec, f, "BKMGTPE"[u])
}
