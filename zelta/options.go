package zelta

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/bell-tower/zelta-go/internal/config"
)

// Filter limits the datasets and snapshots an operation considers.
type Filter struct {
	// Depth limits recursion: 1 is the endpoint's dataset only. Zero is
	// unlimited.
	Depth int
	// Exclude skips datasets or snapshots matching these patterns, as
	// with --exclude: "*/tmp", "@hourly_*", or an absolute dataset name.
	Exclude []string
	// Include considers only matching datasets or snapshots, as with
	// --include.
	Include []string
}

func (f Filter) apply(s map[string]string) {
	if f.Depth != 0 {
		s["DEPTH"] = strconv.Itoa(f.Depth)
	}
	if len(f.Exclude) > 0 {
		s["EXCLUDE"] = strings.Join(f.Exclude, ",")
	}
	if len(f.Include) > 0 {
		s["INCLUDE"] = strings.Join(f.Include, ",")
	}
}

// SnapshotMode controls the source snapshot a backup takes.
type SnapshotMode string

// Snapshot modes.
const (
	// SnapshotDefault uses the configured SNAP_MODE (IF_NEEDED unless set).
	SnapshotDefault SnapshotMode = ""
	// SnapshotIfNeeded snapshots when the source has changes that are not
	// yet in a snapshot, or has no snapshot.
	SnapshotIfNeeded SnapshotMode = "IF_NEEDED"
	// SnapshotAlways always snapshots, and fails if it cannot.
	SnapshotAlways SnapshotMode = "ALWAYS"
	// SnapshotNever never snapshots.
	SnapshotNever SnapshotMode = "0"
)

// SnapshotPolicy describes the source snapshot taken before a sync.
type SnapshotPolicy struct {
	Mode SnapshotMode
	// Name is the snapshot name without "@": letters, digits, and "_.:-".
	// The default is "zelta_YYYY-MM-DD_HH.MM.SS" (UTC). SNAP_PREFIX, when
	// set, replaces a leading "zelta" or is prepended. For a name computed
	// by a shell command, set SNAP_NAME to "$(command)" in Settings.
	Name string
	// MinInterval skips the snapshot when every dataset already has one
	// this recent (SNAP_TIME).
	MinInterval time.Duration
	// MinWritten skips the snapshot unless at least this many bytes were
	// written since the last one (SNAP_SIZE).
	MinWritten int64
}

func (p SnapshotPolicy) apply(s map[string]string) {
	if p.Mode != SnapshotDefault {
		s["SNAP_MODE"] = string(p.Mode)
	}
	if p.Name != "" {
		s["SNAP_NAME"] = p.Name
	}
	if p.MinInterval > 0 {
		s["SNAP_TIME"] = seconds(p.MinInterval)
	}
	if p.MinWritten > 0 {
		s["SNAP_SIZE"] = strconv.FormatInt(p.MinWritten, 10)
	}
}

func seconds(d time.Duration) string { return strconv.FormatInt(int64(d/time.Second), 10) + "s" }

// Direction chooses which host initiates a sync between two remote
// endpoints.
type Direction string

// Sync directions.
const (
	// DirectionDefault uses the configured SYNC_DIRECTION (PULL unless set).
	DirectionDefault Direction = ""
	// Pull runs "zfs send" over ssh from the target host.
	Pull Direction = "PULL"
	// Push runs "zfs recv" over ssh from the source host.
	Push Direction = "PUSH"
	// Proxy streams through this host.
	Proxy Direction = "0"
)

// BackupOptions configure Backup and Rotate.
type BackupOptions struct {
	Filter
	// Snapshot controls the source snapshot taken before syncing.
	Snapshot SnapshotPolicy
	// Incremental syncs only the newest snapshots instead of every
	// intermediate snapshot (-i).
	Incremental bool
	// Replicate uses "zfs send --replicate" (-R). Backup only.
	Replicate bool
	// Parallel syncs up to this many datasets at a time (default 1).
	Parallel int
	// Direction applies when both endpoints are remote.
	Direction Direction
	// Bookmark bookmarks successfully replicated source snapshots.
	Bookmark bool
	// NoResume discards failed partial transfers instead of saving them.
	NoResume bool
	// Origin is a clone origin already backed up on the target, for
	// backing up a clone incrementally (--target-origin).
	Origin string
	// DryRun logs the commands that would change anything, at notice
	// level, without running them.
	DryRun bool
	// Settings are further options by name; see WithSettings.
	Settings map[string]string
}

func (o *BackupOptions) settings() map[string]string {
	s := base(o.Settings, o.DryRun)
	o.Filter.apply(s)
	o.Snapshot.apply(s)
	if o.Incremental {
		s["SEND_INTR"] = "0"
	}
	if o.Parallel != 0 {
		s["PARALLEL"] = strconv.Itoa(o.Parallel)
	}
	if o.Direction != DirectionDefault {
		s["SYNC_DIRECTION"] = string(o.Direction)
	}
	if o.Bookmark {
		s["BOOKMARK_MODE"] = "1"
	}
	if o.NoResume {
		s["RESUME"] = "0"
	}
	if o.Origin != "" {
		s["ORIGIN_ID"] = o.Origin
	}
	return s
}

// SnapshotOptions configure Snapshot.
type SnapshotOptions struct {
	// Name is the snapshot name, as in SnapshotPolicy; a name in the
	// endpoint takes precedence.
	Name string
	// MinInterval and MinWritten skip the snapshot as in SnapshotPolicy.
	MinInterval time.Duration
	MinWritten  int64
	DryRun      bool
	Settings    map[string]string
}

func (o *SnapshotOptions) settings() map[string]string {
	s := base(o.Settings, o.DryRun)
	SnapshotPolicy{Name: o.Name, MinInterval: o.MinInterval, MinWritten: o.MinWritten}.apply(s)
	return s
}

// MatchOptions configure Match.
type MatchOptions struct {
	Filter
	// NoSizes skips size calculations for a faster listing.
	NoSizes  bool
	Settings map[string]string
}

func (o *MatchOptions) settings() map[string]string {
	s := base(o.Settings, false)
	o.Filter.apply(s)
	if o.NoSizes {
		s["LIST_WRITTEN"] = "0"
	}
	return s
}

// GuardMode is how a prune protects snapshots the replica needs.
type GuardMode string

// Prune guard modes.
const (
	// GuardDefault uses the configured PRUNE_GUARD (latest unless set).
	GuardDefault GuardMode = ""
	// GuardLatest keeps the newest snapshot shared with the guard
	// endpoint and everything newer.
	GuardLatest GuardMode = "latest"
	// GuardUnsynced also keeps older snapshots the guard endpoint lacks.
	GuardUnsynced GuardMode = "unsynced"
	// GuardNone disables the guard.
	GuardNone GuardMode = "none"
)

// PruneOptions configure PlanPrune and Prune. With no retention option
// (Keep, KeepFor, Grid, Reclaim) the newest 30 snapshots and those from
// the last 30 days are kept.
type PruneOptions struct {
	Filter
	// Guard is the replica (match endpoint) whose sync state protects
	// snapshots. Prune requires it unless GuardMode is GuardNone.
	Guard     string
	GuardMode GuardMode
	// Keep keeps the newest Keep snapshots older than the guard match.
	Keep int
	// KeepFor keeps snapshots newer than this.
	KeepFor time.Duration
	// Grid is a GFS retention grid such as "30x1 day, 52x1 week, 1 year".
	Grid string
	// Reclaim selects the oldest eligible snapshots until destroying them
	// would free at least this many bytes.
	Reclaim int64
	// Name limits pruning to snapshots matching a glob, or one of
	// "zfsnap", "truenas", or "sanoid".
	Name string
	// DryRun, for Prune, logs the destroy commands without running them.
	DryRun   bool
	Settings map[string]string
}

func (o *PruneOptions) settings() map[string]string {
	s := base(o.Settings, o.DryRun)
	o.Filter.apply(s)
	if o.Guard != "" {
		s["MATCH_ENDPOINT"] = o.Guard
	}
	if o.GuardMode != GuardDefault {
		s["PRUNE_GUARD"] = string(o.GuardMode)
	}
	if o.Keep != 0 {
		s["PRUNE_NUM"] = strconv.Itoa(o.Keep)
	}
	if o.KeepFor > 0 {
		s["PRUNE_TIME"] = seconds(o.KeepFor)
	}
	if o.Grid != "" {
		s["PRUNE_GRID"] = o.Grid
	}
	if o.Reclaim > 0 {
		s["PRUNE_SIZE"] = strconv.FormatInt(o.Reclaim, 10)
	}
	if o.Name != "" {
		s["PRUNE_NAME"] = o.Name
	}
	return s
}

// CloneOptions configure Clone.
type CloneOptions struct {
	Filter
	// OriginBackup and TargetBackup, when both set, also back up the new
	// clone to TargetBackup using the origin's existing backup at
	// OriginBackup.
	OriginBackup, TargetBackup string
	DryRun                     bool
	Settings                   map[string]string
}

func (o *CloneOptions) settings() map[string]string {
	s := base(o.Settings, o.DryRun)
	o.Filter.apply(s)
	return s
}

// RevertOptions configure Revert.
type RevertOptions struct {
	// Force attempts a best-effort revert of an incomplete tree.
	Force    bool
	DryRun   bool
	Settings map[string]string
}

func (o *RevertOptions) settings() map[string]string {
	s := base(o.Settings, o.DryRun)
	if o.Force {
		s["REVERT_FORCE"] = "1"
	}
	return s
}

// LockOptions configure Lock and Failover.
type LockOptions struct {
	// ForceUnmount forces unmounting during the lock.
	ForceUnmount bool
	// NoUnmount leaves locked datasets mounted.
	NoUnmount bool
	DryRun    bool
	Settings  map[string]string
}

func (o *LockOptions) settings() map[string]string {
	s := base(o.Settings, o.DryRun)
	if o.ForceUnmount {
		s["FORCE_UNMOUNT"] = "1"
	}
	if o.NoUnmount {
		s["NO_UNMOUNT"] = "1"
	}
	return s
}

// BasicOptions configure operations without specific options: Unlock,
// Propsync, and Rebase.
type BasicOptions struct {
	DryRun   bool
	Settings map[string]string
}

func (o *BasicOptions) settings() map[string]string { return base(o.Settings, o.DryRun) }

// PolicyOptions configure Policy.
type PolicyOptions struct {
	// Config is the policy file; the default is ZELTA_CONFIG or
	// zelta.conf in the configuration directory.
	Config string
	// Jobs runs jobs for up to this many sites at once.
	Jobs     int
	DryRun   bool
	Settings map[string]string
}

func (o *PolicyOptions) settings() map[string]string {
	s := base(o.Settings, o.DryRun)
	if o.Config != "" {
		s["CONFIG"] = o.Config
	}
	if o.Jobs != 0 {
		s["JOBS"] = strconv.Itoa(o.Jobs)
	}
	return s
}

func base(settings map[string]string, dryRun bool) map[string]string {
	s := map[string]string{}
	for k, v := range settings {
		s[strings.TrimPrefix(k, config.EnvPrefix)] = v
	}
	if dryRun {
		s["DRYRUN"] = "1"
	}
	return s
}

var snapNameRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]+$`)

// checkSnapName rejects names Zelta would expand with the shell or zfs
// would refuse.
func checkSnapName(name string) error {
	if name != "" && !snapNameRe.MatchString(name) {
		return fmt.Errorf("zelta: invalid snapshot name %q", name)
	}
	return nil
}

// operands drops empty endpoints so the engines report them as missing.
func operands(args ...string) []string {
	var out []string
	for _, a := range args {
		if a != "" {
			out = append(out, a)
		}
	}
	return out
}
