package zelta

import (
	"context"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/prune"
)

// PrunePlan is the set of snapshots selected for pruning.
type PrunePlan struct {
	// Datasets lists every compared dataset in suffix order.
	Datasets []PruneDataset
	// Destroyed lists the "zfs destroy" arguments Prune ran successfully.
	Destroyed []string

	candidates []string
}

// PruneDataset is the plan for one dataset.
type PruneDataset struct {
	// Name is the full dataset name and Suffix its name relative to the
	// endpoint.
	Name, Suffix string
	// Destroy lists the selected snapshots ("@name"), oldest first.
	Destroy []string
	// Keep lists the snapshots older than the guard match that retention
	// keeps, oldest first.
	Keep []string
}

// Candidates returns the snapshots to destroy as "zelta prune" prints them:
// one "pool/ds@snap" or "pool/ds@old%new" range per line.
func (p *PrunePlan) Candidates() []string { return append([]string(nil), p.candidates...) }

// PlanPrune selects the snapshots of a dataset tree that retention allows
// destroying, without destroying anything.
func (c *Client) PlanPrune(ctx context.Context, endpoint string, opts *PruneOptions) (*PrunePlan, error) {
	return c.prune(ctx, endpoint, opts, false)
}

// Prune selects snapshots like PlanPrune and destroys them. It refuses to
// run without a Guard endpoint unless GuardMode is GuardNone.
func (c *Client) Prune(ctx context.Context, endpoint string, opts *PruneOptions) (*PrunePlan, error) {
	return c.prune(ctx, endpoint, opts, true)
}

func (c *Client) prune(ctx context.Context, endpoint string, opts *PruneOptions, destroy bool) (*PrunePlan, error) {
	if opts == nil {
		opts = &PruneOptions{}
	}
	s := opts.settings()
	if !destroy {
		delete(s, "DRYRUN")
	}
	o, err := c.start(ctx, "prune", s)
	if err != nil {
		return nil, err
	}
	plan, err := buildPrune(ctx, o.rt, endpoint)
	if err != nil {
		return nil, o.finish(err)
	}
	out := &PrunePlan{candidates: plan.Candidates()}
	for _, d := range plan.Datasets {
		out.Datasets = append(out.Datasets, PruneDataset{Name: d.Name, Suffix: d.Suffix, Destroy: d.Selected(), Keep: d.Protected()})
	}
	if destroy {
		out.Destroyed, err = prune.Destroy(ctx, o.rt, plan)
	}
	return out, o.finish(err)
}

func buildPrune(ctx context.Context, rt *app.Runtime, endpoint string) (*prune.Plan, error) {
	if endpoint == "" {
		return nil, app.Usage("missing endpoint")
	}
	plan, err := prune.Build(ctx, rt, []string{endpoint}, false)
	if err != nil {
		return nil, err
	}
	if plan.Empty {
		return nil, app.Fail("datasets inaccessible or do not exist")
	}
	return plan, nil
}
