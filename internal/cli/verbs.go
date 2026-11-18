package cli

import (
	"context"

	"zelta.space/internal/app"
)

// Engine runs a command with its positional operands.
type Engine func(ctx context.Context, rt *app.Runtime, args []string) error

// operand completion kinds
const (
	opEndpoint = "endpoint"
	opPolicy   = "policy"
	opNone     = "none"
)

// verb describes a Zelta subcommand.
type verb struct {
	name    string
	group   string
	usage   string // operand synopsis
	short   string
	long    string
	engine  string // key into App.Engines; defaults to name
	opVerb  string // option-registry verb; defaults to name
	maxOps  int    // operands to complete; 0 for unlimited
	operand string
	hidden  bool
}

func (v verb) engineKey() string {
	if v.engine != "" {
		return v.engine
	}
	return v.name
}

func (v verb) optionVerb() string {
	if v.opVerb != "" {
		return v.opVerb
	}
	return v.name
}

// command groups, in display order
const (
	groupCompare  = "Comparison"
	groupBackup   = "Backup"
	groupRecovery = "Recovery"
	groupPromote  = "Promotion"
	groupRetain   = "Retention"
	groupAuto     = "Automation"
)

var groupOrder = []string{groupCompare, groupBackup, groupRecovery, groupPromote, groupRetain, groupAuto}

var verbs = []verb{
	{name: "match", group: groupCompare, usage: "SOURCE [TARGET]", short: "Compare datasets", maxOps: 2, operand: opEndpoint,
		long: "Recursively compare a dataset tree with its replica by snapshot GUID and report matching snapshots, sync state, and sizes."},
	{name: "backup", group: groupBackup, usage: "SOURCE TARGET", short: "Sync ZFS datasets", maxOps: 2, operand: opEndpoint,
		long: "Recursively replicate snapshots from SOURCE to TARGET, creating a snapshot and the target's parent when needed and choosing safe zfs send and receive options automatically."},
	{name: "sync", group: groupBackup, usage: "SOURCE TARGET", short: "Sync only the latest snapshots (backup -i)", engine: "backup", maxOps: 2, operand: opEndpoint, hidden: true,
		long: "Compatibility alias for 'zelta backup -i': replicate only the latest snapshots."},
	{name: "replicate", group: groupBackup, usage: "SOURCE TARGET", short: "Replicate with zfs send -R (backup -R)", engine: "backup", maxOps: 2, operand: opEndpoint, hidden: true,
		long: "Compatibility alias for 'zelta backup -R': replicate with 'zfs send --replicate'."},
	{name: "snapshot", group: groupBackup, usage: "ENDPOINT", short: "Snapshot a dataset tree", maxOps: 1, operand: opEndpoint,
		long: "Create a recursive snapshot of a local or remote dataset tree."},
	{name: "policy", group: groupAuto, usage: "[SITE|HOST|DATASET ...]", short: "Run configured replication jobs", operand: opPolicy,
		long: "Run the backup jobs defined in zelta.conf, optionally limited to the given sites, hosts, or datasets."},
	{name: "clone", group: groupRecovery, usage: "SOURCE TARGET [ORIGIN-BACKUP TARGET-BACKUP]", short: "Clone ZFS datasets", maxOps: 4, operand: opEndpoint,
		long: "Recursively clone a dataset tree from its latest or given snapshot. With four endpoints, also back up the new clone using the origin's existing backup."},
	{name: "revert", group: groupRecovery, usage: "ENDPOINT", short: "Rename and clone a dataset in-place", maxOps: 1, operand: opEndpoint,
		long: "Rewind a dataset tree to a snapshot by renaming it out of the way and cloning the snapshot back in place, preserving the current state."},
	{name: "rotate", group: groupRecovery, usage: "SOURCE TARGET", short: "Recover sync continuity", maxOps: 2, operand: opEndpoint,
		long: "Rename a diverged replica and rebuild it from the latest common snapshot (or the source's origin) without destroying anything."},
	{name: "rebase", group: groupRecovery, usage: "UPSTREAM TARGET", short: "Rebase a dataset onto an upgraded upstream", maxOps: 2, operand: opEndpoint,
		long: "Replace a dataset with one based on an upgraded upstream while preserving incremental backup continuity."},
	{name: "lock", group: groupPromote, usage: "ENDPOINT ...", short: "Make datasets read-only and unmount them", operand: opEndpoint,
		long: "Set readonly and canmount=noauto on a dataset tree and unmount it."},
	{name: "unlock", group: groupPromote, usage: "ENDPOINT ...", short: "Inherit readonly and mount datasets", operand: opEndpoint,
		long: "Reverse 'zelta lock': inherit readonly, set canmount=on, and mount the dataset tree."},
	{name: "failover", group: groupPromote, usage: "SOURCE TARGET", short: "Promote a target dataset tree", maxOps: 2, operand: opEndpoint,
		long: "Lock the active source, perform a final backup, sync local properties, and unlock the promoted target."},
	{name: "propsync", group: groupPromote, usage: "SOURCE TARGET", short: "Sync local ZFS properties between trees", maxOps: 2, operand: opEndpoint,
		long: "Replay local ZFS properties from one dataset tree onto another, inheriting target-only local properties."},
	{name: "prune", group: groupRetain, usage: "ENDPOINT [GUARD]", short: "Plan snapshot pruning", maxOps: 2, operand: opEndpoint,
		long: "Report snapshots that are candidates for pruning without destroying anything. Use zprune to destroy them."},
	{name: "zprune", group: groupRetain, usage: "ENDPOINT", short: "Destroy snapshots selected by zelta prune", opVerb: "prune", maxOps: 1, operand: opEndpoint,
		long: "Preview, confirm, and destroy the snapshots selected by 'zelta prune'. Requires --match-endpoint or --no-prune-guard."},
	{name: "report", group: groupRetain, usage: "[ENDPOINT ...]", short: "Report datasets with stale snapshots", operand: opEndpoint, hidden: true,
		long: "Report backup datasets whose newest snapshot is more than a day old (zeport)."},
}

// legacyNames maps historical executable names to subcommands.
var legacyNames = map[string]string{
	"zmatch": "match",
	"zpull":  "backup",
	"zpush":  "backup",
	"zsync":  "sync",
	"zeport": "report",
	"zp":     "policy",
	"zprune": "zprune",
}

func findVerb(name string) (verb, bool) {
	for _, v := range verbs {
		if v.name == name {
			return v, true
		}
	}
	return verb{}, false
}
