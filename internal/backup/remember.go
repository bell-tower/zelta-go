package backup

import (
	"errors"
	"strconv"
	"strings"

	"github.com/bell-tower/zelta-go/internal/cache"
)

// REMEMBER skips reading the target: the last snapshot this host synced to
// each target dataset is remembered in the cache, and the next sync sends
// incrementally from it. That is safe because Zelta never forces a receive:
// if the target diverged, was written, lost the snapshot, or holds a partial
// receive, "zfs receive" refuses the stream at once, and the run starts over
// with a full comparison of both sides.

// errStale reports that the remembered target state did not hold.
var errStale = errors.New("remembered target state is stale")

// memory is the remembered target state of a tree, by dataset suffix.
type memory struct {
	Datasets map[string]memorySnap `json:"datasets"`
}

type memorySnap struct {
	Snap  string `json:"snap"`
	GUID  string `json:"guid"`
	IVSet string `json:"ivset,omitempty"`
}

func (r *run) rememberEnabled() bool {
	return r.opts.Bool("REMEMBER") && r.rt.Cache != nil && !r.dryRun() &&
		(r.verb == "backup" || r.verb == "sync") && !r.forcedReceive() &&
		!r.filteredIntermediate() && !r.bookmarkMode()
}

// forcedReceive reports whether any receive option could force a receive
// ("zfs receive -F"), which would defeat the refusal REMEMBER relies on.
func (r *run) forcedReceive() bool {
	if r.opts.Has("RECV_OVERRIDE") {
		return true
	}
	for _, k := range r.opts.Keys() {
		if !strings.HasPrefix(k, "RECV_") {
			continue
		}
		for w := range strings.FieldsSeq(r.opts.Get(k)) {
			if strings.HasPrefix(w, "-") && !strings.HasPrefix(w, "--") && strings.Contains(w, "F") {
				return true
			}
		}
	}
	return false
}

func (r *run) memoryKey() string {
	return "remember/" + cache.Key(r.src.ID, r.tgt.ID, strconv.Itoa(r.depth), r.opts.Get("EXCLUDE"), r.opts.Get("INCLUDE"))
}

// recall loads the remembered target state, if any.
func (r *run) recall() {
	if r.noMemory || !r.rememberEnabled() {
		return
	}
	var m memory
	if r.rt.Cache.Fresh(r.memoryKey(), &m) && len(m.Datasets) > 0 {
		r.memory = &m
		r.rt.Log.Infof("using remembered target state for %s", r.tgt.ID)
	}
}

// recallTarget stands in for listing the target's properties.
func (r *run) recallTarget() {
	r.tree.targetExists = true
	for suffix := range r.memory.Datasets {
		d := r.dataset(tgt, suffix)
		d.exists = true
		d.props["written"] = "0"
	}
}

// recallMatches stands in for comparing the target's snapshots. Every
// source dataset must be remembered, and every remembered snapshot must
// still exist on the source with the same GUID.
func (r *run) recallMatches() error {
	n := 0
	for _, suffix := range r.list {
		if !r.dataset(src, suffix).exists {
			continue
		}
		n++
		m, ok := r.memory.Datasets[suffix]
		var name string
		if ok && r.srcTree != nil {
			if row := r.srcTree.Dataset(suffix).ByGUID(m.GUID); row != nil && row.Savepoint != "" && row.Savepoint[0] == '@' {
				name = row.Savepoint
			}
		}
		if name == "" {
			return errStale
		}
		p := r.pair(suffix)
		p.match, p.matchIVSet = name, m.IVSet
		r.dataset(tgt, suffix).latest = name
	}
	if n != len(r.memory.Datasets) {
		return errStale
	}
	return nil
}

// memorize saves the target state after a clean run, or forgets it.
func (r *run) memorize() {
	if !r.rememberEnabled() {
		return
	}
	m := memory{Datasets: map[string]memorySnap{}}
	ok := r.sum.errorCode == 0
	for _, suffix := range r.list {
		if !ok {
			break
		}
		sd, td := r.dataset(src, suffix), r.dataset(tgt, suffix)
		if !sd.exists {
			continue
		}
		a := r.action(suffix)
		if !a.upToDate && !r.synced[suffix] || td.latest == "" {
			ok = false
			break
		}
		guid, ivset := "", ""
		if ds := r.srcTree.Dataset(suffix); ds != nil {
			for _, s := range ds.Snaps {
				if s.Savepoint == td.latest {
					guid, ivset = s.GUID, s.IVSetGUID
				}
			}
		}
		// An encrypted tree is only remembered while it replicates raw
		// from a known IV set; otherwise the next run compares again.
		if !truthy(sd.prop("encryption")) {
			ivset = ""
		} else if ivset == "" || r.pair(suffix).matchIVSet == "" || a.sendDecrypted {
			ok = false
			break
		}
		if guid == "" {
			ok = false
			break
		}
		m.Datasets[suffix] = memorySnap{Snap: td.latest, GUID: guid, IVSet: ivset}
	}
	if !ok || len(m.Datasets) == 0 {
		r.rt.Cache.Delete(r.memoryKey())
		return
	}
	_ = r.rt.Cache.Save(r.memoryKey(), m)
}
