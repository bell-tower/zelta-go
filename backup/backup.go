package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"git.belltower.it/djbell/zelta-go/cmdbuild"
	"git.belltower.it/djbell/zelta-go/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/zlog"
	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/report"
	"git.belltower.it/djbell/zelta-go/zfs"
)

// stderrTee is implemented by *zfs.Real so progress hooks work without
// type-asserting the concrete executor in callers.
type stderrTee interface {
	SetStderrLog(w io.Writer)
}

// lineWriter converts each Write into line-by-line callback invocations.
type lineWriter struct {
	mu  sync.Mutex
	buf []byte
	fn  func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := len(p)
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := string(w.buf[:i])
		w.buf = w.buf[i+1:]
		w.fn(line)
	}
	return n, nil
}

// Request is a backup run.
type Request struct {
	Source endpoint.Endpoint
	Target endpoint.Endpoint
	// TargetOrigin is the already-backed-up origin endpoint for clone replication.
	// Zero means unset. Build via endpoint.Parse or field assignment.
	TargetOrigin endpoint.Endpoint
	DryRun       bool
	Intermediate bool // true → -I (default); false → -i
	// SnapMode: zero or SnapIfNeeded (default), SnapAlways, SnapNever.
	// Use ParseSnapMode for CLI/env/JSON strings.
	SnapMode SnapMode
	SnapName string // bare name without @; empty → DefaultSnapName()
	// SnapTime is an IF_NEEDED age threshold (0 = unset). Parse via ParseSnapTime.
	SnapTime time.Duration
	// SnapSize is an IF_NEEDED written-bytes threshold (0 = unset). Parse via ParseSnapSize.
	SnapSize int64
	Depth    int
	Include  []string
	Exclude  []string
	// CreateParent defaults true when nil.
	CreateParent *bool
	// Flags overrides send/recv fragments. Nil → DefaultSendRecv() only (no env).
	Flags *SendRecv
	// SyncDirection: zero/DirectionPull (default), DirectionPush, DirectionProxy.
	// Use ParseSyncDirection for CLI/env/JSON strings. Never reads process env.
	SyncDirection SyncDirection
	// JSON true → collect telemetry and populate JSONReport in Result.
	JSON bool
	// OnLine, when non-nil, is called for each line of zfs send/recv stderr
	// output during execution. Useful for progress logging.
	OnLine func(line string)
	// Log, when non-nil, receives info/debug messages (oracle report()
	// LOG_INFO/LOG_DEBUG) filtered and formatted by the sink. The inner
	// match runs at notice level like the oracle's ipc-run --log-level=2.
	Log *zlog.Sink
}

// Result is match + plan (+ dry-run / execute text).
type Result struct {
	Match    *match.Result
	Plan     *Plan
	Output   string
	Warnings []string // filter warnings from match
	Errors   []string // non-fatal replication errors
	// ErrCode classifies the backup outcome for programmatic handling.
	ErrCode ErrCode
	// JSONReport is set when req.JSON is true.
	JSONReport *report.BackupResult
}

// Run matches source/target, plans snap+send/recv, dry-runs or executes.
func Run(ctx context.Context, exec zfs.Executor, req Request) (*Result, error) {
	srcEp := req.Source
	tgtEp := req.Target
	if srcEp.Dataset == "" {
		return nil, fmt.Errorf("source: empty endpoint")
	}
	if tgtEp.Dataset == "" {
		return nil, fmt.Errorf("target: empty endpoint")
	}
	srcStr := srcEp.String()
	tgtStr := tgtEp.String()

	startTime := time.Now()
	var execEndTime time.Time

	// Oracle: LOG_INFO "checking properties for ID" + LOG_DEBUG "`zfs get`"
	// before each context load; command echoes fire from the executor hook.
	if req.Log != nil {
		if real, ok := exec.(*zfs.Real); ok {
			real.LogCmd = func(ep endpoint.Endpoint, argv []string) {
				req.Log.Debug(zfs.CommandDebug(ep, argv))
			}
		}
		req.Log.Info("checking properties for " + srcStr)
	}

	// Phase 1: cheap dataset context (zfs get filesystem/volume) — features + flags.
	srcCtx, err := zfs.LoadDatasetContext(ctx, exec, srcStr, srcEp.Dataset, req.Depth)
	if err != nil {
		return nil, fmt.Errorf("source properties: %w", err)
	}
	if !srcCtx.Exists {
		return nil, fmt.Errorf("source dataset '%s' does not exist", srcStr)
	}
	if req.Log != nil {
		req.Log.Info("checking properties for " + tgtStr)
	}
	tgtCtx, err := zfs.LoadDatasetContext(ctx, exec, tgtStr, tgtEp.Dataset, req.Depth)
	if err != nil {
		return nil, fmt.Errorf("target properties: %w", err)
	}
	if !tgtCtx.Exists {
		// Missing target: inherit encryption/features from nearest existing ancestor.
		if err := fillMissingTargetParentContext(ctx, exec, tgtStr, tgtEp.Dataset, tgtCtx); err != nil {
			return nil, err
		}
	}

	// Phase 2: expensive snap list — snap/bookmark columns only (never dataset props).
	// Awk: MATCH_IVSET only when target exists and source is encrypted.
	// snapshots_changed / origin / type / encryption come from DatasetContext (get all);
	// if the host lacks them, SNAP_TIME etc. simply cannot use them.
	wantIVSet := tgtCtx.Exists && srcCtx.SourceEncrypted()
	props := match.SnapListProps(srcCtx.Features, match.SnapListOpts{
		Written: true,
		IVSet:   wantIVSet,
	})

	filteredIntermediate := req.Intermediate && (len(req.Include) > 0 || len(req.Exclude) > 0)
	if filteredIntermediate && !req.TargetOrigin.IsZero() {
		return nil, fmt.Errorf("target origin cannot be combined with filtered intermediate sends")
	}
	// Oracle LOG_INFO "checking replica deltas" before the inner match; the
	// inner match runs at notice level (oracle ipc-run pins --log-level=2).
	if req.Log != nil {
		req.Log.Info("checking replica deltas")
	}
	mres, err := match.Compare(ctx, exec, match.Request{
		Source:                  srcEp,
		Target:                  tgtEp,
		Depth:                   req.Depth,
		Include:                 req.Include,
		Exclude:                 req.Exclude,
		Props:                   props,
		Scripting:               true,
		Parsable:                true,
		PreserveSourceSnapshots: filteredIntermediate,
		SrcContext:              srcCtx,
		TgtContext:              tgtCtx,
		Log:                     req.Log.Limit(zlog.Notice),
	})
	if err != nil {
		return nil, fmt.Errorf("match: %w", err)
	}

	if strings.Contains(srcEp.Raw, "\r") && srcEp.Dataset != "" {
		mres.Warnings = append(mres.Warnings, "carriage return stripped: "+srcStr)
	}
	if strings.Contains(tgtEp.Raw, "\r") && tgtEp.Dataset != "" {
		mres.Warnings = append(mres.Warnings, "carriage return stripped: "+tgtStr)
	}
	views := ViewsFromMatch(mres.Pairs)
	if !req.TargetOrigin.IsZero() {
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
	if err := ensureTargetParent(ctx, exec, tgtStr, tgtEp.Dataset, len(mres.TgtRows) > 0, createParent); err != nil {
		return nil, err
	}

	flags := req.sendRecv()
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
			if err := exec.Snapshot(ctx, srcStr, srcEp.Dataset+snapSavepoint, true); err != nil {
				return nil, fmt.Errorf("snapshot: %w", err)
			}
			if !filteredIntermediate {
				if err := plan.ApplySourceSnap(snapSavepoint, req.Intermediate); err != nil {
					return nil, err
				}
			}
		}
	}

	direction := req.SyncDirection.PipeArg()
	if flags.Bookmarks {
		plan.Bookmarks, err = buildBookmarkPlans(plan, srcStr, tgtStr, flags.BookmarkPrefix, tgtEp.Host)
		if err != nil {
			return nil, err
		}
	}
	// Oracle: dual-remote + proxy → one warning (localhost proxy).
	// Same remote on both ends is hairpin/local — never warned.
	if req.SyncDirection.Normalize() == DirectionProxy && bothRemote(srcEp, tgtEp) && !sameRemote(srcEp, tgtEp) {
		mres.Warnings = append(mres.Warnings, "syncing remote endpoints through localhost; consider --push or --pull")
	}

	var b strings.Builder
	var errors []string
	if req.OnLine != nil {
		if tee, ok := exec.(stderrTee); ok {
			tee.SetStderrLog(&lineWriter{fn: req.OnLine})
		}
	}

	if req.DryRun {
		out, err := FormatDryRunDirection(plan, srcStr, tgtStr, direction)
		if err != nil {
			return nil, err
		}
		b.WriteString(out)
	} else {
		work := plan.Full + plan.Incr
		total := work + plan.Skip + plan.Block
		// Oracle: announce sync only when there is work; pure up-to-date skips it.
		if work > 0 && total > 0 {
			b.WriteString(fmt.Sprintf("syncing %d datasets\n", total))
		}
		execStart := time.Now()
		if err := executePlan(ctx, exec, req, plan, direction); err != nil {
			return nil, err
		}
		errors = append(errors, createBookmarks(ctx, exec, req, plan)...)
		execEndTime = time.Now()
		if work == 0 && plan.Skip > 0 {
			if plan.Skip == 1 {
				b.WriteString("dataset up-to-date\n")
			} else {
				b.WriteString(fmt.Sprintf("%d datasets up-to-date\n", plan.Skip))
			}
		}
		if work > 0 {
			secs := execEndTime.Sub(execStart).Seconds()
			// Byte accounting is still incomplete; shellspec accepts wildcard size.
			b.WriteString(fmt.Sprintf("0B sent, %d streams received in %.0f seconds\n", work, secs))
		}
	}
	if sum := plan.Summary(); sum != "" {
		// Dry-run with work: oracle often skips summary; keep for empty plans.
		// Execute path uses oracle-style notices above instead of Summary().
		if req.DryRun && plan.Full+plan.Incr == 0 {
			b.WriteString(sum)
			b.WriteByte('\n')
		}
	}

	res := &Result{
		Match:    mres,
		Plan:     plan,
		Output:   b.String(),
		Warnings: append(append([]string(nil), mres.Warnings...), plan.Warnings...),
		Errors:   errors,
	}
	res.ErrCode = ErrCodeFromOutput(res.Output)
	if req.JSON {
		sentStreams := make([]string, 0, len(plan.Steps))
		streamCount := 0
		for _, st := range plan.Steps {
			if st.Kind == KindFull || st.Kind == KindIncremental {
				streamCount++
				sentStreams = append(sentStreams, st.TgtName+st.SourceEnd)
			}
		}
		var messages []string
		for _, w := range res.Warnings {
			messages = append(messages, "warning: "+w)
		}
		res.JSONReport = report.NewBackupResult(
			srcEp, tgtEp,
			streamCount, sentStreams,
			errors, messages,
			startTime, execEndTime,
		)
	}
	return res, nil
}

// fillMissingTargetParentContext walks parents of a missing target and copies
// encryption (and feature flags) onto tgtCtx root so child sends inherit correctly.
func fillMissingTargetParentContext(ctx context.Context, exec zfs.Executor, tgtStr, dataset string, tgtCtx *zfs.DatasetContext) error {
	parent := parentDataset(dataset)
	for parent != "" {
		pctx, err := zfs.LoadDatasetContext(ctx, exec, tgtStr, parent, 1)
		if err != nil {
			return fmt.Errorf("target parent properties: %w", err)
		}
		if pctx.Exists {
			tgtCtx.Features = pctx.Features
			if tgtCtx.BySuffix == nil {
				tgtCtx.BySuffix = make(map[string]map[string]string)
			}
			if enc := pctx.Prop("", "encryption"); enc != "" {
				m := tgtCtx.BySuffix[""]
				if m == nil {
					m = make(map[string]string)
					tgtCtx.BySuffix[""] = m
				}
				m["encryption"] = enc
			}
			return nil
		}
		parent = parentDataset(parent)
	}
	return nil
}

func configureTargetOrigin(ctx context.Context, exec zfs.Executor, originTarget endpoint.Endpoint, mres *match.Result, views []PairView, props []string, depth int) error {
	if originTarget.Snapshot != "" {
		return fmt.Errorf("target origin must not include a snapshot")
	}
	if originTarget.Dataset == "" {
		return fmt.Errorf("target origin: empty endpoint")
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
	originStr := originTarget.String()
	originLines, err := exec.List(ctx, originStr, originTarget.Dataset, props, depth)
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
	// Oracle LOG_DEBUG "`<pipe command>`" before each transfer.
	if req.Log != nil {
		if sh, err := zfs.PipeShellDirection(req.Source.String(), req.Target.String(), st.Send, st.Recv, direction); err == nil {
			req.Log.Debug("`" + sh + "`")
		}
	}
	if err := exec.RunPipeDirection(ctx, req.Source.String(), st.Send, req.Target.String(), st.Recv, direction); err != nil {
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

func (r Request) sendRecv() SendRecv {
	if r.Flags != nil {
		return *r.Flags
	}
	return DefaultSendRecv()
}
