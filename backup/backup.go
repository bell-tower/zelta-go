package backup

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/bell-tower/zelta-go/cmdbuild"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/match"
	"github.com/bell-tower/zelta-go/zfs"
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
	// OnLine, when non-nil, is called for each line of zfs send/recv stderr
	// output during execution. Useful for progress streaming.
	OnLine func(line string)
}

// Result is the structured outcome of a backup run: match, plan, executed
// commands, telemetry, and classified status. Consumers render their own
// output from these fields; the library never logs or formats human prose.
type Result struct {
	Match    *match.Result
	Plan     *Plan
	Warnings []string // filter warnings from match
	Errors   []string // non-fatal replication errors
	// Commands lists each send/receive pipe actually executed, in order.
	Commands []zfs.Command
	// Stats carries send/recv replication telemetry (zero when nothing ran).
	Stats zfs.PipeStats
	// ErrCode classifies the backup outcome for programmatic handling.
	ErrCode ErrCode
	// StartTime / EndTime bracket the run for CLI presentation (JSON, logs).
	StartTime time.Time
	EndTime   time.Time
}

// Run matches source/target, plans snap+send/recv, executes every step
// (snapshot, pipes, bookmarks), and reports the outcome. For plan-only access
// see Prepare/Commands; for step-by-step execution see Plan.RunStep.
func Run(ctx context.Context, exec zfs.Executor, req Request) (*Result, error) {
	srcEp := req.Source
	startTime := time.Now()
	plan, mres, err := prepare(ctx, exec, req)
	if err != nil {
		return nil, err
	}
	var execEndTime time.Time

	// Execute the source snapshot that prepare predicted into the plan.
	if plan.SnapReason != "" {
		if err := exec.Snapshot(ctx, srcEp.String(), srcEp.Dataset+plan.SnapSavepoint, true); err != nil {
			return nil, fmt.Errorf("snapshot: %w", err)
		}
	}
	direction := req.SyncDirection.PipeArg()

	var errors []string
	var commands []zfs.Command
	// Pipe telemetry lives in the executor (zfs.Real parses send/recv output
	// internally); reset stale counters and forward raw lines to req.OnLine.
	var stats zfs.PipeStats
	if reporter, ok := exec.(zfs.PipeStatsReporter); ok {
		reporter.TakeStats()
	}
	if req.OnLine != nil {
		if tee, ok := exec.(stderrTee); ok {
			tee.SetStderrLog(&lineWriter{fn: req.OnLine})
		}
	}

	for i := range plan.Steps {
		if err := plan.syncPair(ctx, exec, req, direction, i, &commands); err != nil {
			return nil, err
		}
	}
	errors = append(errors, createBookmarks(ctx, exec, req, plan)...)
	execEndTime = time.Now()
	if reporter, ok := exec.(zfs.PipeStatsReporter); ok {
		stats = reporter.TakeStats()
	}

	res := &Result{
		Match:     mres,
		Plan:      plan,
		Warnings:  append([]string(nil), plan.Warnings...),
		Errors:    errors,
		Commands:  commands,
		Stats:     stats,
		StartTime: startTime,
		EndTime:   execEndTime,
		ErrCode:   ErrCodeFromPlan(plan),
	}
	return res, nil
}

// Prepare runs the read-only recon (zfs get/list + match) and builds the full
// execution plan, including the predicted source snapshot and target-origin
// checks. It may create a missing target parent (oracle parity: even a
// dry-run may CREATE parent) but never snapshots or sends. Run = Prepare +
// execute; Commands = Prepare + plan command lines.
//
// Known wrinkle: parent creation is a write on a "commands-only" call, so
// Commands()/Prepare() are not fully side-effect free. Oracle parity demands
// it today (validate_target_parent_dataset); the right long-term behavior is
// open — see djbell/zelta-go issue "Prepare/Commands side effects" on the
// forge (parent create vs pure planning).
func Prepare(ctx context.Context, exec zfs.Executor, req Request) (*Plan, error) {
	plan, _, err := prepare(ctx, exec, req)
	return plan, err
}

// Commands runs the read-only recon needed to produce backup's command list —
// match runs lazily inside, exactly as in Run — and returns structured
// commands without executing anything. The first pass of intermediate fulls
// is shown; the second pass is execute-time, matching the oracle dry-run.
// For the plan behind the commands, see Prepare.
func Commands(ctx context.Context, exec zfs.Executor, req Request) ([]zfs.Command, error) {
	plan, _, err := prepare(ctx, exec, req)
	if err != nil {
		return nil, err
	}
	return plan.Commands(req.Source, req.Target, req.SyncDirection.PipeArg()), nil
}

// prepare runs the recon and planning shared by Run, Prepare and Commands.
// Match warnings are folded into plan.Warnings so consumers read one list.
func prepare(ctx context.Context, exec zfs.Executor, req Request) (*Plan, *match.Result, error) {
	srcEp := req.Source
	tgtEp := req.Target
	if srcEp.Dataset == "" {
		return nil, nil, fmt.Errorf("source: empty endpoint")
	}
	if tgtEp.Dataset == "" {
		return nil, nil, fmt.Errorf("target: empty endpoint")
	}
	srcStr := srcEp.String()
	tgtStr := tgtEp.String()

	// Phase 1: cheap dataset context (zfs get filesystem/volume) — features + flags.
	srcCtx, err := zfs.LoadDatasetContext(ctx, exec, srcStr, srcEp.Dataset, req.Depth)
	if err != nil {
		return nil, nil, fmt.Errorf("source properties: %w", err)
	}
	if !srcCtx.Exists {
		return nil, nil, fmt.Errorf("source dataset '%s' does not exist", srcStr)
	}
	tgtCtx, err := zfs.LoadDatasetContext(ctx, exec, tgtStr, tgtEp.Dataset, req.Depth)
	if err != nil {
		return nil, nil, fmt.Errorf("target properties: %w", err)
	}
	if !tgtCtx.Exists {
		// Missing target: inherit encryption/features from nearest existing ancestor.
		if err := fillMissingTargetParentContext(ctx, exec, tgtStr, tgtEp.Dataset, tgtCtx); err != nil {
			return nil, nil, err
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
		return nil, nil, fmt.Errorf("target origin cannot be combined with filtered intermediate sends")
	}
	mres, err := match.Compare(ctx, exec, match.Request{
		Source:                  srcEp,
		Target:                  tgtEp,
		Depth:                   req.Depth,
		Include:                 req.Include,
		Exclude:                 req.Exclude,
		Props:                   props,
		PreserveSourceSnapshots: filteredIntermediate,
		SrcContext:              srcCtx,
		TgtContext:              tgtCtx,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("match: %w", err)
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
			return nil, nil, err
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
	// Oracle validate_target_parent_dataset: even a dry-run may CREATE parent.
	if err := ensureTargetParent(ctx, exec, tgtStr, tgtEp.Dataset, len(mres.TgtRows) > 0, createParent); err != nil {
		return nil, nil, err
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
			return nil, nil, err
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
		return nil, nil, err
	}

	// Snap-if-needed (or ALWAYS): predict the snapshot into the plan; the
	// real zfs snapshot executes in Run.
	if snapReason != "" {
		plan.SnapReason = snapReason
		plan.SnapSavepoint = snapSavepoint
		plan.SnapArgv = snapArgv
		if !filteredIntermediate {
			if err := plan.ApplySourceSnap(snapSavepoint, req.Intermediate); err != nil {
				return nil, nil, err
			}
		}
	}

	if flags.Bookmarks {
		plan.Bookmarks, err = buildBookmarkPlans(plan, srcStr, tgtStr, flags.BookmarkPrefix, tgtEp.Host)
		if err != nil {
			return nil, nil, err
		}
	}
	// Oracle: dual-remote + proxy → one warning (localhost proxy).
	// Same remote on both ends is hairpin/local — never warned.
	if req.SyncDirection.Normalize() == DirectionProxy && bothRemote(srcEp, tgtEp) && !sameRemote(srcEp, tgtEp) {
		mres.Warnings = append(mres.Warnings, "syncing remote endpoints through localhost; consider --push or --pull")
	}
	// One warning list for consumers: match-level first, then plan steps.
	plan.Warnings = append(plan.Warnings, mres.Warnings...)
	return plan, mres, nil
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
	originDS, _, ok := endpoint.SplitOrigin(sourceOrigin)
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
		dataset, snap, valid := endpoint.SplitOrigin(views[i].SrcOrigin)
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

func runStep(ctx context.Context, exec zfs.Executor, req Request, st *Step, direction string, commands *[]zfs.Command) error {
	if len(st.Send) == 0 || len(st.Recv) == 0 {
		return fmt.Errorf("backup: empty send/recv for %q", st.DSSuffix)
	}
	if commands != nil {
		*commands = append(*commands, zfs.Command{
			Kind:      zfs.CmdSendRecv,
			Source:    req.Source,
			Target:    req.Target,
			Send:      append([]string(nil), st.Send...),
			Recv:      append([]string(nil), st.Recv...),
			Direction: direction,
		})
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
