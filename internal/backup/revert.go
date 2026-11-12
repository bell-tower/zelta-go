package backup

import (
	"sort"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/zfs"
)

// validateRevertTree checks, before anything is renamed, that every dataset
// below the source is selected and has the snapshot it will be reverted to.
// A rename moves every descendant, even ones excluded from the clone plan.
func (r *run) validateRevertTree() error {
	root := r.src.DS
	cmd := zfs.Command(r.rt, r.src, zfs.RemoteDefault, append(command.Words("list", "-Hpr", "-o", "name", "-t", "filesystem,volume,snapshot"), command.Name(root))...)
	res := r.rt.Run(r.ctx, cmd)
	available := map[string]bool{}
	datasets := map[string]bool{}
	invalid := false
	for _, l := range res.Lines {
		line := app.SanitizeLine(l.Text)
		if line == "" {
			continue
		}
		if line != root && !strings.HasPrefix(line, root+"/") && !strings.HasPrefix(line, root+"@") {
			invalid = true
			continue
		}
		available[line] = true
		if !strings.Contains(line, "@") {
			datasets[line] = true
		}
	}
	if !res.OK() || invalid || !datasets[root] {
		return fail("cannot validate revert tree; no datasets renamed")
	}
	selected := map[string]bool{}
	for _, suffix := range r.list {
		ds := root + suffix
		selected[ds] = true
		snap := r.src.Snap
		if snap == "" {
			snap = r.dataset(src, suffix).latest
		}
		if snap == "" {
			return fail("cannot revert tree: no snapshot for '%s'; revert eligible children individually", ds)
		}
		if !available[ds+snap] {
			return fail("cannot revert tree: missing snapshot '%s'; revert eligible children individually", ds+snap)
		}
	}
	names := make([]string, 0, len(datasets))
	for ds := range datasets {
		names = append(names, ds)
	}
	sort.Strings(names)
	for _, ds := range names {
		if !selected[ds] {
			return fail("cannot revert tree: unselected dataset '%s'; revert eligible children individually", ds)
		}
	}
	return nil
}

// runRevert rewinds the source by renaming it aside and cloning the selected
// snapshot back into place.
func (r *run) runRevert() error {
	if r.opts.Bool("REVERT_FORCE") {
		r.rt.Log.Warnf("forcing best-effort revert; failed or skipped datasets remain in the preserved tree")
	} else if err := r.validateRevertTree(); err != nil {
		return err
	}
	renamed, err := r.renameDataset(src)
	if err != nil {
		return err
	}
	incomplete, err := r.createRecursiveClone(src, renamed, r.src.DS)
	if err != nil {
		return err
	}
	if incomplete > 0 {
		r.sum.errorCode = 1
		return nil
	}
	reason := "snapshotting: "
	if r.dryRun() {
		reason = "would snapshot: "
	}
	if _, err := r.createSourceSnapshot(reason); err != nil {
		return err
	}
	followup := "zelta rotate '" + r.src.DS + "' 'TARGET'"
	if r.dryRun() {
		r.rt.Log.Noticef("would recommend: %s", followup)
	} else {
		r.rt.Log.Noticef("to retain replica history, run: %s", followup)
	}
	return nil
}
