package backup

import (
	"zelta.space/internal/app"
	"zelta.space/internal/endpoint"
	"zelta.space/internal/match"
)

// validateSnapshots takes any needed source snapshot, loads the snapshot
// relationship from "zelta match", and requires at least one source snapshot.
func (r *run) validateSnapshots() error {
	if _, err := r.createSourceSnapshot(""); err != nil {
		return err
	}
	if err := r.loadSnapshotDeltas(); err != nil {
		return err
	}
	r.applyPredictedSnapshot()
	for _, suffix := range r.list {
		sd := r.dataset(src, suffix)
		switch {
		case sd.exists && sd.latest == "":
			r.tree.snapshotNeeded = snapMissing
		case r.verb == "rotate":
			if m := r.pair(suffix).match; m != "" && m == sd.latest {
				r.tree.snapshotNeeded = snapLatest
			}
		}
	}
	if _, err := r.createSourceSnapshot(""); err != nil {
		return err
	}
	r.applyPredictedSnapshot()
	for _, suffix := range r.list {
		if r.dataset(src, suffix).latest != "" {
			r.tree.sourceSnapNum++
		}
	}
	if r.tree.sourceSnapNum == 0 {
		return fail("source '%s' has no snapshots", r.src.ID)
	}
	return nil
}

// matchRequest describes the comparison "zelta backup" makes: no size
// columns, and IV set GUIDs when an encrypted source already has a replica.
func (r *run) matchRequest(target endpoint.Endpoint) match.Request {
	return match.Request{
		Source: r.src, Target: target, Depth: r.depth,
		Exclude: r.opts.Get("EXCLUDE"), Include: r.opts.Get("INCLUDE"),
		IVSet: r.tree.targetExists && r.tree.sourceEncrypted,
	}
}

// loadSnapshotDeltas compares the trees and records, per dataset, the match,
// the next and latest source snapshots, and the latest target snapshot.
func (r *run) loadSnapshotDeltas() error {
	r.rt.Log.Infof("checking replica deltas")
	target := r.tgt
	if !r.tree.targetExists {
		target = endpoint.Endpoint{}
	}
	res, err := match.Compare(r.ctx, r.matchRuntime(), r.matchRequest(target))
	if err != nil {
		return err
	}
	r.srcTree = res.Source
	r.sum.sourceListTime += res.Source.ListTime
	if res.Target.Listed {
		r.sum.targetListTime += res.Target.ListTime
	}
	for _, p := range res.Pairs {
		r.list = append(r.list, p.Suffix)
		dp := r.pair(p.Suffix)
		dp.match, dp.matchIVSet = p.Match, p.MatchIVSet
		sd := r.dataset(src, p.Suffix)
		sd.next, sd.latest = p.SrcNext, p.SrcLast
		r.dataset(tgt, p.Suffix).latest = p.TgtLast
	}
	return nil
}

// computeSendRange sets the "zfs send [-I start] end" snapshots.
func (r *run) computeSendRange(suffix string) {
	p := r.pair(suffix)
	sd := r.dataset(src, suffix)
	var final string
	switch {
	case r.tree.finalSnapshot != "":
		final = r.tree.finalSnapshot
	case r.opts.Bool("SEND_INTR") && p.match == "":
		final = sd.next
	case r.verb == "rotate":
		final = sd.next
	default:
		final = sd.latest
	}
	p.sourceStart, p.sourceEnd = p.match, final
}

// computeEligibility decides, per dataset, whether it can be synced or
// rotated and why not.
func (r *run) computeEligibility() {
	r.tree.syncable, r.tree.needsSnapshot, r.tree.upToDate = 0, 0, 0
	r.actions = map[string]*action{}
	for _, suffix := range r.list {
		sd, td := r.dataset(src, suffix), r.dataset(tgt, suffix)
		p := r.pair(suffix)
		a := r.action(suffix)
		r.computeSendRange(suffix)
		switch {
		case !sd.exists:
			a.statusInfo = "no source"
			r.tree.noSource++
		case sd.latest == "":
			a.statusNotice = "no source snapshot"
			r.tree.needsSnapshot++
		case !td.exists:
			a.canSync = true
			r.tree.syncable++
		case td.prop("receive_resume_token") != "":
			a.canSync, a.resumable = true, true
			r.tree.syncable++
		case td.latest == "":
			a.statusNotice = "no snapshot; target diverged"
		case p.match == "":
			a.statusNotice = "no common snapshot (diverged)"
			if sd.prop("origin") != "" {
				a.checkSourceOrigin = true
				r.tree.diverged++
			}
		case p.match == sd.latest || (r.tree.finalSnapshot != "" && p.match == r.tree.finalSnapshot):
			// Nothing to send: the match is the newest source snapshot or
			// the one named in the source endpoint.
			if w := td.prop("written"); w != "" && w != "0" {
				a.statusNotice = "target has local writes"
				continue
			}
			a.upToDate = true
			r.tree.upToDate++
		case p.match != td.latest:
			a.statusNotice = "target has diverged"
			a.canRotate = true
			r.tree.rotatable++
		default:
			a.canSync, a.canRotate = true, true
			if r.verb != "replicate" && truthy(sd.prop("encryption")) && p.matchIVSet == "" {
				a.sendDecrypted = true
			}
			r.tree.syncable++
			r.tree.rotatable++
		}
	}
}

func truthy(v string) bool { return v != "" && v != "0" }

// matchRuntime caps the comparison's log level at notices: the comparison is
// an internal step whose progress and debug messages Zelta never showed.
func (r *run) matchRuntime() *app.Runtime {
	mrt := *r.rt
	mrt.Log = r.rt.Log.WithLevel(min(r.rt.Log.Level(), 2))
	return &mrt
}
