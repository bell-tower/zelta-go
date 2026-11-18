package zelta

import (
	"context"
	"errors"

	"zelta.space/internal/app"
	"zelta.space/internal/prune"
)

// PrunePlan is the set of snapshots selected for pruning.
type PrunePlan struct {
	// Datasets lists every compared dataset in suffix order.
	Datasets []PruneDataset
	// Destroyed lists the "zfs destroy" arguments Prune ran successfully.
	Destroyed []string
	// Skipped lists the approved snapshots ("pool/ds@snap") this plan left
	// out because they are no longer candidates. See PruneOptions.Approved.
	Skipped []string

	candidates []string
	guids      map[string]string
	plan       *prune.Plan
	settings   map[string]string
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
	// Snapshots lists every snapshot of the dataset, oldest first. Those in
	// neither Destroy nor Keep are at or newer than the guard match.
	Snapshots []Snapshot
	// ReclaimMin and ReclaimMax bound the space destroying Destroy frees,
	// from the snapshot listing alone. They are equal when that is exact;
	// otherwise Client.PruneReclaim measures it.
	ReclaimMin, ReclaimMax int64
}

// PruneReclaim returns the space destroying plan.Datasets[i].Destroy frees,
// as "zfs destroy -nvp" would report it, without destroying anything. It
// reads "written@" once for each run of consecutive snapshots whose
// ReclaimMin and ReclaimMax differ, so a planner can show the bounds at
// once and the exact figure when it arrives. Destroy may have been edited.
func (c *Client) PruneReclaim(ctx context.Context, plan *PrunePlan, i int) (int64, error) {
	if plan == nil || plan.plan == nil || i < 0 || i >= len(plan.Datasets) {
		return 0, errors.New("zelta: PruneReclaim needs a dataset of a plan from PlanPrune")
	}
	o, err := c.start(ctx, "prune", plan.settings)
	if err != nil {
		return 0, err
	}
	p := plan.plan
	n, err := p.Datasets[i].Reclaim(ctx, o.rt, p.Source, plan.Datasets[i].Destroy)
	return n, o.finish(err)
}

// Candidates returns the snapshots to destroy as "zelta prune" prints them:
// one "pool/ds@snap" or "pool/ds@old%new" range per line.
func (p *PrunePlan) Candidates() []string { return append([]string(nil), p.candidates...) }

// approved maps the snapshots in Datasets[].Destroy to the GUIDs this plan
// listed. Snapshots a caller added to Destroy have no GUID and are left out.
func (p *PrunePlan) approved() map[string]string {
	out := map[string]string{}
	for _, d := range p.Datasets {
		for _, s := range d.Destroy {
			if g := p.guids[d.Name+s]; g != "" {
				out[d.Name+s] = g
			}
		}
	}
	return out
}

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
	var skipped []string
	if opts.Approved != nil {
		skipped = plan.Restrict(opts.Approved.approved())
	}
	out := &PrunePlan{Skipped: skipped, candidates: plan.Candidates(), guids: plan.SelectedGUIDs(), plan: plan, settings: s}
	for _, d := range plan.Datasets {
		pd := PruneDataset{Name: d.Name, Suffix: d.Suffix, Destroy: d.Selected(), Keep: d.Protected()}
		for _, r := range d.Snapshots() {
			pd.Snapshots = append(pd.Snapshots, snapshotOf(r))
		}
		pd.ReclaimMin, pd.ReclaimMax = d.ReclaimBounds(pd.Destroy)
		out.Datasets = append(out.Datasets, pd)
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
