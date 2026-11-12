package backup

import (
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/zfs"
)

// quoteName renders a name the way Zelta's messages always have: single
// quotes, with spaces escaped for remote endpoints.
func quoteName(remote bool, s string) string {
	if remote {
		s = strings.ReplaceAll(s, " ", `\ `)
	}
	return "'" + s + "'"
}

// cloneFlags are the "zfs clone" options: CLONE_DEFAULT when configured,
// otherwise Zelta's fixed "-p -o readonly=off".
func (r *run) cloneFlags() []command.Arg {
	if r.opts.Source("CLONE_DEFAULT") > config.FromDefault && r.opts.Has("CLONE_DEFAULT") {
		return command.Fragment(r.opts.Get("CLONE_DEFAULT"))
	}
	return command.Words("-p", "-o", "readonly=off")
}

// parentRestored reports whether every ancestor suffix of suffix was cloned.
func parentRestored(suffix string, restored map[string]bool) (string, bool) {
	parent := suffix
	for {
		i := strings.LastIndexByte(parent, '/')
		if i < 0 {
			return "", true
		}
		parent = parent[:i]
		if !restored[parent] {
			return parent, false
		}
	}
}

// createRecursiveClone clones every dataset of originDS (at the requested or
// latest snapshot) below newDS. In best-effort revert mode, failures and
// unrestorable descendants are skipped and counted; it returns how many
// datasets were not restored.
func (r *run) createRecursiveClone(s side, originDS, newDS string) (int, error) {
	ep := r.endpoint(s)
	bestEffort := r.verb == "revert" && r.opts.Bool("REVERT_FORCE")
	restored := map[string]bool{}
	var count, success, failed, skipped int
	for _, suffix := range r.list {
		last := r.dataset(s, suffix).latest
		snap := ep.Snap
		if snap == "" {
			snap = last
		}
		originSnap := originDS + suffix + snap
		cloneDS := newDS + suffix
		if bestEffort {
			parent, ok := parentRestored(suffix, restored)
			if !ok || last == "" {
				skipped++
				if !ok {
					r.rt.Log.Errorf("skipping clone '%s': parent '%s' was not restored", cloneDS, newDS+parent)
				} else {
					r.rt.Log.Errorf("skipping clone '%s': no source snapshot", cloneDS)
				}
				continue
			}
		}
		if last == "" {
			continue
		}
		count++
		args := append(append(command.Words("clone"), r.cloneFlags()...), command.Name(originSnap), command.Name(cloneDS))
		cmd := zfs.Command(r.rt, ep, zfs.RemoteDefault, args...)
		verb := "cloning: "
		if r.dryRun() {
			verb = "would clone: "
		}
		r.rt.Log.Infof("%s%s", verb, quoteName(ep.IsRemote(), originSnap))
		if r.dryRun() {
			r.rt.Log.Noticef("+ %s", cmd.String())
			restored[suffix] = true
			continue
		}
		res := r.rt.Run(r.ctx, cmd)
		for _, l := range res.Lines {
			line := app.SanitizeLine(l.Text)
			switch {
			case line == "":
			case strings.Contains(line, "encryption key not loaded"):
				f := strings.Fields(line)
				r.rt.Log.Infof("to mount %s load encryption key in %s", cloneDS, f[len(f)-1])
			default:
				r.rt.Log.Warnf("unexpected 'zfs clone' output: %s", line)
			}
		}
		if !res.OK() {
			if !bestEffort {
				return 0, fail("error creating clone '%s'", cloneDS)
			}
			r.rt.Log.Errorf("error creating clone '%s'", cloneDS)
			failed++
			continue
		}
		success++
		restored[suffix] = true
	}
	switch {
	case count > 0 && r.dryRun():
		r.rt.Log.Noticef("would clone %d/%d datasets to %s", count, len(r.list), newDS)
	case count > 0:
		r.rt.Log.Noticef("cloned %d/%d datasets to %s", success, len(r.list), newDS)
	default:
		r.rt.Log.Noticef("no source snapshots to clone")
	}
	if bestEffort && (failed > 0 || skipped > 0) {
		if r.dryRun() {
			r.rt.Log.Errorf("revert would be incomplete: %d planned, %d skipped; original tree would be preserved at '%s'", count, skipped, originDS)
		} else {
			r.rt.Log.Errorf("revert incomplete: %d cloned, %d failed, %d skipped; original tree preserved at '%s'", success, failed, skipped, originDS)
		}
		return failed + skipped, nil
	}
	return 0, nil
}

// runCloneShuffle implements four-endpoint clone: clone SOURCE to TARGET,
// then back up the new clone to TARGET_BACKUP using ORIGIN_BACKUP as its
// replicated origin.
func (r *run) runCloneShuffle() error {
	if _, err := r.createRecursiveClone(src, r.src.DS, r.tgt.DS); err != nil {
		return err
	}
	r.verb = "backup"
	r.src, r.tgt = r.tgt, r.cloneBackup
	r.origin = r.cloneOrigin
	r.resetState()
	if err := r.validateSourceDataset(); err != nil {
		return err
	}
	if err := r.validateTargetDataset(); err != nil {
		return err
	}
	// A new clone has no snapshots of its own; snapshot it before planning.
	if strings.Contains(r.dataset(src, "").prop("origin"), "@") {
		reason := "snapshotting: "
		if r.dryRun() {
			reason = "would snapshot: "
		}
		if _, err := r.createSourceSnapshot(reason); err != nil {
			return err
		}
	}
	if err := r.validateSnapshots(); err != nil {
		return err
	}
	if r.cloneBackupNeedsSnapshot() {
		reason := "snapshotting: "
		if r.dryRun() {
			reason = "would snapshot: "
		}
		if _, err := r.createSourceSnapshot(reason); err != nil {
			return err
		}
	}
	r.computeEligibility()
	if err := r.configureOriginBackup(); err != nil {
		return err
	}
	r.runBackup()
	return nil
}

func (r *run) cloneBackupNeedsSnapshot() bool {
	_, snap, ok := strings.Cut(r.dataset(src, "").prop("origin"), "@")
	return ok && r.dataset(src, "").latest == "@"+snap
}

// configureOriginBackup backs up existing clones incrementally from their
// origin snapshot, which already exists below the target origin.
func (r *run) configureOriginBackup() error {
	if r.origin.ID == "" {
		return nil
	}
	n := 0
	for _, suffix := range r.list {
		origin := r.dataset(src, suffix).prop("origin")
		parts := strings.Split(origin, "@")
		if len(parts) != 2 {
			continue
		}
		p := r.pair(suffix)
		if p.match != "" {
			continue
		}
		snap := "@" + parts[1]
		p.sourceOriginMatch = origin
		p.targetOrigin = r.origin.DS
		p.match = snap
		p.sourceStart = snap
		p.sourceEnd = r.dataset(src, suffix).latest
		a := r.action(suffix)
		a.canSync = true
		a.statusNotice, a.statusInfo = "", ""
		n++
	}
	if n == 0 {
		return fail("source has no clone origin: %s", r.src.ID)
	}
	return nil
}
