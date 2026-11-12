package match

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/logging"
	"github.com/bell-tower/zelta-go/internal/units"
)

// Run implements "zelta match SOURCE [TARGET]".
func Run(ctx context.Context, rt *app.Runtime, args []string) error {
	src, tgt, err := Endpoints(rt, args)
	if err != nil {
		return err
	}
	cols, bad := parseColumns(rt.Opts.Get("PROPLIST"))
	if bad != "" || cols == nil {
		return app.Usage("bad property list: invalid property '%s'", bad)
	}
	if err := CheckDepth(rt); err != nil {
		return err
	}
	if src.DS == "" && tgt.DS == "" {
		return app.Usage("no datasets defined")
	}
	req := RequestFromOptions(rt, src, tgt)
	for _, c := range cols {
		if c.Name == "match_ivset" {
			req.IVSet = true
		}
	}
	res, err := Compare(ctx, rt, req)
	if err != nil || res == nil {
		return err
	}
	printResult(rt, res, cols)
	return nil
}

// Endpoints parses the source and optional target operands. Further
// operands are ignored, as they always have been.
func Endpoints(rt *app.Runtime, args []string) (src, tgt endpoint.Endpoint, err error) {
	for i, a := range args {
		ep := rt.Endpoint(a)
		if !ep.Valid() {
			return src, tgt, app.Fail("invalid endpoint '%s'", a)
		}
		switch i {
		case 0:
			src = ep
		case 1:
			tgt = ep
		}
	}
	return src, tgt, nil
}

// CheckDepth rejects a DEPTH that is not a non-negative number. A depth of
// 0 means unlimited, as zelta.env documents.
func CheckDepth(rt *app.Runtime) error {
	if d := rt.Opts.Get("DEPTH"); d != "" && (!units.IsNumeric(d) || units.Atof(d) < 0) {
		return app.Fail("depth of '%s' invalid; must be positive", rt.Opts.Get("DEPTH"))
	}
	return nil
}

// rawValue is a column's value as printed in scripting (-H) mode: counts
// that were never incremented and missing names are empty.
func rawValue(p *Pair, c *Column) string {
	return rawValueIn(nil, p, c)
}

// rawValueIn is rawValue with access to the listings, which match_ivset
// needs: Zelta 1.2 compared the literal "-" that zfs prints for unencrypted
// snapshots, so a match between two of them reports "-".
func rawValueIn(res *Result, p *Pair, c *Column) string {
	count := func(n int) string {
		if n == 0 {
			return ""
		}
		return strconv.Itoa(n)
	}
	switch c.Name {
	case "ds_suffix":
		return p.Suffix
	case "match":
		return p.Match
	case "match_ivset":
		if p.MatchIVSet == "" && res != nil && unencryptedMatch(res, p) {
			return "-"
		}
		return p.MatchIVSet
	case "num_matches":
		return count(p.NumMatches)
	case "xfer_size":
		if p.XferNum == 0 {
			return ""
		}
		return strconv.FormatInt(p.XferSize, 10)
	case "xfer_num":
		return count(p.XferNum)
	case "src_name":
		return p.SrcName
	case "src_first":
		return p.SrcFirst
	case "src_next":
		return p.SrcNext
	case "src_last":
		return p.SrcLast
	case "src_written":
		return p.SrcWritten
	case "src_snaps":
		return count(p.SrcSnaps)
	case "tgt_name":
		return p.TgtName
	case "tgt_first":
		return p.TgtFirst
	case "tgt_next":
		return p.TgtNext
	case "tgt_last":
		return p.TgtLast
	case "tgt_written":
		return p.TgtWritten
	case "tgt_snaps":
		return count(p.TgtSnaps)
	case "info":
		return p.Info
	}
	return ""
}

// humanValue renders a column for the aligned table, with placeholders for
// empty values and human-readable sizes (exact sizes when parsable).
func humanValue(raw string, c *Column, leaf string, parsable bool) string {
	empty := raw == "" || ((c.kind == kindNum || c.kind == kindBytes) && units.Atof(raw) == 0)
	switch {
	case empty && c.kind == kindSuffix:
		return "[" + leaf + "]"
	case empty && c.kind == kindBytes:
		if parsable {
			return "0"
		}
		return "0B"
	case empty:
		return "-"
	case c.kind == kindBytes && !parsable:
		return units.HumanBytes(units.Atof(raw))
	}
	return raw
}

func printResult(rt *app.Runtime, res *Result, cols []*Column) {
	notice := func(s string) { rt.Log.Log(logging.Notice, s) }
	if len(res.Pairs) == 0 {
		rt.Log.Errorf("datasets inaccessible or do not exist")
		return
	}
	scripting := rt.Opts.Bool("SCRIPTING_MODE")
	if scripting {
		for _, p := range res.Pairs {
			cells := make([]string, len(cols))
			for i, c := range cols {
				cells[i] = rawValueIn(res, p, c)
			}
			notice(strings.Join(cells, "\t"))
		}
	} else {
		for _, line := range Table(res, cols, res.Source.Endpoint.Leaf, rt.Opts.Bool("PARSABLE")) {
			notice(line)
		}
		notice(res.Summary)
		if len(res.Pairs) > 1 && strings.Contains(res.Summary, ",") {
			notice(fmt.Sprintf("%d total datasets compared", len(res.Pairs)))
		}
	}
	if rt.Opts.Bool("CHECK_TIME") {
		if t := res.Source.ListTime; t != 0 {
			notice("SOURCE_LIST_TIME:\t" + units.FormatNumber(t))
		}
		if t := res.Target.ListTime; t != 0 {
			notice("TARGET_LIST_TIME:\t" + units.FormatNumber(t))
		}
	}
}

// Table renders the header and rows of the aligned match table. Every
// column, including the last, is padded to its width and columns are
// separated by two spaces.
func Table(res *Result, cols []*Column, leaf string, parsable bool) []string {
	cells := make([][]string, len(res.Pairs))
	width := map[string]int{}
	for _, c := range cols {
		width[c.Name] = len(c.Name)
	}
	for r, p := range res.Pairs {
		cells[r] = make([]string, len(cols))
		for i, c := range cols {
			v := humanValue(rawValueIn(res, p, c), c, leaf, parsable)
			cells[r][i] = v
			if n := utf8.RuneCountInString(v); n > width[c.Name] {
				width[c.Name] = n
			}
		}
	}
	format := func(vals []string) string {
		var b strings.Builder
		for i, c := range cols {
			if i > 0 {
				b.WriteString("  ")
			}
			fmt.Fprintf(&b, "%-*s", width[c.Name], vals[i])
		}
		return b.String()
	}
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = strings.ToUpper(c.Name)
	}
	lines := []string{format(header)}
	for _, row := range cells {
		lines = append(lines, format(row))
	}
	return lines
}

// unencryptedMatch reports whether the match pairs snapshots that both lack
// an IV set GUID.
func unencryptedMatch(res *Result, p *Pair) bool {
	sd := res.Source.Dataset(p.Suffix)
	if p.Match == "" || sd == nil || p.MatchIdx < 1 || p.MatchIdx > len(sd.Snaps) {
		return false
	}
	s := sd.Snaps[p.MatchIdx-1]
	t := res.Target.Dataset(p.Suffix).ByGUID(s.GUID)
	return s.IVSetGUID == "" && t != nil && t.IVSetGUID == ""
}
