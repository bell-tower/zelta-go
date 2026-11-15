// Package prune implements "zelta prune", which selects snapshots that are
// candidates for destruction without destroying anything, and zprune, which
// previews and destroys those candidates.
package prune

import (
	"context"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/match"
	"github.com/bell-tower/zelta-go/internal/units"
	"github.com/bell-tower/zelta-go/internal/zfs"
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
	// eligible holds --prune-size candidates, newest first.
	eligible []*match.Row
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
				if err := d.selectSize(ctx, rt, src, set.sizeBytes); err != nil {
					return nil, err
				}
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
			d.eligible = append(d.eligible, r)
		default:
			d.prune = append(d.prune, entry{r.Savepoint, r.SnapIdx})
		}
	}
}

// selectSize selects the oldest eligible snapshots until destroying them
// would free at least size bytes, or every eligible snapshot is selected.
//
// Eligible snapshots form runs separated by kept snapshots, so the runs'
// frees add up. Destroying a run s[a..b] frees the blocks born in the run
// that are gone by s[b+1], the next snapshot or the live dataset:
//
//	freed = Σ written(s[a..b+1]) − written@s[a−1] of s[b+1]
//
// When s[a] is the oldest snapshot the last term is referenced(s[b+1]),
// unless the dataset is a clone, where s[a−1] is its origin. Otherwise it
// takes a "zfs get written@s[a−1]": snapshot properties alone can't say how
// much of the run's data s[b+1] still holds. The listing does bound it,
//
//	Σ used(s[a..b]) ≤ freed ≤ Σ written(s[a..b])
//
// and a single snapshot frees exactly its used, so written@ is read only
// for the run lengths the bounds can't decide, a few at a time.
func (d *Dataset) selectSize(ctx context.Context, rt *app.Runtime, ep endpoint.Endpoint, size int64) error {
	if len(d.eligible) == 0 {
		return nil
	}
	eligible := make(map[*match.Row]bool, len(d.eligible))
	for _, r := range d.eligible {
		eligible[r] = true
	}
	// chain is every snapshot oldest first, then the live dataset.
	var chain []*match.Row
	for _, r := range slices.Backward(d.src.Snaps) {
		if r.Kind == match.KindSnapshot {
			chain = append(chain, r)
		}
	}
	chain = append(chain, d.src.Row)
	var selected []*match.Row
	var done int64
	for a := 0; a < len(chain)-1 && done < size; a++ {
		if !eligible[chain[a]] {
			continue
		}
		e := a
		for eligible[chain[e+1]] {
			e++
		}
		r := &sizeRun{ctx: ctx, rt: rt, ep: ep, ds: d.src.Name, chain: chain, a: a, e: e}
		n, freed, err := r.choose(size - done)
		if err != nil {
			return err
		}
		selected = append(selected, chain[a:a+n]...)
		done += freed
		a = e
	}
	for _, s := range slices.Backward(selected) {
		d.prune = append(d.prune, entry{s.Savepoint, s.SnapIdx})
	}
	return nil
}

// sizeRun measures a run chain[a..e] of eligible snapshots.
type sizeRun struct {
	ctx   context.Context
	rt    *app.Runtime
	ep    endpoint.Endpoint
	ds    string
	chain []*match.Row
	a, e  int

	from    string // snapshot written@ is relative to; "" for none
	fromSet bool
	base    map[string]int64 // written@from by name
	batch   int
}

// choose returns how many of the run's oldest snapshots to destroy to free
// need bytes, and at least how much they free (exactly, if not enough).
func (r *sizeRun) choose(need int64) (int, int64, error) {
	var written, used int64
	for b := r.a; b <= r.e; b++ {
		written += r.chain[b].Written
		used += r.chain[b].Used
		n := b - r.a + 1
		if b < r.e && written < need {
			continue
		}
		if used >= need {
			return n, used, nil
		}
		freed, err := r.freed(b)
		if err != nil || freed >= need || b == r.e {
			return n, freed, err
		}
	}
	return 0, 0, nil
}

// freed returns what destroying chain[a..b] frees.
func (r *sizeRun) freed(b int) (int64, error) {
	if b == r.a {
		return r.chain[b].Used, nil
	}
	var sum int64
	for _, s := range r.chain[r.a : b+1] {
		sum += s.Written
	}
	next := r.chain[b+1]
	base, err := r.since(b + 1)
	return sum + next.Written - base, err
}

// since returns written@from of chain[j], reading it with the following
// rows up to the end of the run, in batches that double.
func (r *sizeRun) since(j int) (int64, error) {
	if !r.fromSet {
		r.fromSet = true
		switch {
		case r.a > 0:
			r.from = strings.TrimPrefix(r.chain[r.a-1].Savepoint, "@")
		case r.chain[r.a].Written != r.chain[r.a].Referenced:
			// A clone's oldest snapshot still shares its origin's data.
			var err error
			if r.from, err = origin(r.ctx, r.rt, r.ep, r.ds); err != nil {
				return 0, err
			}
		}
	}
	row := r.chain[j]
	if r.from == "" {
		return row.Referenced, nil
	}
	if w, ok := r.base[row.Name]; ok {
		return w, nil
	}
	r.batch = max(4, 2*r.batch)
	rows := r.chain[j:min(j+r.batch, r.e+2)]
	got, err := writtenSince(r.ctx, r.rt, r.ep, r.from, rows)
	if err != nil {
		return 0, err
	}
	if r.base == nil {
		r.base = map[string]int64{}
	}
	maps.Copy(r.base, got)
	w, ok := r.base[row.Name]
	if !ok {
		return 0, app.Fail("cannot read written@%s for %s", r.from, row.Name)
	}
	return w, nil
}

// origin returns the full name of a clone's origin snapshot.
func origin(ctx context.Context, rt *app.Runtime, ep endpoint.Endpoint, ds string) (string, error) {
	res := rt.Run(ctx, zfs.Command(rt, ep, zfs.RemoteDefault, append(command.Words("get", "-Hpo", "value", "origin"), command.Name(ds))...))
	if out := res.Stdout(); res.OK() && len(out) == 1 && strings.Contains(out[0], "@") {
		return out[0], nil
	}
	return "", app.Fail("cannot read origin for %s", ds)
}

// writtenSince reads "written@from" for rows. From is a short snapshot
// name or, for a clone, its origin's full name.
func writtenSince(ctx context.Context, rt *app.Runtime, ep endpoint.Endpoint, from string, rows []*match.Row) (map[string]int64, error) {
	args := command.Words("get", "-Hpo", "name,value", "written@"+from)
	for _, r := range rows {
		args = append(args, command.Name(r.Name))
	}
	res := rt.Run(ctx, zfs.Command(rt, ep, zfs.RemoteDefault, args...))
	if !res.OK() {
		return nil, app.Fail("cannot read written@%s for %s", from, rows[0].Name)
	}
	// zfs get doesn't print in argument order.
	out := map[string]int64{}
	for _, l := range res.Stdout() {
		if f := strings.Split(l, "\t"); len(f) == 2 {
			if n, err := strconv.ParseInt(f[1], 10, 64); err == nil {
				out[f[0]] = n
			}
		}
	}
	return out, nil
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

// guids maps each of the dataset's snapshots ("@name") to its GUID.
func (d *Dataset) guids() map[string]string {
	out := map[string]string{}
	if d.src != nil {
		for _, r := range d.src.Snaps {
			if r.Kind == match.KindSnapshot {
				out[r.Savepoint] = r.GUID
			}
		}
	}
	return out
}

// SelectedGUIDs maps each selected snapshot ("pool/ds@name") to its GUID.
func (p *Plan) SelectedGUIDs() map[string]string {
	out := map[string]string{}
	for _, d := range p.Datasets {
		g := d.guids()
		for _, e := range d.prune {
			out[d.Name+e.savepoint] = g[e.savepoint]
		}
	}
	return out
}

// Restrict keeps only the selected snapshots that are approved: approved
// maps "pool/ds@name" to the GUID the approver saw, so a snapshot that was
// replaced under the same name is not approved. Ranges are rebuilt from
// this plan's listing, so an unapproved snapshot inside an approved range
// splits it. Restrict returns the approved snapshots it doesn't select,
// sorted.
func (p *Plan) Restrict(approved map[string]string) []string {
	left := maps.Clone(approved)
	for _, d := range p.Datasets {
		g := d.guids()
		var keep []entry
		for _, e := range d.prune {
			name := d.Name + e.savepoint
			if guid, ok := approved[name]; ok && guid != "" && guid == g[e.savepoint] {
				keep = append(keep, e)
				delete(left, name)
			}
		}
		d.prune = keep
	}
	return slices.Sorted(maps.Keys(left))
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
