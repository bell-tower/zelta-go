package backup

import (
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/match"
	"git.belltower.it/djbell/zelta-go/internal/opt"
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
	DSSuffix    string
	Kind        Kind
	Info        string
	Match       string
	SourceStart string // match savepoint (@/#) for incr; empty = full
	SourceEnd   string // first-pass send end (earliest on intermediate full)
	FinalEnd    string // if set after full+intermediate: second-pass end (latest)
	SrcName     string
	TgtName     string
	SrcWritten  string
	SrcType     string // filesystem (default) or volume
	Send        []string
	Recv        []string
	Notice      string
}

// Plan is the full backup plan from a match result.
type Plan struct {
	Steps []*Step
	Full  int
	Incr  int
	Skip  int
	Block int
	// Flags drive SEND/RECV fragments (from opt.Resolve or Request).
	Flags opt.SendRecv
	// Snap is set when a source snapshot is planned (@name without dataset).
	SnapSavepoint string
	SnapReason    string
	SnapArgv      []string // zfs snapshot -r ds@snap
}

// PairView is the match fields backup needs (decoupled for tests).
type PairView struct {
	DSSuffix   string
	Info       string
	Match      string
	SrcLast    string
	SrcNext    string
	TgtLast    string
	SrcName    string
	TgtName    string
	SrcWritten string
	SrcType    string // filesystem | volume; empty → filesystem
}

// ViewsFromMatch maps match.Pair → PairView.
func ViewsFromMatch(pairs []*match.Pair) []PairView {
	out := make([]PairView, 0, len(pairs))
	for _, p := range pairs {
		out = append(out, PairView{
			DSSuffix:   p.DSSuffix,
			Info:       p.Info,
			Match:      p.Match,
			SrcLast:    p.SrcLast,
			SrcNext:    p.SrcNext,
			TgtLast:    p.TgtLast,
			SrcName:    p.SrcName,
			TgtName:    p.TgtName,
			SrcWritten: p.SrcWritten,
			SrcType:    p.SrcType,
		})
	}
	return out
}

// PlanFromMatch builds send/recv steps from match pairs (no execution).
// intermediate: true → -I (default); false → -i.
// flags come from opt.Default() or opt.Resolve() (env).
func PlanFromMatch(pairs []PairView, intermediate bool, flags opt.SendRecv) (*Plan, error) {
	p := &Plan{Flags: flags}
	for _, v := range pairs {
		st, err := planPair(v, intermediate, flags)
		if err != nil {
			return nil, err
		}
		p.Steps = append(p.Steps, st)
	}
	p.recount()
	return p, nil
}

func planPair(v PairView, intermediate bool, flags opt.SendRecv) (*Step, error) {
	st := &Step{
		DSSuffix:   v.DSSuffix,
		Info:       v.Info,
		Match:      v.Match,
		SrcName:    v.SrcName,
		TgtName:    v.TgtName,
		SrcWritten: v.SrcWritten,
		SrcType:    v.SrcType,
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
	case strings.HasPrefix(v.Info, "blocked") || v.Info == "no source (target only)":
		st.Kind = KindBlocked
		st.Notice = v.Info
		return st, nil
	default:
		st.Kind = KindSkip
		st.Notice = v.Info
		return st, nil
	}

	if st.SourceEnd == "" {
		st.Kind = KindBlocked
		st.Notice = "no source snapshot to send"
		return st, nil
	}
	if err := buildCmds(st, intermediate, flags); err != nil {
		return nil, err
	}
	return st, nil
}

func buildCmds(st *Step, intermediate bool, flags opt.SendRecv) error {
	srcSnap := st.SrcName + st.SourceEnd
	vars := map[string]string{
		"flags":   flags.SendFlags(),
		"ds_snap": srcSnap,
	}
	if st.Kind == KindIncremental && st.SourceStart != "" {
		flag := "-i"
		if intermediate {
			flag = "-I"
		}
		vars["intr_snap"] = flag + " " + st.SrcName + st.SourceStart
	}
	send, err := cmdbuild.Build("SEND", vars)
	if err != nil {
		return err
	}
	st.Send = send

	tgt := st.TgtName
	if tgt == "" {
		return fmt.Errorf("backup: empty target name for %q", st.DSSuffix)
	}
	recv, err := cmdbuild.Build("RECV", map[string]string{
		"flags": recvFlags(st, flags),
		"ds":    tgt,
	})
	if err != nil {
		return err
	}
	st.Recv = recv
	return nil
}

// recvFlags: RECV_DEFAULT; full → TOP (root) + FS/VOL; + PARTIAL when Resume.
// RECV_OVERRIDE replaces the whole fragment.
func recvFlags(st *Step, f opt.SendRecv) string {
	if f.RecvOverride != "" {
		return f.RecvOverride
	}
	var parts []string
	if f.RecvDefault != "" {
		parts = append(parts, f.RecvDefault)
	}
	if st.Kind == KindFull {
		if st.DSSuffix == "" && f.RecvTop != "" {
			parts = append(parts, f.RecvTop)
		}
		switch st.SrcType {
		case "volume":
			if f.RecvVol != "" {
				parts = append(parts, f.RecvVol)
			}
		default: // filesystem
			if f.RecvFS != "" {
				parts = append(parts, f.RecvFS)
			}
		}
	}
	if f.Resume && f.RecvPartial != "" {
		parts = append(parts, f.RecvPartial)
	}
	return strings.Join(parts, " ")
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
	return nil
}

func applySnapToStep(st *Step, savepoint string, intermediate bool, flags opt.SendRecv) error {
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
