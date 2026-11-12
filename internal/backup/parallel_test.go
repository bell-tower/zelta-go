package backup

import (
	"context"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

// pipeRecorder scripts every send/receive pipeline, recording when each
// target starts and finishes and how many pipelines overlap.
var seconds = regexp.MustCompile(`in [0-9.]+ seconds`)

type pipeRecorder struct {
	mu         sync.Mutex
	events     []string // "start <target>" and "end <target>"
	active     int
	maxActive  int
	delay      time.Duration
	fail       map[string]string // target -> failure line
	targetName *regexp.Regexp
}

func newPipeRecorder(delay time.Duration) *pipeRecorder {
	return &pipeRecorder{delay: delay, fail: map[string]string{}, targetName: regexp.MustCompile(`'([^']+)' ; }$`)}
}

func (p *pipeRecorder) install(f *fx) {
	f.tr.Fake.OnFunc(func(s string) bool { return strings.HasPrefix(s, "{ zfs send") }, func(s string) execx.Result {
		target := p.targetName.FindStringSubmatch(s)[1]
		p.mu.Lock()
		p.events = append(p.events, "start "+target)
		p.active++
		p.maxActive = max(p.maxActive, p.active)
		p.mu.Unlock()
		time.Sleep(p.delay)
		p.mu.Lock()
		p.active--
		p.events = append(p.events, "end "+target)
		msg, failed := p.fail[target]
		p.mu.Unlock()
		if failed {
			return execx.Fail(1, msg)
		}
		kind := "incremental"
		if !strings.Contains(s, " -I ") && !strings.Contains(s, " -i ") {
			kind = "full"
		}
		return received(kind, 1024, 1)
	})
}

func (p *pipeRecorder) index(event string) int { return slices.Index(p.events, event) }

func TestParallelFullBackupWaitsForParents(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"PARALLEL": "4"})
	f.props("apool/src", ". type filesystem", "/a type filesystem", "/a/b type filesystem", "/c type filesystem").
		missing("bpool/tgt").
		list("apool/src", "/c@s 4", "/a/b@s 3", "/a@s 2", "@s 1", "/c 40", "/a/b 30", "/a 20", ". 10")
	rec := newPipeRecorder(40 * time.Millisecond)
	rec.install(f)
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	for _, dep := range [][2]string{{"bpool/tgt", "bpool/tgt/a"}, {"bpool/tgt", "bpool/tgt/c"}, {"bpool/tgt/a", "bpool/tgt/a/b"}} {
		if rec.index("end "+dep[0]) > rec.index("start "+dep[1]) {
			t.Errorf("%s started before %s was received: %v", dep[1], dep[0], rec.events)
		}
	}
	if rec.maxActive < 2 {
		t.Errorf("siblings /a and /c should overlap; max concurrency %d (%v)", rec.maxActive, rec.events)
	}
	// The summary reports elapsed time, not the 2s of summed stream time.
	eqLines(t, "stdout", seconds.ReplaceAllString(f.tr.Out.String(), "in N seconds"), "syncing 4 datasets", "4K sent, 4 streams received in N seconds")
	if f.tr.Out.String() == "syncing 4 datasets\n4K sent, 4 streams received in 2 seconds\n" {
		t.Errorf("parallel summary reports summed stream time")
	}
}

func incrementalTree(t *testing.T, n int, opts map[string]string) *fx {
	f := newFx(t, "backup", opts)
	var srcProps, tgtProps, srcList, tgtList []string
	srcProps, tgtProps = append(srcProps, ". type filesystem"), append(tgtProps, ". type filesystem")
	srcList = append(srcList, "@b 2", "@a 1")
	tgtList = append(tgtList, "@a 1")
	for i := range n {
		d := "/d" + string(rune('0'+i))
		srcProps = append(srcProps, d+" type filesystem")
		tgtProps = append(tgtProps, d+" type filesystem")
		srcList = append(srcList, d+"@b "+itoa(100+i), d+"@a "+itoa(200+i), d+" "+itoa(300+i))
		tgtList = append(tgtList, d+"@a "+itoa(200+i), d+" "+itoa(400+i))
	}
	srcList = append(srcList, ". 9")
	tgtList = append(tgtList, ". 8")
	return f.props("apool/src", srcProps...).props("bpool/tgt", tgtProps...).list("apool/src", srcList...).list("bpool/tgt", tgtList...)
}

func TestParallelIncrementalsRespectLimit(t *testing.T) {
	f := incrementalTree(t, 6, map[string]string{"PARALLEL": "3"})
	rec := newPipeRecorder(50 * time.Millisecond)
	rec.install(f)
	start := time.Now()
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	if rec.maxActive < 2 || rec.maxActive > 3 {
		t.Errorf("max concurrency = %d, want 2..3", rec.maxActive)
	}
	if len(rec.events) != 14 {
		t.Errorf("events = %v", rec.events)
	}
	if elapsed := time.Since(start); elapsed > 7*50*time.Millisecond {
		t.Errorf("7 pipelines of 50ms took %v; not parallel", elapsed)
	}
	// Only per-dataset lines may be reordered; the notices match a serial run.
	serial := incrementalTree(t, 6, nil)
	newPipeRecorder(0).install(serial)
	if err := serial.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	if seconds.ReplaceAllString(f.tr.Out.String(), "in N seconds") != seconds.ReplaceAllString(serial.tr.Out.String(), "in N seconds") {
		t.Errorf("parallel stdout:\n%s\nserial stdout:\n%s", f.tr.Out.String(), serial.tr.Out.String())
	}
	if !strings.Contains(serial.tr.Out.String(), "7K sent, 7 streams received in 3.5 seconds") || strings.Contains(f.tr.Out.String(), "in 3.5 seconds") {
		t.Errorf("parallel runs report elapsed time: %q", f.tr.Out.String())
	}
}

func TestParallelFailureIsIsolated(t *testing.T) {
	f := incrementalTree(t, 3, map[string]string{"PARALLEL": "3"})
	rec := newPipeRecorder(10 * time.Millisecond)
	rec.fail["bpool/tgt/d1"] = "cannot receive incremental stream: destination bpool/tgt/d1 has been modified"
	rec.install(f)
	err := f.backup("apool/src", "bpool/tgt")
	if app.Code(err) != 2 {
		t.Fatalf("exit code = %d (%v)", app.Code(err), err)
	}
	if !strings.Contains(f.tr.Err.String(), "error: cannot receive incremental stream: destination bpool/tgt/d1 has been modified") {
		t.Errorf("stderr = %q", f.tr.Err.String())
	}
	if !strings.Contains(f.tr.Out.String(), "3K sent, 3 streams received") {
		t.Errorf("the other datasets should still sync: %q", f.tr.Out.String())
	}
}

func TestParallelFilteredIntermediate(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"PARALLEL": "2", "EXCLUDE": "@h*"})
	f.props("apool/src", ". type filesystem", "/x type filesystem").
		props("bpool/tgt", ". type filesystem", "/x type filesystem").
		list("apool/src", "/x@b 6", "/x@h1 5", "/x@a 4", "@b 3", "@h1 2", "@a 1", "/x 20", ". 10").
		list("bpool/tgt", "/x@a 4", "@a 1", "/x 21", ". 11")
	rec := newPipeRecorder(20 * time.Millisecond)
	rec.install(f)
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	pipes := f.tr.Fake.CallsMatching("zfs send")
	slices.Sort(pipes)
	want := []string{
		"{ zfs send -P -L -c -e -i 'apool/src/x@a' 'apool/src/x@b'|zfs recv -v -s 'bpool/tgt/x' ; }",
		"{ zfs send -P -L -c -e -i 'apool/src@a' 'apool/src@b'|zfs recv -v -s 'bpool/tgt' ; }",
	}
	eqSlice(t, "stepwise pipelines skip @h1", pipes, want...)
}

func TestParallelValidation(t *testing.T) {
	for _, v := range []string{"0", "-1", "x", "1.5"} {
		f := newFx(t, "backup", map[string]string{"PARALLEL": v})
		err := f.backup("apool/src", "bpool/tgt")
		if err == nil || err.Error() != "invalid parallel value '"+v+"'; must be a positive number" {
			t.Errorf("PARALLEL=%s: err = %v", v, err)
		}
	}
}

func TestForEachDatasetCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	tr := app.NewTest("backup", nil)
	r := newRun(ctx, tr.Runtime, nil)
	r.resetState()
	r.parallel = 2
	r.list = []string{"", "/a", "/b"}
	called := 0
	var mu sync.Mutex
	// The semaphore has free slots, so a canceled run may still start a
	// few datasets; what matters is that it returns promptly.
	done := make(chan struct{})
	go func() {
		r.forEachDataset(func(string) { mu.Lock(); called++; mu.Unlock() })
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("forEachDataset did not return after cancellation")
	}
	if called > 3 {
		t.Errorf("called = %d", called)
	}
}

func TestNearestAncestor(t *testing.T) {
	done := map[string]chan struct{}{"": make(chan struct{}), "/a": make(chan struct{})}
	for suffix, want := range map[string]chan struct{}{"": nil, "/a": done[""], "/a/b": done["/a"], "/a/b/c": done["/a"], "/z": done[""]} {
		if got := nearestAncestor(suffix, done); got != want {
			t.Errorf("nearestAncestor(%q) mismatch", suffix)
		}
	}
	if nearestAncestor("/q", map[string]chan struct{}{}) != nil {
		t.Errorf("no listed ancestor")
	}
	if got := removeLast([]string{"a", "b", "a"}, "a"); strings.Join(got, ",") != "a,b" {
		t.Errorf("removeLast = %v", got)
	}
	if got := removeLast([]string{"a"}, "z"); len(got) != 1 {
		t.Errorf("removeLast missing = %v", got)
	}
}
