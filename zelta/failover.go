package zelta

import (
	"context"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/backup"
	"github.com/bell-tower/zelta-go/internal/failover"
	"github.com/bell-tower/zelta-go/internal/policy"
)

// Lock makes a dataset tree read-only and unmounts it.
func (c *Client) Lock(ctx context.Context, endpoint string, opts *LockOptions) error {
	if opts == nil {
		opts = &LockOptions{}
	}
	return c.run(ctx, "lock", opts.settings(), failover.Lock, endpoint)
}

// Unlock reverses Lock: it inherits readonly, sets canmount=on, and mounts
// the dataset tree.
func (c *Client) Unlock(ctx context.Context, endpoint string, opts *BasicOptions) error {
	if opts == nil {
		opts = &BasicOptions{}
	}
	return c.run(ctx, "unlock", opts.settings(), failover.Unlock, endpoint)
}

// Failover promotes a replica: it locks the source, runs a final backup
// with a fresh snapshot, replays local properties onto the target, and
// unlocks the target.
func (c *Client) Failover(ctx context.Context, source, target string, opts *LockOptions) error {
	if opts == nil {
		opts = &LockOptions{}
	}
	return c.run(ctx, "failover", opts.settings(), failover.Failover, source, target)
}

// Propsync replays local ZFS properties from one dataset tree onto
// another, inheriting target-only local properties.
func (c *Client) Propsync(ctx context.Context, source, target string, opts *BasicOptions) error {
	if opts == nil {
		opts = &BasicOptions{}
	}
	return c.run(ctx, "propsync", opts.settings(), failover.Propsync, source, target)
}

// Policy runs the backup jobs of a policy file (zelta.conf), optionally
// limited to the named sites, hosts, or datasets. Job messages are logged
// with each job's prefix.
func (c *Client) Policy(ctx context.Context, opts *PolicyOptions, selectors ...string) error {
	if opts == nil {
		opts = &PolicyOptions{}
	}
	o, err := c.start(ctx, "policy", opts.settings())
	if err != nil {
		return err
	}
	deps := policy.Deps{Loader: o.loader, CLI: o.loader.Overrides, Backup: backup.Run}
	return o.finish(policy.Run(ctx, o.rt, selectors, deps))
}

type engine func(context.Context, *app.Runtime, []string) error

func (c *Client) run(ctx context.Context, verb string, settings map[string]string, fn engine, args ...string) error {
	o, err := c.start(ctx, verb, settings)
	if err != nil {
		return err
	}
	return o.finish(fn(ctx, o.rt, operands(args...)))
}
