package prune

import (
	"context"
	"io"

	"zelta.space/internal/app"
	"zelta.space/internal/logging"
)

// Run implements "zelta prune ENDPOINT [GUARD]": it reports prune candidates
// as snapshot names or ranges, one per line, and never destroys anything.
func Run(ctx context.Context, rt *app.Runtime, args []string) error {
	plan, err := Build(ctx, rt, args, rt.DryRun())
	if err != nil || plan == nil {
		return err
	}
	if rt.Opts.Bool("PRUNE_VISUAL") {
		_, err := io.WriteString(rt.Stdout, plan.Visual())
		return err
	}
	plan.report(rt.Log)
	return nil
}

// report logs the kept snapshots (info) and the candidates (notices).
func (p *Plan) report(log *logging.Logger) {
	if p.Empty {
		log.Errorf("datasets inaccessible or do not exist")
		return
	}
	if k := p.Kept(); k != "" {
		log.Infof("keeping: %s", k)
	}
	for _, c := range p.Candidates() {
		log.Log(logging.Notice, c)
	}
}
