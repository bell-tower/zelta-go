// Package backup implements Zelta's replication verbs: backup, sync,
// replicate, rotate, clone, revert, rebase, and snapshot.
//
// After "zelta match" identifies the relationship between a source tree and
// its replica, the planner chooses snapshot, send, and receive options per
// dataset and runs one or more send/receive pipelines to bring the target up
// to date. Diverged targets are never rolled back; "zelta rotate" renames
// them and clones the replica forward instead.
package backup

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/match"
	"github.com/bell-tower/zelta-go/internal/units"
)

// Reasons a snapshot is needed in IF_NEEDED mode.
const (
	snapNone = iota
	snapAlways
	snapWritten
	snapMissing
	snapLatest
)

// side selects the source or target endpoint.
type side int

const (
	src side = iota
	tgt
)

func (s side) String() string {
	if s == src {
		return "SRC"
	}
	return "TGT"
}

// dataset holds what is known about one dataset of a tree.
type dataset struct {
	exists   bool
	props    map[string]string
	latest   string
	next     string
	earliest string
}

func (d *dataset) prop(key string) string {
	if d == nil {
		return ""
	}
	return d.props[key]
}

// pair is the replication relationship of one dataset suffix.
type pair struct {
	match             string
	matchIVSet        string
	sourceStart       string
	sourceEnd         string
	sourceOriginMatch string
	targetOrigin      string
}

// action is the plan for one dataset suffix.
type action struct {
	canSync, canRotate, resumable     bool
	sendDecrypted, warnedDecrypted    bool
	checkSourceOrigin, forceIncrement bool
	upToDate                          bool
	statusNotice, statusInfo          string
}

// treeState is per-tree planning state (DSTree in the Awk implementation).
type treeState struct {
	finalSnapshot           string
	targetExists            bool
	sourcePool, targetPool  string
	snapshotNeeded          int
	snapshotAttempted       bool
	sourceEncrypted         bool
	snapshotsChanged        int64
	snapshotsChangedMissing bool
	syncable, rotatable     int
	upToDate, needsSnapshot int
	noSource, diverged      int
	sourceSnapNum           int
	predictedSnapshot       string
	predictedApplied        bool
	warnedProxy             bool
}

// summary holds telemetry reported in JSON output.
type summary struct {
	startTime, endTime             int64
	finished                       bool
	sourceListTime, targetListTime float64
	sourceWritten                  int64
	targetsCloned, targetsResumed  int
	replicationSize                int64
	streamsSent, streamsReceived   int
	errorCode                      int
	replicationTime                float64
	// parallelSeconds is the elapsed time of parallel sync phases, which
	// the text summary reports instead of the summed stream times.
	parallelSeconds float64
	failedProps     []string
	sentStreams     []string
}

type bookmark struct {
	snap, targetSnap, sourceSnap string
}

// run is the state of one replication command.
type run struct {
	ctx     context.Context
	rt      *app.Runtime
	opts    *config.Options
	verb    string // effective verb; becomes "backup" during a clone backup
	runVerb string // verb the command started with
	args    []string

	src, tgt          endpoint.Endpoint
	origin            endpoint.Endpoint
	cloneOrigin       endpoint.Endpoint
	cloneBackup       endpoint.Endpoint
	depth             int
	recvPropsAdd      []string
	recvPropsDel      []string
	receivePipe       string
	snapName          string
	snapNameResolved  bool
	sendCheckDisabled map[string]bool

	ds      [2]map[string]*dataset
	pairs   map[string]*pair
	actions map[string]*action
	list    []string
	tree    treeState
	sum     summary
	marks   map[string]*bookmark
	srcTree *match.Tree

	// parallel is the number of datasets synced at a time (PARALLEL).
	parallel int
	// mu guards the run state while pipelines run concurrently. It is held
	// everywhere except while an external send/receive pipeline runs.
	mu sync.Mutex
	// failed marks datasets whose sync failed, for parallel runs.
	failed map[string]bool
	// synced marks datasets that received at least one stream.
	synced map[string]bool
	// created is the snapshot this run created (or would create).
	created string
}

// Run implements backup, sync, replicate, rotate, clone, and revert, selected
// by the VERB option.
func Run(ctx context.Context, rt *app.Runtime, args []string) error {
	_, err := Execute(ctx, rt, args)
	return err
}

func newRun(ctx context.Context, rt *app.Runtime, args []string) *run {
	verb := rt.Opts.Get("VERB")
	switch verb {
	case "backup", "sync", "replicate", "rotate", "clone", "revert":
	default:
		verb = "backup"
	}
	r := &run{ctx: ctx, rt: rt, opts: rt.Opts, verb: verb, runVerb: verb, args: args,
		sendCheckDisabled: map[string]bool{}, marks: map[string]*bookmark{}, failed: map[string]bool{}, synced: map[string]bool{}, parallel: 1}
	r.sum.startTime = rt.Unix()
	return r
}

func (r *run) main() error {
	if r.opts.Bool("USAGE") {
		return app.Usage("")
	}
	if err := r.validateOptions(); err != nil {
		return err
	}
	r.loadEndpoints()
	r.resetState()
	if r.verb == "replicate" {
		r.depth = 1
	}
	r.recvPropsAdd = splitList(r.opts.Get("RECV_PROPS_ADD"))
	r.recvPropsDel = splitList(r.opts.Get("RECV_PROPS_DEL"))

	if err := r.validateDatasets(); err != nil {
		return err
	}
	if err := r.validateSnapshots(); err != nil {
		return err
	}
	r.computeEligibility()
	if err := r.configureOriginBackup(); err != nil {
		return err
	}
	r.runSendCheck()

	var err error
	switch {
	case r.verb == "clone" && len(r.args) == 4:
		err = r.runCloneShuffle()
	case r.verb == "clone":
		_, err = r.createRecursiveClone(src, r.src.DS, r.tgt.DS)
	case r.verb == "revert":
		err = r.runRevert()
	case r.verb == "rotate":
		err = r.runRotate()
	case r.filteredIntermediate():
		r.runFilteredIntermediate()
	default:
		r.runBackup()
	}
	if err != nil {
		return err
	}
	if err := r.runBookmarks(); err != nil {
		return err
	}
	r.sum.endTime = r.rt.Unix()
	r.sum.finished = true
	r.computeEligibility()
	r.printSummary()
	return nil
}

func (r *run) validateOptions() error {
	if d := r.opts.Get("DEPTH"); d != "" {
		if !units.IsNumeric(d) || units.Atof(d) < 0 {
			return app.Fail("depth of '%s' invalid; must be positive", d)
		}
		r.depth = units.Atoi(d)
	}
	switch r.opts.Get("BOOKMARK_MODE") {
	case "", "0", "1":
	default:
		return app.Fail("invalid bookmark mode: %s", r.opts.Get("BOOKMARK_MODE"))
	}
	r.parallel = 1
	if p := r.opts.Get("PARALLEL"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 {
			return app.Fail("invalid parallel value '%s'; must be a positive number", p)
		}
		r.parallel = n
	}
	return nil
}

func (r *run) bookmarkMode() bool { return r.opts.Get("BOOKMARK_MODE") == "1" }

func (r *run) loadEndpoints() {
	r.src, r.tgt = endpoint.Endpoint{}, endpoint.Endpoint{}
	if len(r.args) > 0 {
		r.src = r.rt.Endpoint(r.args[0])
	}
	if len(r.args) > 1 {
		r.tgt = r.rt.Endpoint(r.args[1])
	}
	if r.verb == "clone" && len(r.args) == 4 {
		r.cloneOrigin = r.rt.Endpoint(r.args[2])
		r.cloneBackup = r.rt.Endpoint(r.args[3])
	}
	if id := r.opts.Get("ORIGIN_ID"); id != "" {
		r.origin = r.rt.Endpoint(id)
	}
}

// resetState clears all per-tree planning state.
func (r *run) resetState() {
	r.ds = [2]map[string]*dataset{{}, {}}
	r.pairs = map[string]*pair{}
	r.actions = map[string]*action{}
	r.list = nil
	r.tree = treeState{finalSnapshot: r.src.Snap, sourcePool: r.src.Pool, targetPool: r.tgt.Pool}
	if r.snapMode() == "ALWAYS" {
		r.tree.snapshotNeeded = snapAlways
	}
}

func (r *run) snapMode() string { return strings.ToUpper(r.opts.Get("SNAP_MODE")) }

func (r *run) endpoint(s side) endpoint.Endpoint {
	if s == src {
		return r.src
	}
	return r.tgt
}

func (r *run) dataset(s side, suffix string) *dataset {
	d := r.ds[s][suffix]
	if d == nil {
		d = &dataset{props: map[string]string{}}
		r.ds[s][suffix] = d
	}
	return d
}

func (r *run) pair(suffix string) *pair {
	p := r.pairs[suffix]
	if p == nil {
		p = &pair{}
		r.pairs[suffix] = p
	}
	return p
}

func (r *run) action(suffix string) *action {
	a := r.actions[suffix]
	if a == nil {
		a = &action{}
		r.actions[suffix] = a
	}
	return a
}

func (r *run) dryRun() bool { return r.rt.DryRun() }

// fail records a fatal error message.
func fail(format string, a ...any) error { return app.Fail(format, a...) }

func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

// innerError logs the message carried by an error from an in-process
// command, which would otherwise only be logged by the outermost caller.
func innerError(rt *app.Runtime, err error) {
	var ee *app.ExitError
	var ue *app.UsageError
	switch {
	case errors.As(err, &ee) && ee.Msg != "":
		rt.Log.Errorf("%s", ee.Msg)
	case errors.As(err, &ue) && ue.Msg != "":
		rt.Log.Errorf("%s", ue.Msg)
	case err != nil && !errors.As(err, &ee) && !errors.As(err, &ue):
		rt.Log.Errorf("%s", err.Error())
	}
}
