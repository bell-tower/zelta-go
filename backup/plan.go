package backup

import (
	"context"
	"fmt"
	"strings"

	"github.com/bell-tower/zelta-go/cmdbuild"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/match"
	"github.com/bell-tower/zelta-go/zfs"
)

// Kind is the planned action for one dataset pair.
type Kind int

const (
	KindSkip Kind = iota
	KindFull
	KindIncremental
	KindBlocked
)

// Step is one dataset's send/recv plan.
type Step struct {
	DSSuffix      string
	Kind          Kind
	Info          string
	Match         string
	SourceStart   string // match savepoint (@/#) for incr; empty = full
	SourceEnd     string // first-pass send end (earliest on intermediate full)
	Origin        string // clone origin receive property, when using target-origin
	FinalEnd      string // if set after full+intermediate: second-pass end (latest)
	SrcName       string
	TgtName       string
	SrcWritten    string
	SrcType       string // filesystem (default) or volume
	SrcEncryption string
	TgtEncryption string
	MatchIVSet    string
	Send          []string
	Recv          []string
	Notice        string
	Warning       string
	Filtered      bool
	ResumeToken   string
}

// Plan is the full backup plan from a match result.
type Plan struct {
	Steps []*Step
	Full  int
	Incr  int
	Skip  int
	Block int
	// Flags drive SEND/RECV fragments (from Request.Flags or built-in defaults).
	Flags    SendRecv
	Warnings []string
	// Snap is set when a source snapshot is planned (@name without dataset).
	SnapSavepoint string
	SnapReason    string
	SnapArgv      []string // zfs snapshot -r ds@snap
	Bookmarks     []BookmarkPlan
}

// BookmarkPlan is the verification and creation pair for one received snap.
type BookmarkPlan struct {
	VerifyEndpoint string
	SourceEndpoint string
	Verify         []string
	Create         []string
}

// PairView is the match fields backup needs (decoupled for tests).
type PairView struct {
	DSSuffix              string
	Info                  string
	Match                 string
	SrcLast               string
	SrcNext               string
	TgtLast               string
	SrcName               string
	TgtName               string
	SrcWritten            string
	SrcEncryption         string
	TgtEncryption         string
	MatchIVSet            string
	SrcSnapshotsChanged   string
	SrcSavepoints         []string
	SrcOrigin             string
	TargetOrigin          string
	TgtReceiveResumeToken string
	FilteredEnds          []string
	FilteredActive        bool
	SrcType               string // filesystem | volume; empty → filesystem
}

// ViewsFromMatch maps match.Pair → PairView.
func ViewsFromMatch(pairs []*match.Pair) []PairView {
	out := make([]PairView, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, PairView{
			DSSuffix:              p.DSSuffix,
			Info:                  p.Info,
			Match:                 p.Match,
			SrcLast:               p.SrcLast,
			SrcNext:               p.SrcNext,
			TgtLast:               p.TgtLast,
			SrcName:               p.SrcName,
			TgtName:               p.TgtName,
			SrcWritten:            p.SrcWritten,
			SrcEncryption:         p.SrcEncryption,
			TgtEncryption:         p.TgtEncryption,
			MatchIVSet:            p.MatchIVSet,
			SrcSnapshotsChanged:   p.SrcSnapshotsChanged,
			SrcSavepoints:         append([]string(nil), p.SrcSavepoints...),
			SrcOrigin:             p.SrcOrigin,
			TgtReceiveResumeToken: p.TgtReceiveResumeToken,
			SrcType:               p.SrcType,
		})
	}
	return out
}

// PlanFromMatch builds send/recv steps from match pairs (no execution).
// intermediate: true → -I (default); false → -i.
// flags come from DefaultSendRecv() or an explicit Request.Flags value.
func PlanFromMatch(pairs []PairView, intermediate bool, flags SendRecv) (*Plan, error) {
	p := &Plan{Flags: flags}
	for _, v := range pairs {
		if v.FilteredActive {
			steps, err := planFilteredPair(v, flags)
			if err != nil {
				return nil, err
			}
			p.Steps = append(p.Steps, steps...)
			continue
		}
		st, err := planPair(v, intermediate, flags)
		if err != nil {
			return nil, err
		}
		p.Steps = append(p.Steps, st)
	}
	p.recount()
	p.refreshWarnings()
	return p, nil
}

func planPair(v PairView, intermediate bool, flags SendRecv) (*Step, error) {
	st := &Step{
		DSSuffix:      v.DSSuffix,
		Info:          v.Info,
		Match:         v.Match,
		SrcName:       v.SrcName,
		TgtName:       v.TgtName,
		SrcWritten:    v.SrcWritten,
		SrcType:       v.SrcType,
		SrcEncryption: v.SrcEncryption,
		TgtEncryption: v.TgtEncryption,
		MatchIVSet:    v.MatchIVSet,
		ResumeToken:   v.TgtReceiveResumeToken,
	}
	switch {
	case v.Info == "up-to-date":
		st.Kind = KindSkip
		st.Notice = "up-to-date"
		st.SourceStart = v.Match
		st.SourceEnd = v.SrcLast
		return st, nil
	case v.Info == "syncable (full)":
		st.Kind = KindFull
		// Intermediate full: first pass earliest (src_next); second pass → latest.
		if intermediate && v.SrcNext != "" && v.SrcNext != v.SrcLast {
			st.SourceEnd = v.SrcNext
			st.FinalEnd = v.SrcLast
		} else {
			st.SourceEnd = v.SrcLast
		}
	case v.Info == "syncable (incremental)":
		st.Kind = KindIncremental
		st.SourceStart = v.Match
		st.SourceEnd = v.SrcLast
	case v.Info == "syncable (resume)":
		st.Kind = KindIncremental
	case strings.HasPrefix(v.Info, "blocked") || v.Info == "no source (target only)":
		st.Kind = KindBlocked
		st.Notice = v.Info
		return st, nil
	default:
		st.Kind = KindSkip
		st.Notice = v.Info
		return st, nil
	}
	if v.SrcOrigin != "" && v.TargetOrigin != "" {
		originDS, originSnap, ok := endpoint.SplitOrigin(v.SrcOrigin)
		if !ok {
			return nil, fmt.Errorf("backup: invalid source clone origin %q", v.SrcOrigin)
		}
		st.Kind = KindIncremental
		st.SourceStart = originDS + originSnap
		st.Origin = v.TargetOrigin
	}

	if st.ResumeToken == "" && st.SourceEnd == "" {
		st.Kind = KindBlocked
		st.Notice = "no source snapshot to send"
		return st, nil
	}
	if err := buildCmds(st, intermediate, flags); err != nil {
		return nil, err
	}
	return st, nil
}

func planFilteredPair(v PairView, flags SendRecv) ([]*Step, error) {
	steps := make([]*Step, 0, len(v.FilteredEnds))
	start := v.Match
	for i, end := range v.FilteredEnds {
		st := &Step{
			DSSuffix: v.DSSuffix, Info: v.Info, Match: v.Match,
			SourceStart: start, SourceEnd: end, SrcName: v.SrcName,
			TgtName: v.TgtName, SrcWritten: v.SrcWritten, SrcType: v.SrcType,
			SrcEncryption: v.SrcEncryption, TgtEncryption: v.TgtEncryption, MatchIVSet: v.MatchIVSet,
			Filtered: true,
		}
		if v.Info == "syncable (full)" && i == 0 && start == "" {
			st.Kind = KindFull
			st.SourceStart = ""
		} else {
			st.Kind = KindIncremental
		}
		if err := buildCmds(st, false, flags); err != nil {
			return nil, err
		}
		steps = append(steps, st)
		start = end
	}
	return steps, nil
}

func buildCmds(st *Step, intermediate bool, flags SendRecv) error {
	if st.ResumeToken != "" {
		send, err := cmdbuild.ResumeSendArgv(st.ResumeToken)
		if err != nil {
			return err
		}
		st.Send = send
		return buildRecvCmd(st, flags)
	}
	srcSnap := st.SrcName + st.SourceEnd
	sendFlags := flags.SendFlags()
	if flags.SendOverride == "" {
		var warning string
		sendFlags, warning = compatibleSendFlags(st, sendFlags)
		st.Warning = warning
	}
	vars := map[string]string{
		"flags":   sendFlags,
		"ds_snap": srcSnap,
	}
	if st.Kind == KindIncremental && st.SourceStart != "" {
		flag := "-i"
		if intermediate && !st.Filtered && st.Origin == "" {
			flag = "-I"
		}
		start := st.SrcName + st.SourceStart
		if st.Origin != "" {
			start = st.SourceStart
		}
		vars["intr_snap"] = flag + " " + start
	}
	send, err := cmdbuild.Build("SEND", vars)
	if err != nil {
		return err
	}
	st.Send = send

	return buildRecvCmd(st, flags)
}

func buildRecvCmd(st *Step, flags SendRecv) error {
	if st.TgtName == "" {
		return fmt.Errorf("backup: empty target name for %q", st.DSSuffix)
	}
	vars := map[string]string{
		"flags": flags.RecvFlags(st.SrcType, st.DSSuffix == "", st.Kind == KindFull),
		"ds":    st.TgtName,
	}
	recv, err := cmdbuild.Build("RECV", vars)
	if err != nil {
		return err
	}
	if st.Origin != "" {
		// "-o origin=…" as its own argv pair before the target: the value may
		// contain spaces (dataset names with spaces) and must stay a single
		// element. A single "-o origin=…" token would make zfs recv read the
		// space after -o as part of the property name.
		end := len(recv) - 1
		recv = append(recv[:end], append([]string{"-o", "origin=" + st.Origin}, recv[end:]...)...)
	}
	st.Recv = recv
	return nil
}

func (p *Plan) recount() {
	p.Full, p.Incr, p.Skip, p.Block = 0, 0, 0, 0
	for _, st := range p.Steps {
		switch st.Kind {
		case KindFull:
			p.Full++
		case KindIncremental:
			p.Incr++
		case KindBlocked:
			p.Block++
		default:
			p.Skip++
		}
	}
}

// ApplySourceSnap updates ends after a real/predicted source snapshot @name.
func (p *Plan) ApplySourceSnap(savepoint string, intermediate bool) error {
	if savepoint == "" || savepoint[0] != '@' {
		return fmt.Errorf("backup: bad snap savepoint %q", savepoint)
	}
	p.SnapSavepoint = savepoint
	for _, st := range p.Steps {
		if err := applySnapToStep(st, savepoint, intermediate, p.Flags); err != nil {
			return err
		}
	}
	p.recount()
	p.refreshWarnings()
	return nil
}

func (p *Plan) refreshWarnings() {
	p.Warnings = nil
	for _, st := range p.Steps {
		if st.Warning != "" {
			p.Warnings = append(p.Warnings, st.Warning)
		}
	}
}

func applySnapToStep(st *Step, savepoint string, intermediate bool, flags SendRecv) error {
	switch st.Kind {
	case KindSkip:
		if st.Info != "up-to-date" {
			return nil
		}
		// New snap after match → incremental.
		st.Kind = KindIncremental
		if st.SourceStart == "" {
			st.SourceStart = st.Match
		}
		if st.SourceStart == "" {
			st.SourceStart = st.SourceEnd
		}
		st.SourceEnd = savepoint
		st.FinalEnd = ""
		st.Notice = ""
	case KindBlocked:
		if st.Notice != "no source snapshot to send" {
			return nil
		}
		st.Kind = KindFull
		st.SourceEnd = savepoint
		st.FinalEnd = ""
		st.Notice = ""
	case KindFull:
		// Keep intermediate first-pass end; new snap is second-pass target.
		if intermediate && st.SourceEnd != "" && st.SourceEnd != savepoint {
			st.FinalEnd = savepoint
		} else {
			st.SourceEnd = savepoint
			st.FinalEnd = ""
		}
	case KindIncremental:
		st.SourceEnd = savepoint
		st.FinalEnd = ""
	default:
		return nil
	}
	if st.SrcName == "" {
		return nil
	}
	return buildCmds(st, intermediate, flags)
}

// Summary is a short human line.
func (p *Plan) Summary() string {
	var parts []string
	if p.SnapSavepoint != "" {
		parts = append(parts, "snapshot "+p.SnapSavepoint)
	}
	if p.Full > 0 {
		parts = append(parts, fmt.Sprintf("%d full", p.Full))
	}
	if p.Incr > 0 {
		parts = append(parts, fmt.Sprintf("%d incremental", p.Incr))
	}
	if p.Skip > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", p.Skip))
	}
	if p.Block > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", p.Block))
	}
	return strings.Join(parts, ", ")
}

// Commands returns the oracle-shaped dry-run "+ …" command lines for the plan:
// the planned source snapshot (when SnapReason is set), one line per syncable
// dataset's send|recv pipe, and bookmark verify/create lines. The second pass
// of intermediate fulls is execute-time, matching the oracle dry-run.
func (p *Plan) Commands(srcEp, tgtEp, direction string) ([]string, error) {
	var out []string
	if p.SnapReason != "" && p.SnapSavepoint != "" && len(p.SnapArgv) > 0 {
		sh, err := zfs.SnapshotShell(srcEp, p.SnapArgv[len(p.SnapArgv)-1], hasRecursive(p.SnapArgv))
		if err != nil {
			return nil, err
		}
		out = append(out, "+ "+sh)
	}
	for _, st := range p.Steps {
		if st.Kind != KindFull && st.Kind != KindIncremental {
			continue
		}
		body, err := zfs.PipeShellDirection(srcEp, tgtEp, st.Send, st.Recv, direction)
		if err != nil {
			return nil, err
		}
		out = append(out, "+ "+body)
	}
	for _, bm := range p.Bookmarks {
		verify, err := zfs.CommandShell(bm.VerifyEndpoint, bm.Verify)
		if err != nil {
			return nil, err
		}
		create, err := zfs.CommandShell(bm.SourceEndpoint, bm.Create)
		if err != nil {
			return nil, err
		}
		out = append(out, "+ "+verify)
		out = append(out, "+ "+create)
	}
	return out, nil
}

// StreamCount returns the number of send streams the plan would execute and
// the received target dataset@snap names in order (JSON/telemetry parity).
func (p *Plan) StreamCount() (int, []string) {
	count := 0
	var names []string
	for _, st := range p.Steps {
		if st.Kind != KindFull && st.Kind != KindIncremental {
			continue
		}
		count++
		names = append(names, st.TgtName+st.SourceEnd)
	}
	return count, names
}

// RunStep executes the i-th plan step: the send|recv pipes for one dataset,
// including the second pass of an intermediate full. Snapshot and bookmark
// steps are plan-level — Run executes them around the step loop; RunStep
// callers handle plan.SnapArgv and plan.Bookmarks themselves.
func (p *Plan) RunStep(ctx context.Context, exec zfs.Executor, req Request, i int) error {
	if i < 0 || i >= len(p.Steps) {
		return fmt.Errorf("backup: step %d out of range (plan has %d steps)", i, len(p.Steps))
	}
	if st := p.Steps[i]; st.Kind != KindFull && st.Kind != KindIncremental {
		return fmt.Errorf("backup: step %d not executable (kind %d)", i, st.Kind)
	}
	return p.syncPair(ctx, exec, req, req.SyncDirection.PipeArg(), i, nil)
}

// syncPair executes one dataset's send|recv pipes, including the second pass
// of an intermediate full, appending executed command lines to commands when
// non-nil. Skip and blocked steps have no pipes and are no-ops (RunStep
// validates the kind before calling).
func (p *Plan) syncPair(ctx context.Context, exec zfs.Executor, req Request, direction string, i int, commands *[]string) error {
	st := p.Steps[i]
	if st.Kind != KindFull && st.Kind != KindIncremental {
		return nil
	}
	if err := runStep(ctx, exec, req, st, direction, commands); err != nil {
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
		if err := buildCmds(second, req.Intermediate, p.Flags); err != nil {
			return err
		}
		return runStep(ctx, exec, req, second, direction, commands)
	}
	return nil
}
