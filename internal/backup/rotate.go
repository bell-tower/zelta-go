package backup

import (
	"context"
	"regexp"
	"strings"

	"zelta.space/internal/app"
	"zelta.space/internal/command"
	"zelta.space/internal/zfs"
)

var cannotRenameRe = regexp.MustCompile(`cannot (rename|unmount)`)

// renameDataset moves an endpoint's dataset aside, naming it after the
// requested snapshot, its latest snapshot, or the new snapshot name, and
// returns the new name.
func (r *run) renameDataset(s side) (string, error) {
	ep := r.endpoint(s)
	unique := ep.Snap
	if unique == "" {
		unique = r.dataset(s, "").latest
	}
	if unique == "" {
		unique = r.resolveSnapName()
	}
	newDS := ep.DS + strings.ReplaceAll(unique, "@", "_")
	cmd := zfs.Command(r.rt, ep, zfs.RemoteDefault, command.Word("rename"), command.Word("-fp"), command.Name(ep.DS), command.Name(newDS))
	if r.dryRun() {
		r.rt.Log.Noticef("would rename '%s' to '%s'", ep.DS, newDS)
		r.rt.Log.Noticef("+ %s", cmd.String())
		return newDS, nil
	}
	r.rt.Log.Noticef("renaming '%s' to '%s'", ep.DS, newDS)
	res := r.rt.Run(r.ctx, cmd)
	for _, l := range res.Lines {
		line := app.SanitizeLine(l.Text)
		if cannotRenameRe.MatchString(line) {
			return "", fail("%s", line)
		}
		r.rt.Log.Errorf("unexpected 'zfs rename' output: %s", line)
	}
	if !res.OK() {
		return "", fail("cannot rename '%s' to '%s'", ep.DS, newDS)
	}
	return newDS, nil
}

var missingSnapRe = regexp.MustCompile(`'[^']*':`)

// checkOriginMatch uses each diverged source clone's origin snapshot as the
// match when the target still has that snapshot.
func (r *run) checkOriginMatch() {
	var names []command.Arg
	for _, suffix := range r.list {
		origin := r.dataset(src, suffix).prop("origin")
		p := r.pair(suffix)
		if !r.dataset(tgt, suffix).exists || p.match != "" {
			continue
		}
		parts := strings.Split(origin, "@")
		if len(parts) != 2 {
			continue
		}
		originSnap := "@" + parts[1]
		p.sourceOriginMatch = origin
		p.match = originSnap
		r.action(suffix).canRotate = true
		names = append(names, command.Name(r.tgt.DS+suffix+originSnap))
		r.tree.rotatable++
	}
	if len(names) == 0 {
		return
	}
	r.rt.Log.Infof("confirming source origin deltas")
	args := append(command.Words("list", "-Ho", "name"), names...)
	res := r.rt.Run(r.ctx, zfs.Command(r.rt, r.tgt, zfs.RemoteDefault, args...))
	for _, l := range res.Lines {
		line := app.SanitizeLine(l.Text)
		if zfs.DoesNotExist(line) {
			r.rt.Log.Warnf("%s is missing and will fail to clone", missingSnapRe.FindString(line))
		}
	}
}

// runRotate renames a diverged target out of the way and clones the replica
// forward from the latest match, the source's clone origin, or a full sync.
func (r *run) runRotate() error {
	n := len(r.list)
	srcSnap := r.src.DS + r.pair("").match
	canRotate := n == r.tree.rotatable
	upToDate := n == r.tree.upToDate
	fullBackups := n - r.tree.rotatable - r.tree.noSource
	originDS, _, _ := strings.Cut(r.dataset(src, "").prop("origin"), "@")

	switch {
	case canRotate:
		r.rt.Log.Noticef("rotating from source: %s", srcSnap)
	case r.tree.diverged > 0:
		r.checkOriginMatch()
		if n == r.tree.rotatable {
			r.rt.Log.Noticef("rotating from source origin: %s", originDS)
		}
	case upToDate:
		return fail("replica is up-to-date; source snapshot required for rotation: %s", r.src.DS)
	case r.tree.rotatable > 0:
		r.rt.Log.Warnf("insufficient snapshots; performing full backup for %d datasets", fullBackups)
	default:
		r.rt.Log.Errorf("target is not a replica of the source")
		return fail("to perform a full backup, rename the target dataset or sync to an empty target")
	}

	targetOrigin, err := r.renameDataset(tgt)
	if err != nil {
		return err
	}
	for _, suffix := range r.list {
		if p := r.pair(suffix); p.match != "" {
			p.targetOrigin = targetOrigin
		} else if r.dataset(src, suffix).exists {
			r.action(suffix).canRotate = true
		}
		r.runSync(suffix)
	}

	// Reload the target for the final summary.
	if r.dryRun() {
		r.rt.Log.Noticef("to ensure target is up-to-date, run: zelta backup %s %s", r.src.ID, r.tgt.ID)
		return nil
	}
	srcDS, oldList := r.ds[src], r.list
	r.ds[tgt] = map[string]*dataset{}
	r.pairs = map[string]*pair{}
	r.list = nil
	snapNum := r.tree.sourceSnapNum
	r.tree = treeState{finalSnapshot: r.src.Snap, sourcePool: r.src.Pool, targetPool: r.tgt.Pool, sourceEncrypted: r.tree.sourceEncrypted, sourceSnapNum: snapNum}
	r.ds[src] = srcDS
	if err := r.validateTargetDataset(); err != nil {
		return err
	}
	if err := r.loadSnapshotDeltas(); err != nil {
		return err
	}
	// Datasets that left the target tree (renamed away) still count in the
	// final summary, so it never claims they are up to date.
	seen := map[string]bool{}
	for _, s := range r.list {
		seen[s] = true
	}
	for _, s := range oldList {
		if !seen[s] {
			r.list = append(r.list, s)
		}
	}
	r.rt.Log.Noticef("to ensure target is up-to-date, run: zelta backup %s %s", r.src.ID, r.tgt.ID)
	return nil
}

// Rebase implements "zelta rebase": a rotate of TARGET from an upgraded
// UPSTREAM on the same host.
func Rebase(ctx context.Context, rt *app.Runtime, args []string) error {
	if rt.Opts.Bool("USAGE") {
		return app.Usage("")
	}
	if rt.Opts.Has("REBASE_FILE") {
		return fail("--rebase-file is not supported by this rebase workflow")
	}
	if len(args) != 2 {
		return app.Usage("zelta rebase requires UPSTREAM and TARGET")
	}
	upstream, target := rt.Endpoint(args[0]), rt.Endpoint(args[1])
	if upstream.Remote != target.Remote {
		return fail("rebase requires UPSTREAM and TARGET on the same host")
	}
	rt.Log.Noticef("rebasing %s from %s", target.ID, upstream.ID)
	opts := rt.Opts.Clone()
	opts.Set("VERB", "rotate", opts.Source("VERB"))
	inner := *rt
	inner.Opts = opts
	if err := Run(ctx, &inner, args); err != nil {
		innerError(rt, err)
		return fail("rebase failed")
	}
	return nil
}
