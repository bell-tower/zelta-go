package backup

import (
	"context"
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/match"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

// Request is a backup run.
type Request struct {
	Source       string
	Target       string
	TargetOrigin string // already-backed-up origin endpoint for clone replication
	DryRun       bool
	Intermediate bool   // true → -I (default); false → -i
	SnapMode     string // IF_NEEDED (default), ALWAYS, NEVER
	SnapName     string // bare name without @; empty → DefaultSnapName()
	SnapTime     string // IF_NEEDED threshold; recent snapshots may skip
	SnapSize     string // IF_NEEDED threshold in bytes; small changes may skip
	Depth        int
	Include      []string
	Exclude      []string
	// CreateParent mirrors ZELTA_CREATE_PARENT (default true). Nil → true.
	CreateParent *bool
	// Flags overrides send/recv fragments. Nil → opt.Resolve() (defaults + env).
	Flags *opt.SendRecv
	// SyncDirection for dual-remote: "PULL" (default), "PUSH", or ""/"0" (proxy + warn).
	// Empty → ZELTA_SYNC_DIRECTION env or PULL.
	SyncDirection string
}

// Result is match + plan (+ dry-run / execute text).
type Result struct {
	Match    *match.Result
	Plan     *Plan
	Output   string
	Warnings []string // filter warnings from match
	Errors   []string // non-fatal replication errors
}

// Run matches source/target, plans snap+send/recv, dry-runs or executes.
func Run(ctx context.Context, exec zfs.Executor, req Request) (*Result, error) {
	srcEp, err := endpoint.Parse(req.Source)
	if err != nil {
		return nil, fmt.Errorf("source: %w", err)
	}
	tgtEp, err := endpoint.Parse(req.Target)
	if err != nil {
		return nil, fmt.Errorf("target: %w", err)
	}

	// Written + type (parsable default cols would skip written via add_written).
	props := append([]string(nil), match.BackupListProps...)
	if strings.TrimSpace(req.TargetOrigin) != "" {
		props = append(props, "origin")
	}
	if strings.TrimSpace(req.SnapTime) != "" || strings.TrimSpace(req.SnapSize) != "" {
		props = append(props, "snapshots_changed")
	}
	filteredIntermediate := req.Intermediate && (len(req.Include) > 0 || len(req.Exclude) > 0)
	if filteredIntermediate && strings.TrimSpace(req.TargetOrigin) != "" {
		return nil, fmt.Errorf("target origin cannot be combined with filtered intermediate sends")
	}
	mres, err := match.Compare(ctx, exec, match.Request{
		Source:                  req.Source,
		Target:                  req.Target,
		Depth:                   req.Depth,
		Include:                 req.Include,
		Exclude:                 req.Exclude,
		Props:                   props,
		Scripting:               true,
		Parsable:                true,
		PreserveSourceSnapshots: filteredIntermediate,
	})
	if err != nil {
		return nil, fmt.Errorf("match: %w", err)
	}

	views := ViewsFromMatch(mres.Pairs)
	if strings.TrimSpace(req.TargetOrigin) != "" {
		if err := configureTargetOrigin(ctx, exec, req.TargetOrigin, mres, views, props, req.Depth); err != nil {
			return nil, err
		}
	}
	var filteredFilter *match.Filter
	if filteredIntermediate {
		filter := match.ParseFilter(req.Include, req.Exclude)
		filteredFilter = filter
		for i := range views {
			views[i].FilteredActive = true
			views[i].FilteredEnds = filteredEnds(views[i], filter)
		}
	}
	for i := range views {
		if views[i].TgtName == "" && views[i].SrcName != "" {
			views[i].TgtName = joinTgt(tgtEp.Dataset, views[i].DSSuffix)
		}
	}

	createParent := true
	if req.CreateParent != nil {
		createParent = *req.CreateParent
	}
	// Oracle validate_target_parent_dataset: even dry-run may CREATE parent.
	if err := ensureTargetParent(ctx, exec, req.Target, tgtEp.Dataset, len(mres.TgtRows) > 0, createParent); err != nil {
		return nil, err
	}

	flags := req.sendRecv()
	if flags.BookmarkMode != "" && flags.BookmarkMode != "0" && flags.BookmarkMode != "1" {
		return nil, fmt.Errorf("invalid bookmark mode: %s", flags.BookmarkMode)
	}
	snapReason := ShouldSnapshotWithThresholds(req.SnapMode, views, req.SnapTime, req.SnapSize)
	var snapSavepoint string
	var snapArgv []string
	if snapReason != "" {
		name := req.SnapName
		if name == "" {
			name = DefaultSnapName()
		}
		snapSavepoint = "@" + strings.TrimPrefix(name, "@")
		snapArgv, err = BuildSnapArgv(srcEp.Dataset, snapSavepoint)
		if err != nil {
			return nil, err
		}
		if filteredIntermediate {
			for i := range views {
				if filteredFilter.KeepSourceSnap(snapSavepoint, views[i].SrcName, views[i].DSSuffix) {
					views[i].FilteredEnds = append(views[i].FilteredEnds, snapSavepoint)
				}
			}
		}
	}
	plan, err := PlanFromMatch(views, req.Intermediate, flags)
	if err != nil {
		return nil, err
	}

	// Snap-if-needed (or ALWAYS).
	if snapReason != "" {
		plan.SnapReason = snapReason
		plan.SnapSavepoint = snapSavepoint
		plan.SnapArgv = snapArgv

		if req.DryRun {
			if !filteredIntermediate {
				if err := plan.ApplySourceSnap(snapSavepoint, req.Intermediate); err != nil {
					return nil, err
				}
			}
		} else {
			if err := exec.Snapshot(ctx, req.Source, srcEp.Dataset+snapSavepoint, true); err != nil {
				return nil, fmt.Errorf("snapshot: %w", err)
			}
			if !filteredIntermediate {
				if err := plan.ApplySourceSnap(snapSavepoint, req.Intermediate); err != nil {
					return nil, err
				}
			}
		}
	}

	direction := req.syncDirection()
	if flags.BookmarkMode == "1" {
		plan.Bookmarks, err = buildBookmarkPlans(plan, req.Source, req.Target, flags.BookmarkPrefix, tgtEp.Host)
		if err != nil {
			return nil, err
		}
	}
	// Oracle: dual-remote + no direction → one warning (localhost proxy).
	// Same remote on both ends is hairpin/local — never warned.
	if direction == "" && bothRemote(srcEp, tgtEp) && !sameRemote(srcEp, tgtEp) {
		mres.Warnings = append(mres.Warnings, "syncing remote endpoints through localhost; consider --push or --pull")
	}

	var b strings.Builder
	var errors []string
	if req.DryRun {
		out, err := FormatDryRunDirection(plan, req.Source, req.Target, direction)
		if err != nil {
			return nil, err
		}
		b.WriteString(out)
	} else {
		if err := executePlan(ctx, exec, req, plan, direction); err != nil {
			return nil, err
		}
		errors = append(errors, createBookmarks(ctx, exec, req, plan)...)
	}
	if sum := plan.Summary(); sum != "" {
		// Dry-run with work: oracle often skips summary; keep for empty plans / execute.
		if !req.DryRun || plan.Full+plan.Incr == 0 {
			b.WriteString(sum)
			b.WriteByte('\n')
		}
	}

	return &Result{
		Match:    mres,
		Plan:     plan,
		Output:   b.String(),
		Warnings: append([]string(nil), mres.Warnings...),
		Errors:   errors,
	}, nil
}

func configureTargetOrigin(ctx context.Context, exec zfs.Executor, targetOrigin string, mres *match.Result, views []PairView, props []string, depth int) error {
	originTarget, err := endpoint.Parse(targetOrigin)
	if err != nil {
		return fmt.Errorf("target origin: %w", err)
	}
	if originTarget.Snapshot != "" {
		return fmt.Errorf("target origin must not include a snapshot")
	}
	var sourceOrigin string
	for _, pair := range mres.Pairs {
		if pair.DSSuffix == "" {
			sourceOrigin = pair.SrcOrigin
			break
		}
	}
	originDS, _, ok := splitOrigin(sourceOrigin)
	if !ok {
		return fmt.Errorf("target origin: source has no usable clone origin")
	}
	originLines, err := exec.List(ctx, targetOrigin, originTarget.Dataset, props, depth)
	if err != nil {
		return fmt.Errorf("target origin list: %w", err)
	}
	originRows, err := zfs.ParseListLines(originLines, props)
	if err != nil {
		return fmt.Errorf("target origin parse: %w", err)
	}
	originNames := make(map[string]bool, len(originRows))
	for _, row := range originRows {
		originNames[row.Name] = true
	}
	for i := range views {
		if views[i].SrcOrigin == "" {
			return fmt.Errorf("target origin: dataset %q has no clone origin", views[i].DSSuffix)
		}
		dataset, snap, valid := splitOrigin(views[i].SrcOrigin)
		if !valid {
			return fmt.Errorf("target origin: invalid source clone origin %q", views[i].SrcOrigin)
		}
		suffix, err := endpoint.DSSuffix(originDS, dataset)
		if err != nil {
			return fmt.Errorf("target origin: %w", err)
		}
		targetDataset := joinTgt(originTarget.Dataset, suffix)
		if !originNames[targetDataset+snap] {
			return fmt.Errorf("target origin: missing %s", targetDataset+snap)
		}
		views[i].TargetOrigin = targetDataset + snap
	}
	return nil
}

func buildBookmarkPlans(plan *Plan, source, target, prefix, targetHost string) ([]BookmarkPlan, error) {
	if prefix == "" {
		prefix = targetHost + "_"
	}
	var out []BookmarkPlan
	last := make(map[string]int)
	for i, st := range plan.Steps {
		if st.Kind == KindFull || st.Kind == KindIncremental {
			last[st.DSSuffix] = i
		}
	}
	for i, st := range plan.Steps {
		if st.Kind != KindFull && st.Kind != KindIncremental {
			continue
		}
		if last[st.DSSuffix] != i {
			continue
		}
		end := st.SourceEnd
		if st.FinalEnd != "" {
			end = st.FinalEnd
		}
		if end == "" {
			continue
		}
		targetSnap := st.TgtName + end
		name := st.SrcName + "#" + prefix + strings.TrimPrefix(end, "@")
		verify, err := cmdbuild.CheckArgv(targetSnap)
		if err != nil {
			return nil, err
		}
		create, err := cmdbuild.BookmarkArgv(st.SrcName+end, name)
		if err != nil {
			return nil, err
		}
		out = append(out, BookmarkPlan{
			VerifyEndpoint: target,
			SourceEndpoint: source,
			Verify:         verify,
			Create:         create,
		})
	}
	return out, nil
}

func createBookmarks(ctx context.Context, exec zfs.Executor, req Request, plan *Plan) []string {
	var errors []string
	for _, bm := range plan.Bookmarks {
		if _, err := exec.List(ctx, bm.VerifyEndpoint, bm.Verify[len(bm.Verify)-1], []string{"name"}, 0); err != nil {
			errors = append(errors, fmt.Sprintf("bookmark verify %s: %v", bm.Verify[len(bm.Verify)-1], err))
			continue
		}
		if err := exec.Bookmark(ctx, bm.SourceEndpoint, bm.Create[len(bm.Create)-2], bm.Create[len(bm.Create)-1]); err != nil {
			errors = append(errors, fmt.Sprintf("bookmark %s: %v", bm.Create[len(bm.Create)-1], err))
		}
	}
	return errors
}

func executePlan(ctx context.Context, exec zfs.Executor, req Request, plan *Plan, direction string) error {
	for _, st := range plan.Steps {
		if st.Kind != KindFull && st.Kind != KindIncremental {
			continue
		}
		if err := runStep(ctx, exec, req, st, direction); err != nil {
			return err
		}
		// Intermediate full: second pass match→latest (oracle run_zfs_sync twice).
		if st.Kind == KindFull && st.FinalEnd != "" && st.FinalEnd != st.SourceEnd {
			second := &Step{
				DSSuffix:    st.DSSuffix,
				Kind:        KindIncremental,
				SourceStart: st.SourceEnd,
				SourceEnd:   st.FinalEnd,
				SrcName:     st.SrcName,
				TgtName:     st.TgtName,
				SrcType:     st.SrcType,
			}
			if err := buildCmds(second, req.Intermediate, plan.Flags); err != nil {
				return err
			}
			if err := runStep(ctx, exec, req, second, direction); err != nil {
				return err
			}
		}
	}
	return nil
}

func runStep(ctx context.Context, exec zfs.Executor, req Request, st *Step, direction string) error {
	if len(st.Send) == 0 || len(st.Recv) == 0 {
		return fmt.Errorf("backup: empty send/recv for %q", st.DSSuffix)
	}
	if err := exec.RunPipeDirection(ctx, req.Source, st.Send, req.Target, st.Recv, direction); err != nil {
		return fmt.Errorf("sync %s: %w", st.DSSuffix, err)
	}
	return nil
}

func joinTgt(root, suffix string) string {
	if suffix == "" {
		return root
	}
	if strings.HasPrefix(suffix, "/") {
		return root + suffix
	}
	return root + "/" + suffix
}

// filteredEnds returns eligible source snapshots oldest-first, after the
// dataset's match. Each end becomes its own -i stream; excluded snapshots are
// never hidden inside a single -I range.
func filteredEnds(v PairView, filter *match.Filter) []string {
	var out []string
	matched := v.Match == ""
	for i := len(v.SrcSavepoints) - 1; i >= 0; i-- {
		sp := v.SrcSavepoints[i]
		if !matched {
			if sp == v.Match {
				matched = true
			}
			continue
		}
		if filter.KeepSourceSnap(sp, v.SrcName, v.DSSuffix) {
			out = append(out, sp)
		}
	}
	return out
}

// bothRemote is oracle _orchestrate: SRC_REMOTE && TGT_REMOTE.
// localhost is treated local (same as zfs.sshTarget).
func bothRemote(srcEp, tgtEp endpoint.Endpoint) bool {
	return remoteOK(srcEp) && remoteOK(tgtEp)
}

func remoteOK(ep endpoint.Endpoint) bool {
	return ep.Remote && ep.Host != "" && ep.Host != "localhost"
}

// sameRemote is oracle SRC_REMOTE == TGT_REMOTE (hairpin check).
func sameRemote(a, b endpoint.Endpoint) bool {
	return a.User == b.User && a.Host == b.Host
}

func (r Request) sendRecv() opt.SendRecv {
	if r.Flags != nil {
		return *r.Flags
	}
	return opt.Resolve()
}

// syncDirection mirrors ZELTA_SYNC_DIRECTION (shell default PULL).
// Oracle false values ("0", "no", "false", "off") mean proxy (controller-side).
func (r Request) syncDirection() string {
	d := strings.TrimSpace(r.SyncDirection)
	if d == "" {
		if v, ok := opt.Lookup("SYNC_DIRECTION"); ok {
			d = strings.TrimSpace(v)
		}
	}
	if d == "" {
		d = "PULL"
	}
	switch strings.ToLower(d) {
	case "0", "no", "false", "off":
		return ""
	default:
		return strings.ToUpper(d)
	}
}
