// Package match compares a ZFS dataset tree (the source) with its replica
// (the target) by snapshot GUID, which drives "zelta match", "zelta prune",
// and the replication planning of "zelta backup".
package match

import (
	"context"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"zelta.space/internal/app"
	"zelta.space/internal/command"
	"zelta.space/internal/endpoint"
	"zelta.space/internal/zfs"
)

// Kind is the type of a listed object.
type Kind uint8

// Object kinds.
const (
	KindUnknown Kind = iota
	KindDataset
	KindSnapshot
	KindBookmark
)

// Row is one object from "zfs list".
type Row struct {
	// Name is the full object name.
	Name string
	// Suffix is the dataset suffix relative to the endpoint ("" for the top).
	Suffix string
	// Savepoint is "@snap" or "#bookmark", empty for datasets.
	Savepoint string
	Kind      Kind
	GUID      string
	IVSetGUID string
	Written   int64
	Creation  int64
	Used      int64
	// Referenced and Clones are only listed for prune.
	Referenced int64
	Clones     string
	// SnapIdx is the 1-based position of a source snapshot among all of its
	// dataset's snapshots (newest first), counted before filtering.
	SnapIdx int
	// Filtered marks a source snapshot excluded by filters but kept for
	// prune so it stays visible.
	Filtered bool
}

// Dataset is a listed dataset with its snapshots and bookmarks.
type Dataset struct {
	*Row
	// Snaps holds snapshots and bookmarks, newest first.
	Snaps []*Row
	guids map[string]*Row
	all   int
}

// ByGUID returns the snapshot (preferred) or bookmark with the given GUID.
func (d *Dataset) ByGUID(guid string) *Row {
	if d == nil {
		return nil
	}
	return d.guids[guid]
}

// HasSnapshotNamed reports whether the dataset has a snapshot or bookmark
// with this savepoint.
func (d *Dataset) HasSnapshotNamed(savepoint string) bool {
	if d == nil {
		return false
	}
	for _, s := range d.Snaps {
		if s.Savepoint == savepoint {
			return true
		}
	}
	return false
}

// Tree is the listing of one endpoint.
type Tree struct {
	Endpoint endpoint.Endpoint
	// Datasets are in listing order (newest first).
	Datasets []*Dataset
	bySuffix map[string]*Dataset
	// pending holds snapshot rows for datasets whose own row was filtered.
	pending map[string]*Dataset
	// ListTime is the wall-clock time of the zfs list, in seconds.
	ListTime float64
	// Listed reports whether the endpoint was listed at all.
	Listed bool
}

// Dataset returns the dataset with the given suffix, or nil.
func (t *Tree) Dataset(suffix string) *Dataset {
	if t == nil {
		return nil
	}
	return t.bySuffix[suffix]
}

func (t *Tree) snapsOf(suffix string) *Dataset {
	if d := t.bySuffix[suffix]; d != nil {
		return d
	}
	d := t.pending[suffix]
	if d == nil {
		d = &Dataset{guids: map[string]*Row{}}
		t.pending[suffix] = d
	}
	return d
}

// Status describes whether a dataset exists on each side.
type Status uint8

// Pair statuses.
const (
	StatusUnknown Status = iota
	StatusExists
	StatusSourceOnly
	StatusTargetOnly
)

// SyncState classifies a pair for replication.
type SyncState uint8

// Sync states.
const (
	SyncUnknown SyncState = iota
	SyncUpToDate
	SyncSyncable
	SyncBlocked
)

// Pair is the comparison of one dataset suffix. Its fields correspond to
// "zelta match" columns.
type Pair struct {
	Suffix     string
	Status     Status
	State      SyncState
	Match      string
	MatchIVSet string
	// MatchIdx is the source snapshot index (1-based, newest first) of the
	// match, or 0 when there is none.
	MatchIdx   int
	NumMatches int
	XferSize   int64
	XferNum    int
	SrcName    string
	SrcFirst   string
	SrcNext    string
	SrcLast    string
	SrcWritten string
	SrcSnaps   int
	TgtName    string
	TgtFirst   string
	TgtNext    string
	TgtLast    string
	TgtWritten string
	TgtSnaps   int
	NumBlocked int
	Info       string
}

// Request describes a comparison.
type Request struct {
	Source, Target endpoint.Endpoint
	// Depth limits recursion; 0 is unlimited.
	Depth int
	// Exclude and Include are comma-separated filter patterns.
	Exclude, Include string
	// Written lists written, creation, and used.
	Written bool
	// IVSet lists ivsetguid to compare encrypted snapshot chains.
	IVSet bool
	// Prune lists prune columns and keeps filtered source snapshots.
	Prune bool
	// DryRun only logs the zfs list commands.
	DryRun bool
}

// Result is a completed comparison.
type Result struct {
	Source, Target *Tree
	// Pairs are sorted by suffix.
	Pairs []*Pair
	// Counts of pair states.
	UpToDate, Syncable, Blocked int
	// Summary is e.g. "5 up-to-date, 2 syncable, 4 blocked".
	Summary string
}

// Pair returns the pair for suffix, or nil.
func (r *Result) Pair(suffix string) *Pair {
	i := sort.Search(len(r.Pairs), func(i int) bool { return r.Pairs[i].Suffix >= suffix })
	if i < len(r.Pairs) && r.Pairs[i].Suffix == suffix {
		return r.Pairs[i]
	}
	return nil
}

// RequestFromOptions builds a request from DEPTH, EXCLUDE, INCLUDE,
// LIST_WRITTEN, PARSABLE, and PROPLIST.
func RequestFromOptions(rt *app.Runtime, src, tgt endpoint.Endpoint) Request {
	o := rt.Opts
	req := Request{Source: src, Target: tgt, Depth: o.Int("DEPTH"), Exclude: o.Get("EXCLUDE"), Include: o.Get("INCLUDE"), Written: o.Bool("LIST_WRITTEN"), DryRun: rt.DryRun()}
	if req.Written && o.Has("PROPLIST") && o.Bool("PARSABLE") {
		pl := o.Get("PROPLIST")
		if !strings.Contains(pl, "all") && !strings.Contains(pl, "written") && !strings.Contains(pl, "size") {
			req.Written = false
		}
	}
	return req
}

// ListProps returns the zfs list properties requested for an endpoint.
func (req Request) ListProps(source bool) []string {
	props := []string{"name", "guid"}
	if req.IVSet {
		props = append(props, "ivsetguid")
	}
	switch {
	case req.Prune:
		props = append(props, "written", "creation", "used", "referenced")
		if source {
			props = append(props, "clones")
		}
	case req.Written:
		props = append(props, "written", "creation", "used")
	}
	return props
}

// ListCommand returns the zfs list command for an endpoint.
func (req Request) ListCommand(rt *app.Runtime, ep endpoint.Endpoint, source bool) command.Cmd {
	args := []command.Arg{command.Word("list"), command.Word("-Hpr"), command.Word("-o"), command.Word(strings.Join(req.ListProps(source), ",")),
		command.Word("-t"), command.Word("all"), command.Word("-Screatetxg")}
	if req.Depth > 0 {
		args = append(args, command.Word("-d"+strconv.Itoa(req.Depth)))
	}
	return zfs.Command(rt, ep, zfs.RemoteDefault, append(args, command.Name(ep.DS))...)
}

// Compare lists both endpoints concurrently and compares them. A zero
// Target compares the source against nothing. In dry-run mode the list
// commands are logged and a nil result is returned.
func Compare(ctx context.Context, rt *app.Runtime, req Request) (*Result, error) {
	filter := NewFilter(req.Exclude, req.Include)
	if req.DryRun {
		for i, ep := range []endpoint.Endpoint{req.Source, req.Target} {
			if ep.DS != "" {
				rt.Log.Noticef("+ %s", req.ListCommand(rt, ep, i == 0).String())
			}
		}
		return nil, nil
	}
	for _, w := range filter.Warnings {
		rt.Log.Warnf("%s", w)
	}
	src := newTree(req.Source)
	tgt := newTree(req.Target)
	var wg sync.WaitGroup
	if req.Source.DS != "" {
		rt.Log.Infof("listing source: %s", req.Source.ID)
		wg.Go(func() { list(ctx, rt, req, filter, src, true) })
	}
	if req.Target.DS != "" {
		if req.Target.ID == req.Source.ID {
			rt.Log.Warnf("identical source and target; skipping 'zfs list' for target")
		} else {
			rt.Log.Infof("listing target: %s", req.Target.ID)
			wg.Go(func() { list(ctx, rt, req, filter, tgt, false) })
		}
	}
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	res := &Result{Source: src, Target: tgt}
	res.compare(req)
	res.classify()
	return res, nil
}

func newTree(ep endpoint.Endpoint) *Tree {
	return &Tree{Endpoint: ep, bySuffix: map[string]*Dataset{}, pending: map[string]*Dataset{}}
}

func list(ctx context.Context, rt *app.Runtime, req Request, filter *Filter, t *Tree, source bool) {
	cmd := req.ListCommand(rt, t.Endpoint, source)
	start := time.Now()
	res := rt.Run(ctx, cmd)
	t.ListTime = math.Round(time.Since(start).Seconds()*100) / 100
	t.Listed = true
	props := req.ListProps(source)
	for _, l := range res.Lines {
		line := app.SanitizeLine(l.Text)
		if zfs.DoesNotExist(line) {
			continue
		}
		f := zfs.Fields(line)
		if len(f) >= 2 && isDigits(f[1]) {
			t.add(rowFrom(props, f), req, filter, source)
		} else if line != "" {
			rt.Log.Warnf("stream output unexpected: %s", line)
		}
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func rowFrom(props []string, f []string) *Row {
	r := &Row{}
	for i, p := range props {
		if i >= len(f) {
			break
		}
		v := f[i]
		switch p {
		case "name":
			r.Name = v
		case "guid":
			r.GUID = v
		case "ivsetguid":
			if v != "-" {
				r.IVSetGUID = v
			}
		case "written":
			r.Written = num(v)
		case "creation":
			r.Creation = num(v)
		case "used":
			r.Used = num(v)
		case "referenced":
			r.Referenced = num(v)
		case "clones":
			r.Clones = v
		}
	}
	return r
}

func num(s string) int64 {
	n, _ := strconv.ParseInt(s, 10, 64)
	return n
}

// depthOK applies the -d limit to a suffix the way Zelta always has: the top
// dataset has depth 0, "/a" depth 2, and "/a/b" depth 3.
func depthOK(suffix string, depth int) bool {
	if depth <= 0 || suffix == "" {
		return true
	}
	return strings.Count(suffix, "/")+1 <= depth
}

func (t *Tree) add(r *Row, req Request, filter *Filter, source bool) {
	rest := strings.TrimPrefix(r.Name, t.Endpoint.DS)
	if i := strings.IndexAny(rest, "@#"); i >= 0 {
		r.Suffix, r.Savepoint = rest[:i], rest[i:]
		if rest[i] == '@' {
			r.Kind = KindSnapshot
		} else {
			r.Kind = KindBookmark
		}
	} else {
		r.Suffix, r.Kind = rest, KindDataset
	}
	if !depthOK(r.Suffix, req.Depth) {
		return
	}
	if r.Kind == KindDataset {
		if !filter.Dataset(r.Name, r.Suffix) {
			return
		}
		d := t.pending[r.Suffix]
		if d == nil {
			d = &Dataset{guids: map[string]*Row{}}
		} else {
			delete(t.pending, r.Suffix)
		}
		d.Row = r
		t.Datasets = append(t.Datasets, d)
		t.bySuffix[r.Suffix] = d
		return
	}
	d := t.snapsOf(r.Suffix)
	if source && r.Kind == KindSnapshot {
		d.all++
		r.SnapIdx = d.all
		dsName := strings.TrimSuffix(r.Name, r.Savepoint)
		if !filter.Snapshot(r.Savepoint, dsName, r.Suffix) {
			if !req.Prune {
				return
			}
			r.Filtered = true
		}
	}
	if !r.Filtered {
		if prev := d.guids[r.GUID]; prev == nil || r.Kind == KindSnapshot {
			d.guids[r.GUID] = r
		}
	}
	d.Snaps = append(d.Snaps, r)
}

func (res *Result) compare(req Request) {
	pairs := map[string]*Pair{}
	newPair := func(suffix string) *Pair {
		p := &Pair{Suffix: suffix}
		s, t := res.Source.Dataset(suffix), res.Target.Dataset(suffix)
		if s != nil {
			p.SrcName, p.SrcWritten, p.SrcSnaps = s.Name, writtenStr(s.Row, req), len(s.Snaps)
			if n := len(s.Snaps); n > 0 {
				p.SrcFirst, p.SrcLast = s.Snaps[n-1].Savepoint, s.Snaps[0].Savepoint
			}
		}
		if t != nil {
			p.TgtName, p.TgtWritten, p.TgtSnaps = t.Name, writtenStr(t.Row, req), len(t.Snaps)
			if n := len(t.Snaps); n > 0 {
				p.TgtFirst, p.TgtLast = t.Snaps[n-1].Savepoint, t.Snaps[0].Savepoint
			}
		}
		pairs[suffix] = p
		res.Pairs = append(res.Pairs, p)
		return p
	}

	for _, sd := range res.Source.Datasets {
		p := newPair(sd.Suffix)
		td := res.Target.Dataset(sd.Suffix)
		if td != nil {
			p.Status = StatusExists
		} else {
			p.Status = StatusSourceOnly
		}
		for i, s := range sd.Snaps {
			if s.Filtered {
				continue
			}
			if m := td.ByGUID(s.GUID); m != nil {
				if m.Kind != KindSnapshot {
					continue
				}
				if p.NumMatches == 0 {
					p.Match, p.MatchIdx = s.Savepoint, i+1
					if req.IVSet && s.IVSetGUID != "" && s.IVSetGUID == m.IVSetGUID {
						p.MatchIVSet = s.IVSetGUID
					}
				}
				p.NumMatches++
			} else if p.Match == "" {
				p.SrcNext = s.Savepoint
				p.XferNum++
				p.XferSize += s.Written
			}
		}
	}
	for _, td := range res.Target.Datasets {
		p := pairs[td.Suffix]
		if p == nil {
			p = newPair(td.Suffix)
			p.Status = StatusTargetOnly
		}
		sd := res.Source.Dataset(td.Suffix)
		if sd == nil {
			sd = res.Source.pending[td.Suffix]
		}
		found := false
		for _, s := range td.Snaps {
			if sd.ByGUID(s.GUID) != nil {
				found = true
			}
			if !found && s.Kind == KindSnapshot {
				p.NumBlocked++
				p.TgtNext = s.Savepoint
			}
		}
	}
	sort.SliceStable(res.Pairs, func(i, j int) bool { return res.Pairs[i].Suffix < res.Pairs[j].Suffix })
}

func writtenStr(r *Row, req Request) string {
	if r == nil || !(req.Written || req.Prune) {
		return ""
	}
	return strconv.FormatInt(r.Written, 10)
}

func (res *Result) classify() {
	for _, p := range res.Pairs {
		switch p.Status {
		case StatusTargetOnly:
			p.Info = "no source (target only)"
			p.State = SyncBlocked
			res.Blocked++
			continue
		case StatusSourceOnly:
			p.Info = "syncable (full)"
			p.State = SyncSyncable
			res.Syncable++
			continue
		}
		var blocked string
		switch {
		case p.TgtSnaps == 0 && p.SrcSnaps > 0:
			blocked = "no target snapshots"
		case p.Match != p.TgtLast:
			blocked = "target diverged"
		case p.TgtWritten != "" && p.TgtWritten != "0":
			blocked = "target is written"
		}
		switch {
		case blocked != "":
			p.Info = "blocked sync: " + blocked
			p.State = SyncBlocked
			res.Blocked++
		case p.Match == p.SrcLast:
			p.Info = "up-to-date"
			p.State = SyncUpToDate
			res.UpToDate++
		default:
			p.Info = "syncable (incremental)"
			p.State = SyncSyncable
			res.Syncable++
		}
	}
	var parts []string
	if res.UpToDate > 0 {
		parts = append(parts, strconv.Itoa(res.UpToDate)+" up-to-date")
	}
	if res.Syncable > 0 {
		parts = append(parts, strconv.Itoa(res.Syncable)+" syncable")
	}
	if res.Blocked > 0 {
		parts = append(parts, strconv.Itoa(res.Blocked)+" blocked")
	}
	res.Summary = strings.Join(parts, ", ")
}

// SendRange returns the selected snapshots of a source dataset that are newer
// than the snapshot named after, oldest first, for stepwise replication of a
// filtered snapshot stream. It returns nil when after is not a snapshot of
// the dataset.
func (t *Tree) SendRange(suffix, after string) []string {
	d := t.Dataset(suffix)
	if d == nil {
		return nil
	}
	idx := -1
	for i, s := range d.Snaps {
		if s.Kind == KindSnapshot && s.Savepoint == after {
			idx = i
			break
		}
	}
	var out []string
	for i := idx - 1; i >= 0; i-- {
		if s := d.Snaps[i]; s.Kind == KindSnapshot && !s.Filtered {
			out = append(out, s.Savepoint)
		}
	}
	return out
}
