// Package prune implements "zelta prune", which selects snapshots that are
// candidates for destruction without destroying anything, and zprune, which
// previews and destroys those candidates.
package prune

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/match"
	"github.com/bell-tower/zelta-go/internal/units"
)

// Guard modes for --prune-guard.
type guardMode int

const (
	guardNone guardMode = iota
	guardLatest
	guardUnsynced
)

// gridTerm is one [COUNT x]INTERVAL term of --prune-grid; count -1 covers
// all remaining history.
type gridTerm struct {
	count    int64
	interval int64
}

// settings are the parsed retention options.
type settings struct {
	guard     guardMode
	keep      int64
	keepSet   bool
	timeSecs  int64
	timeSet   bool
	grid      []gridTerm
	sizeBytes int64
	name      *regexp.Regexp
	sendRange string
}

// builtinNames maps --prune-name shortcuts to snapshot name globs.
var builtinNames = map[string]string{"zfsnap": "zfsnap_*", "truenas": "auto-*", "sanoid": "autosnap_*"}

func parseGuard(v string) (guardMode, error) {
	switch strings.ToLower(v) {
	case "", "latest", "1":
		return guardLatest, nil
	case "none", "0", "no", "false", "off":
		return guardNone, nil
	case "unsynced", "2":
		return guardUnsynced, nil
	}
	return 0, app.Fail("invalid prune-guard mode: %s", v)
}

// duration parses a duration. Like Zelta 1.2, a malformed number makes the
// value invalid (ok=false) while an unusable unit is a fatal error.
func duration(s string) (secs int64, ok bool, err error) {
	t := strings.Join(strings.Fields(strings.TrimLeft(s, "+-")), "")
	num := t
	if i := strings.IndexFunc(t, func(r rune) bool { return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }); i >= 0 {
		num = t[:i]
	}
	if num == "" || !units.IsNumeric(num) || strings.ContainsAny(num, "eE+-") {
		return 0, false, nil
	}
	secs, err = units.ParseDuration(s)
	if err != nil {
		return 0, false, app.Fail("%s", err.Error())
	}
	return secs, true, nil
}

// parseSettings reads PRUNE_* options. With no retention option given, the
// default strategy keeps 30 snapshots and everything from the last 30 days.
func parseSettings(o interface {
	Get(string) string
}) (*settings, error) {
	s := &settings{sendRange: o.Get("SEND_RANGE")}
	var err error
	if s.guard, err = parseGuard(o.Get("PRUNE_GUARD")); err != nil {
		return nil, err
	}
	num, tm, grid, size := o.Get("PRUNE_NUM"), o.Get("PRUNE_TIME"), o.Get("PRUNE_GRID"), o.Get("PRUNE_SIZE")
	if num == "" && tm == "" && grid == "" && size == "" {
		num, tm = "30", "30days"
	}
	if name := o.Get("PRUNE_NAME"); name != "" {
		if b, ok := builtinNames[name]; ok {
			name = b
		}
		s.name = match.GlobRegexp("@"+name, "")
	}
	if num != "" {
		n, perr := strconv.ParseInt(strings.TrimSpace(num), 10, 64)
		if perr != nil || n < 0 {
			return nil, app.Usage("invalid --prune-num: %s", num)
		}
		s.keep, s.keepSet = n, true
	}
	if tm != "" {
		secs, ok, err := duration(tm)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, app.Fail("invalid --prune-time: %s", tm)
		}
		s.timeSecs, s.timeSet = secs, true
	}
	if size != "" {
		b, ok := units.ParseSize(size)
		if !ok {
			return nil, app.Usage("invalid --prune-size: %s", size)
		}
		s.sizeBytes = b
	}
	if grid != "" {
		if s.grid, err = parseGrid(grid); err != nil {
			return nil, err
		}
	}
	return s, nil
}

var gridSpaceX = regexp.MustCompile(`[ \t]*x[ \t]*`)
var gridSep = regexp.MustCompile(`[,|]+`)

// parseGrid parses terms such as "30x1 day, 52x1 week, 1 year".
func parseGrid(grid string) ([]gridTerm, error) {
	var terms []gridTerm
	for _, term := range gridSep.Split(gridSpaceX.ReplaceAllString(grid, "x"), -1) {
		term = strings.TrimSpace(term)
		if term == "" {
			continue
		}
		gt := gridTerm{count: -1}
		interval := term
		if c, rest, ok := strings.Cut(term, "x"); ok {
			n, err := strconv.ParseInt(c, 10, 64)
			if err != nil || n < 0 || strings.ContainsAny(c, "+- ") {
				return nil, app.Usage("invalid --prune-grid term: %s", term)
			}
			gt.count, interval = n, rest
		}
		secs, ok, err := duration(interval)
		if err != nil {
			return nil, err
		}
		if !ok || secs == 0 {
			return nil, app.Usage("invalid --prune-grid term: %s", term)
		}
		gt.interval = secs
		terms = append(terms, gt)
	}
	return terms, nil
}

// gridKeeps reports whether the grid protects a snapshot created at
// creation: the newest snapshot (the first one seen, newest first) in each
// interval bucket, measured back from anchor, is kept.
func gridKeeps(grid []gridTerm, buckets map[[2]int64]bool, creation, anchor int64) bool {
	age := anchor - creation
	if len(grid) == 0 || age < 0 {
		return false
	}
	var start int64
	for g, t := range grid {
		if t.count == -1 {
			if age < start {
				return false
			}
			return claim(buckets, int64(g), (age-start)/t.interval)
		}
		end := start + t.count*t.interval
		if age >= start && age < end {
			return claim(buckets, int64(g), (age-start)/t.interval)
		}
		start = end
	}
	return false
}

func claim(buckets map[[2]int64]bool, term, bucket int64) bool {
	k := [2]int64{term, bucket}
	if buckets[k] {
		return false
	}
	buckets[k] = true
	return true
}

// entry is a snapshot in a keep or prune list.
type entry struct {
	savepoint string
	idx       int // position among all source snapshots, newest first
}

// Dataset is the plan for one source dataset.
type Dataset struct {
	// Suffix is relative to the endpoint.
	Suffix string
	// Name is the full dataset name.
	Name string
	src  *match.Dataset
	// prune and kept are newest first.
	prune, kept []entry
}

// Plan is the result of prune candidate selection.
type Plan struct {
	Source endpoint.Endpoint
	// Guard is the match endpoint, if any.
	Guard endpoint.Endpoint
	// Datasets lists every compared dataset in suffix order.
	Datasets []*Dataset
	// Empty is set when neither endpoint had datasets.
	Empty    bool
	noRanges bool
}

// Endpoints resolves the pruned endpoint and the optional match endpoint
// from operands and --match-endpoint.
func Endpoints(rt *app.Runtime, args []string) (src, guard endpoint.Endpoint, err error) {
	ops := append([]string{}, args...)
	if me := rt.Opts.Get("MATCH_ENDPOINT"); me != "" {
		if len(ops) > 1 && ops[1] != "" {
			return src, guard, app.Fail("cannot use --match-endpoint with a second positional operand")
		}
		if len(ops) == 0 {
			ops = append(ops, "")
		}
		ops = append(ops[:1], me)
	}
	src, guard, err = match.Endpoints(rt, nonEmpty(ops))
	return src, guard, err
}

func nonEmpty(ops []string) []string {
	if len(ops) > 0 && ops[0] == "" {
		return nil
	}
	return ops
}

// Build selects prune candidates. It returns a nil plan in dry-run mode
// (after logging the zfs list commands).
func Build(ctx context.Context, rt *app.Runtime, args []string, dryRun bool) (*Plan, error) {
	if _, err := parseGuard(rt.Opts.Get("PRUNE_GUARD")); err != nil {
		return nil, err
	}
	src, guard, err := Endpoints(rt, args)
	if err != nil {
		return nil, err
	}
	if err := match.CheckDepth(rt); err != nil {
		return nil, err
	}
	if src.DS == "" && guard.DS == "" {
		return nil, app.Usage("no datasets defined")
	}
	req := match.RequestFromOptions(rt, src, guard)
	req.Prune, req.DryRun = true, dryRun
	if dryRun {
		_, err := match.Compare(ctx, rt, req)
		return nil, err
	}
	set, err := parseSettings(rt.Opts)
	if err != nil {
		return nil, err
	}
	if guard.DS == "" {
		set.guard = guardNone
	}
	res, err := match.Compare(ctx, rt, req)
	if err != nil {
		return nil, err
	}
	now, err := clock(ctx, rt)
	if err != nil {
		return nil, err
	}
	plan := &Plan{Source: src, Guard: guard, Empty: len(res.Pairs) == 0, noRanges: rt.Opts.Bool("NO_RANGES")}
	for _, p := range res.Pairs {
		d := &Dataset{Suffix: p.Suffix, Name: src.DS + p.Suffix, src: res.Source.Dataset(p.Suffix)}
		if d.src != nil {
			d.Name = d.src.Name
			if set.sendRange != "" {
				d.sendRange(set.sendRange)
			} else {
				d.analyze(set, p, res.Target.Dataset(p.Suffix), now)
			}
		}
		plan.Datasets = append(plan.Datasets, d)
	}
	return plan, nil
}

// clock returns the current Unix time, honoring SYSTIME (a command that
// prints the time) for compatibility with deterministic test setups.
func clock(ctx context.Context, rt *app.Runtime) (int64, error) {
	cmd := rt.Opts.Get("SYSTIME")
	if cmd == "" {
		return rt.Unix(), nil
	}
	res := rt.Exec.Run(ctx, command.Script(cmd))
	for _, l := range res.Stdout() {
		if n, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64); err == nil {
			return n, nil
		}
	}
	return rt.Unix(), nil
}

// analyze applies scope filters, protections, and retention to the
// snapshots older than the protected match boundary.
func (d *Dataset) analyze(set *settings, p *match.Pair, tgt *match.Dataset, now int64) {
	snaps := d.src.Snaps
	if len(snaps) == 0 {
		return
	}
	matchIdx := p.MatchIdx
	anchor := snaps[0].Creation
	if matchIdx > 0 {
		anchor = snaps[matchIdx-1].Creation
	}
	minAge := now - set.timeSecs
	buckets := map[[2]int64]bool{}
	var eligible []*match.Row
	seen := int64(0)
	for s := matchIdx + 1; s <= len(snaps); s++ {
		r := snaps[s-1]
		if r.Kind != match.KindSnapshot {
			continue
		}
		keep := func() { d.kept = append(d.kept, entry{r.Savepoint, r.SnapIdx}) }
		if r.Filtered || (set.name != nil && !set.name.MatchString(r.Savepoint)) {
			keep()
			continue
		}
		seen++
		switch {
		case r.Clones != "" && r.Clones != "-":
			keep()
		case set.guard == guardUnsynced && (tgt.ByGUID(r.GUID) == nil || !tgt.HasSnapshotNamed(r.Savepoint)):
			keep()
		case len(set.grid) > 0 && (s == 1 || s == len(snaps) || gridKeeps(set.grid, buckets, r.Creation, anchor)),
			set.keepSet && set.keep > 0 && seen <= set.keep,
			set.timeSet && r.Creation >= minAge:
			keep()
		case set.sizeBytes > 0:
			eligible = append(eligible, r)
		default:
			d.prune = append(d.prune, entry{r.Savepoint, r.SnapIdx})
		}
	}
	// Select the oldest eligible snapshots until the estimated reclaim
	// reaches the target size.
	var selected []*match.Row
	var written int64
	for _, r := range slices.Backward(eligible) {
		selected = append(selected, r)
		written += r.Written
		if written-r.Referenced >= set.sizeBytes {
			break
		}
	}
	for _, s := range slices.Backward(selected) {
		d.prune = append(d.prune, entry{s.Savepoint, s.SnapIdx})
	}
}

// sendRange lists the selected snapshots newer than the named snapshot.
func (d *Dataset) sendRange(after string) {
	idx := -1
	for i, r := range d.src.Snaps {
		if r.Kind == match.KindSnapshot && r.Savepoint == after {
			idx = i
			break
		}
	}
	for i := 0; i < idx; i++ {
		if r := d.src.Snaps[i]; r.Kind == match.KindSnapshot && !r.Filtered {
			d.prune = append(d.prune, entry{r.Savepoint, r.SnapIdx})
		}
	}
}

// ranges compresses a newest-first list into zfs snapshot ranges of
// consecutive snapshots, returned oldest range first ("@old%new").
func ranges(list []entry) []string {
	var out []string
	for i := len(list) - 1; i >= 0; {
		start := list[i]
		end, prev := start, start.idx
		j := i - 1
		for ; j >= 0 && list[j].idx == prev-1; j-- {
			end, prev = list[j], list[j].idx
		}
		if end == start {
			out = append(out, start.savepoint)
		} else {
			out = append(out, start.savepoint+"%"+end.savepoint[1:])
		}
		i = j
	}
	return out
}

// Selected returns the snapshots ("@name") chosen for destruction, oldest
// first.
func (d *Dataset) Selected() []string { return savepoints(d.prune) }

// Protected returns the snapshots ("@name") older than the match boundary
// that retention keeps, oldest first.
func (d *Dataset) Protected() []string { return savepoints(d.kept) }

func savepoints(list []entry) []string {
	out := make([]string, 0, len(list))
	for _, l := range slices.Backward(list) {
		out = append(out, l.savepoint)
	}
	return out
}

// Candidates returns the snapshots or ranges to destroy, one per line, in
// the order Zelta has always printed them: individual snapshots oldest
// first, ranges newest range first.
func (p *Plan) Candidates() []string {
	var out []string
	for _, d := range p.Datasets {
		if p.noRanges {
			for _, v := range slices.Backward(d.prune) {
				out = append(out, d.Name+v.savepoint)
			}
			continue
		}
		r := ranges(d.prune)
		for _, v := range slices.Backward(r) {
			out = append(out, d.Name+v)
		}
	}
	return out
}

// Kept describes the protected snapshots older than each match boundary, as
// comma-separated names and ranges.
func (p *Plan) Kept() string {
	var parts []string
	for _, d := range p.Datasets {
		r := ranges(d.kept)
		for _, v := range slices.Backward(r) {
			parts = append(parts, p.Source.DS+d.Suffix+v)
		}
	}
	return strings.Join(parts, ",")
}

// Visual renders one line per dataset (its endpoint name) followed by one
// symbol per snapshot, oldest first: ❌ for candidates, 🔹 otherwise.
func (p *Plan) Visual() string {
	var b strings.Builder
	for _, d := range p.Datasets {
		fmt.Fprintf(&b, "%s%s\n", p.Source.ID, d.Suffix)
		kill := map[int]bool{}
		for _, e := range d.prune {
			kill[e.idx] = true
		}
		if d.src != nil {
			for _, r := range slices.Backward(d.src.Snaps) {
				if r.Kind == match.KindSnapshot {
					if kill[r.SnapIdx] {
						b.WriteString("❌")
					} else {
						b.WriteString("🔹")
					}
				}
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
