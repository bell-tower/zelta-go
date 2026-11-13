package backup

import (
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/logging"
	"github.com/bell-tower/zelta-go/internal/match"
	"github.com/bell-tower/zelta-go/internal/units"
	"github.com/bell-tower/zelta-go/internal/zfs"
)

var (
	ignoreSendOutput   = regexp.MustCompile(`^(incremental|full)`)
	ignoreResumeOutput = regexp.MustCompile(`^nvlist version|^\t(fromguid|object|offset|bytes|toguid|toname|embedok|compressok)`)
	warnRecvProps      = regexp.MustCompile(`cannot receive .* property`)
	failSendRecv       = regexp.MustCompile(`^(cannot receive .* stream|cannot send|missing.*argument)`)
	failOption         = regexp.MustCompile(`^(invalid|illegal|unknown|unrecognized) option|^usage:`)
	progressNumber     = regexp.MustCompile(`^[0-9]+$`)
)

// syncOutcome accumulates what a send/receive pipeline reported.
type syncOutcome struct {
	size      string
	streams   int
	seconds   float64
	message   string
	errMsg    string
	syncError bool
}

func (r *run) syncErrorMessage(line, suffix string) string {
	sep := ": "
	if strings.HasSuffix(line, ":") {
		sep = " "
	}
	return line + sep + r.tgt.DS + suffix
}

func (r *run) syncFailed(o *syncOutcome, msg string) {
	if r.memory != nil {
		// The receive refused the remembered state; compare again.
		r.rt.Log.Infof("%s", msg)
		r.stale = true
		o.errMsg = msg
		o.syncError = true
		return
	}
	r.rt.Log.Errorf("%s", msg)
	o.errMsg = msg
	o.syncError = true
	r.sum.errorCode = 2
}

// parseSyncLine handles one line of send/receive output.
func (r *run) parseSyncLine(o *syncOutcome, suffix, dsSnap, line string) {
	f := strings.Fields(line)
	field := func(i int) string {
		if i < len(f) {
			return f[i]
		}
		return ""
	}
	switch {
	case field(0) == "size":
		o.size = field(1)
		r.rt.Log.Infof("syncing: %s for %s", units.HumanBytes(units.Atof(o.size)), dsSnap)
		r.sum.replicationSize += int64(units.Atof(o.size))
	case field(0) == "received":
		o.streams++
		t := units.Atof(field(4))
		o.seconds += t
		r.sum.replicationTime += t
		r.sum.streamsReceived++
	case field(0) == "receiving":
		o.message = field(1) + " " + field(2)
	case strings.Contains(line, "using provided clone origin"):
		r.sum.targetsCloned++
	case strings.Contains(line, "resume token contents"):
		r.dataset(tgt, suffix).props["receive_resume_token"] = ""
		r.sum.targetsResumed++
		r.rt.Log.Noticef("resuming transfer for: %s", dsSnap)
		r.rt.Log.Infof("to abort a failed resume, run: 'zfs receive -A %s'", r.tgt.DS+suffix)
	case failSendRecv.MatchString(line):
		r.syncFailed(o, r.syncErrorMessage(line, suffix))
	case r.sendCheckActive() && sendOptionError(line):
		r.syncFailed(o, r.syncErrorMessage(line, suffix))
	case failOption.MatchString(line):
		r.syncFailed(o, r.syncErrorMessage(line, suffix))
	case warnRecvProps.MatchString(line):
		r.rt.Log.Debugf("%s: %s", line, r.tgt.DS+suffix)
		r.addFailedProp(field(2))
	case strings.Contains(field(0), ":") && progressNumber.MatchString(field(1)):
		r.rt.Log.Infof("%s", line)
	case ignoreSendOutput.MatchString(line), ignoreResumeOutput.MatchString(line):
	case zfs.Feedback(r.rt.Log, line) == logging.Error:
		o.errMsg = line
		o.syncError = true
		r.sum.errorCode = 2
	}
}

func (r *run) addFailedProp(prop string) {
	if slices.Contains(r.sum.failedProps, prop) {
		return
	}
	r.sum.failedProps = append(r.sum.failedProps, prop)
}

// streamName describes a stream as "pool/ds@from%to" or "pool/ds@to".
func (r *run) streamName(suffix string) string {
	p := r.pair(suffix)
	if p.sourceStart != "" {
		return r.src.DS + suffix + p.sourceStart + "%" + strings.TrimLeft(p.sourceEnd, "@#")
	}
	return r.src.DS + suffix + p.sourceEnd
}

// runSync replicates one dataset with a single send/receive pipeline. It
// holds r.mu except while the pipeline runs, so datasets may sync
// concurrently.
func (r *run) runSync(suffix string) {
	r.mu.Lock()
	retry := r.syncLocked(suffix)
	r.mu.Unlock()
	if retry {
		r.runSync(suffix)
	}
}

// syncLocked is runSync with r.mu held on entry and exit. It reports
// whether the sync should be retried after dropping an unsupported send
// option.
func (r *run) syncLocked(suffix string) (retry bool) {
	a := r.action(suffix)
	if r.verb == "rotate" && !a.canRotate || r.verb != "rotate" && !a.canSync {
		return false
	}
	p := r.pair(suffix)
	dsSnap := r.src.DS + suffix + p.sourceEnd
	stream := r.streamName(suffix)
	r.sum.sentStreams = append(r.sum.sentStreams, stream)
	r.sum.streamsSent++

	pipe := r.syncPipeline(suffix)
	if r.dryRun() {
		r.rt.Log.Noticef("+ %s", pipe.String())
		r.trackBookmark(suffix)
		return false
	}
	o := &syncOutcome{message: "synced"}
	r.rt.Log.Debugf("`%s`", pipe.String())
	r.mu.Unlock()
	res := r.rt.Exec.Pipe(r.ctx, pipe, func(l execx.Line) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.parseSyncLine(o, suffix, dsSnap, app.SanitizeLine(l.Text))
	})
	r.mu.Lock()
	if o.streams == 0 && !o.syncError && !res.OK() {
		r.syncFailed(o, "sync command failed before receiving stream: "+dsSnap+" -> "+r.tgt.DS+suffix)
	}
	if o.streams > 0 {
		msg := units.HumanBytes(units.Atof(o.size)) + " " + o.message + " received in " + units.FormatNumber(o.seconds) + " seconds"
		if o.streams > 1 {
			msg += " (" + itoa(o.streams) + " streams)"
		}
		r.rt.Log.Infof("%s", msg)
		r.synced[suffix] = true
		r.updateLatest(tgt, suffix, p.sourceEnd)
		r.trackBookmark(suffix)
		return false
	}
	if o.syncError && r.dropSendFeature(suffix, o.errMsg) {
		// Retry without the feature; failures of other datasets stand.
		r.sum.errorCode = 0
		if len(r.failed) > 0 {
			r.sum.errorCode = 2
		}
		r.sum.streamsSent--
		r.sum.sentStreams = removeLast(r.sum.sentStreams, stream)
		return true
	}
	if !o.syncError {
		r.syncFailed(o, "sync failed before receiving stream: "+dsSnap+" -> "+r.tgt.DS+suffix)
	}
	a.statusNotice = "sync attempted with errors"
	r.tree.syncable--
	a.canSync = false
	r.failed[suffix] = true
	return false
}

// removeLast removes the last occurrence of s from list.
func removeLast(list []string, s string) []string {
	for i, l := range slices.Backward(list) {
		if l == s {
			return append(list[:i], list[i+1:]...)
		}
	}
	return list
}

// canSync reports, under the lock, whether a dataset still needs a sync.
func (r *run) canSync(suffix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.action(suffix).canSync
}

// stopped reports whether a dataset's stepwise sync should stop after an
// error. Serial runs keep Zelta's historical rule (any error so far);
// parallel runs only consider the dataset's own failure.
func (r *run) stopped(suffix string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.parallel > 1 {
		return r.failed[suffix]
	}
	return r.sum.errorCode != 0
}

// forEachDataset runs fn for every dataset to replicate. Serial runs keep
// the historical order. With PARALLEL above 1, up to that many datasets
// sync at once, and a dataset without a target waits for its nearest
// replicated ancestor so the parent exists before the child is received.
func (r *run) forEachDataset(fn func(suffix string)) {
	if r.parallel <= 1 || r.dryRun() {
		for _, suffix := range r.list {
			fn(suffix)
		}
		return
	}
	done := make(map[string]chan struct{}, len(r.list))
	for _, suffix := range r.list {
		done[suffix] = make(chan struct{})
	}
	sem := make(chan struct{}, r.parallel)
	var wg sync.WaitGroup
	start := time.Now()
	defer func() {
		r.mu.Lock()
		r.sum.parallelSeconds += math.Round(time.Since(start).Seconds()*100) / 100
		r.mu.Unlock()
	}()
	for _, suffix := range r.list {
		var parent chan struct{}
		if !r.dataset(tgt, suffix).exists {
			parent = nearestAncestor(suffix, done)
		}
		wg.Go(func() {
			defer close(done[suffix])
			if parent != nil {
				<-parent
			}
			select {
			case sem <- struct{}{}:
			case <-r.ctx.Done():
				return
			}
			defer func() { <-sem }()
			fn(suffix)
		})
	}
	wg.Wait()
}

// nearestAncestor returns the completion channel of the closest ancestor of
// suffix that is being replicated, or nil.
func nearestAncestor(suffix string, done map[string]chan struct{}) chan struct{} {
	for suffix != "" {
		i := strings.LastIndex(suffix, "/")
		if i < 0 {
			return nil
		}
		suffix = suffix[:i]
		if ch, ok := done[suffix]; ok {
			return ch
		}
	}
	return nil
}

// runBackup syncs every eligible dataset, with a second pass for
// intermediate syncs that first had to send a full stream.
func (r *run) runBackup() {
	r.announceSync()
	r.forEachDataset(func(suffix string) {
		if r.canSync(suffix) {
			r.runSync(suffix)
		}
		if r.dryRun() {
			return
		}
		if r.canSync(suffix) {
			r.runSync(suffix)
		}
	})
}

func (r *run) announceSync() {
	// A retry after stale memory was already announced.
	if r.tree.syncable == 0 || r.noMemory {
		return
	}
	verb := "syncing "
	if r.dryRun() {
		verb = "would sync "
	}
	r.rt.Log.Noticef("%s%d datasets", verb, len(r.list))
}

// filteredIntermediate reports whether intermediate snapshots must be sent
// one at a time because filters select only some of them.
func (r *run) filteredIntermediate() bool {
	return r.opts.Bool("SEND_INTR") && (r.opts.Has("EXCLUDE") || r.opts.Has("INCLUDE")) && r.verb != "replicate"
}

// runFilteredIntermediate replicates selected snapshots stepwise with
// "zfs send -i" so filtered snapshots are not pulled into a -I stream.
func (r *run) runFilteredIntermediate() {
	r.announceSync()
	var once sync.Once
	var tree *match.Tree
	sourceTree := func() *match.Tree {
		once.Do(func() { tree = r.listSourceTree() })
		return tree
	}
	r.forEachDataset(func(suffix string) {
		if !r.canSync(suffix) {
			return
		}
		r.mu.Lock()
		p := r.pair(suffix)
		full := p.match == ""
		r.mu.Unlock()
		if full {
			r.runSync(suffix)
			if r.stopped(suffix) {
				return
			}
			if r.dryRun() {
				p.match = p.sourceEnd
			}
		}
		snaps := sourceTree()
		r.mu.Lock()
		last := p.match
		a := r.action(suffix)
		a.forceIncrement = true
		steps := snaps.SendRange(suffix, p.match)
		r.mu.Unlock()
		for _, snap := range steps {
			r.mu.Lock()
			p.sourceStart, p.sourceEnd = last, snap
			r.mu.Unlock()
			r.runSync(suffix)
			if r.stopped(suffix) {
				break
			}
			last = snap
		}
		r.mu.Lock()
		a.forceIncrement = false
		r.mu.Unlock()
	})
}

// listSourceTree lists the source again with the run's filters, so the
// stepwise send range includes snapshots created during this run.
func (r *run) listSourceTree() *match.Tree {
	res, err := match.Compare(r.ctx, r.matchRuntime(), r.matchRequest(endpoint.Endpoint{}))
	if err != nil || res == nil {
		return r.srcTree
	}
	return res.Source
}

func itoa(n int) string { return strconv.Itoa(n) }

// bookmarks

func (r *run) trackBookmark(suffix string) {
	if !r.bookmarkMode() {
		return
	}
	snap := r.pair(suffix).sourceEnd
	if snap == "" {
		return
	}
	r.marks[suffix] = &bookmark{snap: snap, targetSnap: r.tgt.DS + suffix + snap, sourceSnap: r.src.DS + suffix + snap}
}

func (r *run) bookmarkName(suffix string) string {
	name := strings.TrimLeft(r.marks[suffix].snap, "@#")
	prefix := r.opts.Get("BOOKMARK_PREFIX")
	if prefix == "" {
		prefix = r.tgt.Host + "_"
	}
	return r.src.DS + suffix + "#" + prefix + name
}

func (r *run) confirmBookmarkTarget(suffix string) bool {
	snap := r.marks[suffix].targetSnap
	cmd := zfs.Command(r.rt, r.tgt, zfs.RemoteDefault, command.Word("list"), command.Word("-Ho"), command.Word("name"), command.Name(snap))
	if r.dryRun() {
		r.rt.Log.Noticef("+ %s", cmd.String())
		return true
	}
	res := r.rt.Run(r.ctx, cmd)
	confirmed, failed := false, false
	for _, l := range res.Lines {
		line := app.SanitizeLine(l.Text)
		if line == snap {
			confirmed = true
		} else {
			failed = true
			r.rt.Log.Errorf("cannot confirm replicated snapshot: %s: %s", snap, line)
		}
	}
	if !confirmed && !failed {
		r.rt.Log.Errorf("cannot confirm replicated snapshot: %s", snap)
	}
	return confirmed
}

func (r *run) createSourceBookmark(suffix string) bool {
	snap := r.marks[suffix].sourceSnap
	name := r.bookmarkName(suffix)
	cmd := zfs.Command(r.rt, r.src, zfs.RemoteDefault, command.Word("bookmark"), command.Name(snap), command.Name(name))
	r.rt.Log.Infof("bookmarking: %s", name)
	if r.dryRun() {
		r.rt.Log.Noticef("+ %s", cmd.String())
		return true
	}
	res := r.rt.Run(r.ctx, cmd)
	failed := false
	for _, l := range res.Lines {
		failed = true
		r.rt.Log.Errorf("cannot bookmark replicated snapshot: %s: %s", snap, app.SanitizeLine(l.Text))
	}
	if !failed && !res.OK() {
		failed = true
		r.rt.Log.Errorf("cannot bookmark replicated snapshot: %s", snap)
	}
	return !failed
}

// runBookmarks bookmarks each confirmed replicated source snapshot.
func (r *run) runBookmarks() error {
	if !r.bookmarkMode() || r.runVerb == "revert" || r.runVerb == "clone" || len(r.marks) == 0 {
		return nil
	}
	n := 0
	for _, suffix := range slices.Backward(r.list) {
		if r.marks[suffix] == nil {
			continue
		}
		if r.confirmBookmarkTarget(suffix) && r.createSourceBookmark(suffix) {
			n++
		} else if r.sum.errorCode == 0 {
			r.sum.errorCode = 1
		}
	}
	if !r.dryRun() {
		noun := "snapshots"
		if n == 1 {
			noun = "snapshot"
		}
		r.rt.Log.Noticef("%d %s bookmarked", n, noun)
	}
	return nil
}
