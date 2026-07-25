// Package rotate plans safe root dataset rotation.
package rotate

import (
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
	"git.belltower.it/djbell/zelta-go/internal/opt"
)

type Request struct {
	Source, Target string
	Match          string
	SourceLast     string
	TargetLast     string
	SourceOrigin   string
	OriginVerified bool
	SourceType     string
	Intermediate   bool
	Flags          opt.SendRecv
}

type Step struct {
	Kind string
	Argv []string
}

// Plan handles root direct-match and verified source-origin paths. It remains a
// planner only; execution waits for recursive goldens and lifecycle review.
func Plan(req Request) ([]Step, error) {
	if req.Source == "" || req.Target == "" {
		return nil, fmt.Errorf("rotate requires source and target")
	}
	sourceStart := req.Source + req.Match
	if req.Match == "" {
		originDS, originSnap, ok := splitOrigin(req.SourceOrigin)
		if !ok || !req.OriginVerified {
			return nil, fmt.Errorf("rotate has no verified common snapshot or source origin")
		}
		req.Match = originSnap
		sourceStart = originDS + originSnap
	}
	if req.SourceLast == "" || req.SourceLast == req.Match {
		return nil, fmt.Errorf("rotate source is up-to-date or has no new snapshot")
	}
	if req.TargetLast == "" || req.TargetLast == req.Match {
		return nil, fmt.Errorf("rotate target is not divergent")
	}
	preserved := req.Target + "_" + strings.TrimPrefix(req.Match, "@")
	rename, err := cmdbuild.RenameArgv(req.Target, preserved)
	if err != nil {
		return nil, err
	}
	send, err := cmdbuild.Build("SEND", map[string]string{
		"flags":     req.Flags.SendFlags(),
		"intr_snap": incrFlag(req.Intermediate) + " " + sourceStart,
		"ds_snap":   req.Source + req.SourceLast,
	})
	if err != nil {
		return nil, err
	}
	recv, err := cmdbuild.Build("RECV", map[string]string{
		"flags": recvFlags(req.Flags, req.SourceType, preserved+req.Match),
		"ds":    req.Target,
	})
	if err != nil {
		return nil, err
	}
	return []Step{{Kind: "rename", Argv: rename}, {Kind: "send", Argv: send}, {Kind: "recv", Argv: recv}}, nil
}

func splitOrigin(origin string) (string, string, bool) {
	i := strings.LastIndex(origin, "@")
	if i <= 0 || i == len(origin)-1 {
		return "", "", false
	}
	return origin[:i], origin[i:], true
}

func incrFlag(intermediate bool) string {
	if intermediate {
		return "-I"
	}
	return "-i"
}

func recvFlags(f opt.SendRecv, sourceType, origin string) string {
	if f.RecvOverride != "" {
		return f.RecvOverride
	}
	var parts []string
	if f.RecvDefault != "" {
		parts = append(parts, f.RecvDefault)
	}
	if f.RecvTop != "" {
		parts = append(parts, f.RecvTop)
	}
	if sourceType == "volume" {
		if f.RecvVol != "" {
			parts = append(parts, f.RecvVol)
		}
	} else if f.RecvFS != "" {
		parts = append(parts, f.RecvFS)
	}
	for _, prop := range f.RecvPropsAdd {
		if prop != "" {
			parts = append(parts, "-o "+prop)
		}
	}
	for _, prop := range f.RecvPropsDel {
		if prop != "" {
			parts = append(parts, "-x "+prop)
		}
	}
	if f.Resume && f.RecvPartial != "" {
		parts = append(parts, f.RecvPartial)
	}
	parts = append(parts, "-o origin="+origin)
	return strings.Join(parts, " ")
}

func Format(steps []Step) string {
	var b strings.Builder
	for _, step := range steps {
		if len(step.Argv) == 0 {
			continue
		}
		b.WriteString(strings.Join(step.Argv, " "))
		b.WriteByte('\n')
	}
	return b.String()
}
