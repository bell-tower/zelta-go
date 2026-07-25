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
	Flags          opt.SendRecv
}

type Step struct {
	Kind string
	Argv []string
}

// Plan handles only the direct-common-snapshot root path. Other lineage paths
// must be added explicitly once origin inspection and receive-origin flags are
// available.
func Plan(req Request) ([]Step, error) {
	if req.Source == "" || req.Target == "" || req.Match == "" {
		return nil, fmt.Errorf("rotate requires source, target, and a common snapshot")
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
		"intr_snap": "-I " + req.Source + req.Match,
		"ds_snap":   req.Source + req.SourceLast,
	})
	if err != nil {
		return nil, err
	}
	recv, err := cmdbuild.Build("RECV", map[string]string{
		"flags": req.Flags.RecvOverride,
		"ds":    req.Target,
	})
	if err != nil {
		return nil, err
	}
	return []Step{{Kind: "rename", Argv: rename}, {Kind: "send", Argv: send}, {Kind: "recv", Argv: recv}}, nil
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
