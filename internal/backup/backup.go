package backup

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/endpoint"
	"git.belltower.it/djbell/zelta-go/internal/match"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/internal/zfs"
)

// Request is a backup run.
type Request struct {
	Source       string
	Target       string
	DryRun       bool
	Intermediate bool   // true → -I (default); false → -i
	SnapMode     string // IF_NEEDED (default), ALWAYS, NEVER
	SnapName     string // bare name without @; empty → DefaultSnapName()
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
	mres, err := match.Compare(ctx, exec, match.Request{
		Source:    req.Source,
		Target:    req.Target,
		Depth:     req.Depth,
		Include:   req.Include,
		Exclude:   req.Exclude,
		Props:     match.BackupListProps,
		Scripting: true,
		Parsable:  true,
	})
	if err != nil {
		return nil, fmt.Errorf("match: %w", err)
	}

	views := ViewsFromMatch(mres.Pairs)
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
	plan, err := PlanFromMatch(views, req.Intermediate, flags)
	if err != nil {
		return nil, err
	}

	// Snap-if-needed (or ALWAYS).
	if reason := ShouldSnapshot(req.SnapMode, views); reason != "" {
		name := req.SnapName
		if name == "" {
			name = DefaultSnapName()
		}
		name = strings.TrimPrefix(name, "@")
		savepoint := "@" + name
		argv, err := BuildSnapArgv(srcEp.Dataset, savepoint)
		if err != nil {
			return nil, err
		}
		plan.SnapReason = reason
		plan.SnapSavepoint = savepoint
		plan.SnapArgv = argv

		if req.DryRun {
			if err := plan.ApplySourceSnap(savepoint, req.Intermediate); err != nil {
				return nil, err
			}
		} else {
			if err := exec.Snapshot(ctx, req.Source, srcEp.Dataset+savepoint, true); err != nil {
				return nil, fmt.Errorf("snapshot: %w", err)
			}
			if err := plan.ApplySourceSnap(savepoint, req.Intermediate); err != nil {
				return nil, err
			}
		}
	}

	direction := req.syncDirection()
	// Oracle: dual-remote + no direction → one warning (localhost proxy).
	// Same remote on both ends is hairpin/local — never warned.
	if direction == "" && bothRemote(srcEp, tgtEp) && !sameRemote(srcEp, tgtEp) {
		mres.Warnings = append(mres.Warnings, "syncing remote endpoints through localhost; consider --push or --pull")
	}

	var b strings.Builder
	if req.DryRun {
		out, err := FormatDryRunDirection(plan, req.Source, req.Target, direction)
		if err != nil {
			return nil, err
		}
		b.WriteString(out)
	} else {
		if err := sendCheck(ctx, exec, req, plan); err != nil {
			return nil, err
		}
		if err := executePlan(ctx, exec, req, plan, direction); err != nil {
			return nil, err
		}
		if err := createBookmarks(ctx, exec, req, plan, srcEp, tgtEp, flags); err != nil {
			return nil, err
		}
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
	}, nil
}

var sendCheckOption = regexp.MustCompile(`(?i)(invalid|illegal|unknown|unrecognized option|usage: zfs send)`)

func sendCheck(ctx context.Context, exec zfs.Executor, req Request, plan *Plan) error {
	if req.DryRun || !plan.Flags.SendCheck || plan.Flags.SendOverride != "" {
		return nil
	}
	var root *Step
	for _, st := range plan.Steps {
		if st.DSSuffix == "" && (st.Kind == KindFull || st.Kind == KindIncremental) {
			root = st
			break
		}
	}
	if root == nil {
		return nil
	}
	for _, candidate := range []string{"-e", "-c", "-L"} {
		for strings.Contains(" "+plan.Flags.SendDefault+" ", " "+candidate+" ") {
			probe := append([]string(nil), root.Send...)
			if len(probe) < 2 {
				return nil
			}
			probe = append(probe[:2:2], append([]string{"-n", "-v"}, probe[2:]...)...)
			out, _ := exec.SendCheck(ctx, req.Source, probe)
			if !sendCheckOption.MatchString(out) {
				return nil
			}
			plan.Flags.SendDefault = removeSendFlag(plan.Flags.SendDefault, candidate)
			for _, st := range plan.Steps {
				if st.Kind == KindFull || st.Kind == KindIncremental {
					if err := buildCmds(st, req.Intermediate, plan.Flags); err != nil {
						return err
					}
				}
			}
		}
	}
	return nil
}

func removeSendFlag(flags, drop string) string {
	parts := strings.Fields(flags)
	out := parts[:0]
	for _, p := range parts {
		if p != drop {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

func createBookmarks(ctx context.Context, exec zfs.Executor, req Request, plan *Plan, srcEp, tgtEp endpoint.Endpoint, flags opt.SendRecv) error {
	if flags.BookmarkMode != "1" {
		return nil
	}
	prefix := flags.BookmarkPrefix
	if prefix == "" {
		prefix = tgtEp.Host + "_"
	}
	for _, st := range plan.Steps {
		if st.Kind != KindFull && st.Kind != KindIncremental {
			continue
		}
		if st.SourceEnd == "" {
			continue
		}
		targetSnap := st.TgtName + st.SourceEnd
		if _, err := exec.List(ctx, req.Target, targetSnap, []string{"name"}, 0); err != nil {
			return fmt.Errorf("bookmark verify %s: %w", targetSnap, err)
		}
		name := st.SrcName + "#" + prefix + strings.TrimPrefix(st.SourceEnd, "@")
		if err := exec.Bookmark(ctx, req.Source, st.SrcName+st.SourceEnd, name); err != nil {
			return fmt.Errorf("bookmark %s: %w", name, err)
		}
	}
	return nil
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
