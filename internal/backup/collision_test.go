package backup

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

const (
	firstName  = "zelta_2026-01-02_03.04.05" // the default name at app.TestNow
	secondName = "zelta_2026-01-02_03.04.06"
)

// fakeClock makes rt.Now advance only when the snapshot retry sleeps.
func fakeClock(t *testing.T, tr *app.TestRuntime) *[]time.Duration {
	now := app.TestNow
	tr.Now = func() time.Time { return now }
	var slept []time.Duration
	old := sleep
	sleep = func(d time.Duration) { slept = append(slept, d); now = now.Add(d) }
	t.Cleanup(func() { sleep = old })
	return &slept
}

func collides(name string) execx.Result {
	return execx.Fail(1, "cannot create snapshot 'apool/src@"+name+"': dataset already exists", "no snapshots were created")
}

func TestDefaultSnapshotNameCollisionRetries(t *testing.T) {
	f := newFx(t, "backup", map[string]string{"SNAP_MODE": "ALWAYS"})
	slept := fakeClock(t, f.tr)
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "@"+secondName+" 2", "@a 1", ". 9").list("bpool/tgt", "@a 1", ". 8").
		on("zfs snapshot -r 'apool/src@"+firstName+"'", collides(firstName)).
		on("zfs snapshot -r 'apool/src@"+secondName+"'", execx.Out()).
		on("{ zfs send -P -L -c -e -I 'apool/src@a' 'apool/src@"+secondName+"'|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	if len(*slept) != 1 || (*slept)[0] != time.Second {
		t.Errorf("slept = %v, want one wait for the next second", *slept)
	}
	if !strings.Contains(f.tr.Out.String(), "snapshot 'apool/src@"+firstName+"' already exists; snapshotting: @"+secondName) {
		t.Errorf("stdout = %q", f.tr.Out.String())
	}
	if strings.Contains(f.tr.Err.String(), "error") {
		t.Errorf("stderr = %q", f.tr.Err.String())
	}
}

func TestChosenSnapshotNameNeverRetries(t *testing.T) {
	f := snapshotFailure(t, map[string]string{"SNAP_MODE": "ALWAYS", "SNAP_NAME": "new", "LOG_LEVEL": "0"})
	slept := fakeClock(t, f.tr)
	if err := f.backup("apool/source", "bpool/backups/source"); app.Code(err) != 1 {
		t.Fatalf("err = %v", err)
	}
	if len(*slept) != 0 || len(f.tr.Fake.CallsMatching("zfs snapshot")) != 1 {
		t.Errorf("a chosen name must not be retried: slept %v, calls %v", *slept, f.tr.Fake.CallsMatching("zfs snapshot"))
	}
}

func TestSnapshotRetryIsBounded(t *testing.T) {
	tr := app.NewTest("snapshot", nil)
	slept := fakeClock(t, tr)
	tr.Fake.OnRegexp(`^zfs snapshot -r`, collides("x"))
	err := Snapshot(context.Background(), tr.Runtime, []string{"apool/src"})
	if app.Code(err) != 1 || len(tr.Fake.Calls) != snapshotAttempts || len(*slept) != snapshotAttempts-1 {
		t.Errorf("err=%v calls=%v slept=%v", err, tr.Fake.Calls, *slept)
	}

	// A clock that does not advance (e.g. a fixed SYSTIME) cannot produce
	// a new name, so there is nothing to retry.
	tr = app.NewTest("snapshot", nil)
	old := sleep
	sleep = func(time.Duration) {}
	defer func() { sleep = old }()
	tr.Fake.OnRegexp(`^zfs snapshot -r`, collides("x"))
	if err := Snapshot(context.Background(), tr.Runtime, []string{"apool/src"}); app.Code(err) != 1 || len(tr.Fake.Calls) != 1 {
		t.Errorf("frozen clock: err=%v calls=%v", err, tr.Fake.Calls)
	}
}

func TestSnapshotVerbRetriesDefaultName(t *testing.T) {
	tr := app.NewTest("snapshot", map[string]string{"SNAP_PREFIX": "daily"})
	fakeClock(t, tr)
	tr.Fake.Once("zfs snapshot -r 'apool/src@daily_2026-01-02_03.04.05'", collides("daily_2026-01-02_03.04.05"))
	tr.Fake.On("zfs snapshot -r 'apool/src@daily_2026-01-02_03.04.06'", execx.Out())
	if err := Snapshot(context.Background(), tr.Runtime, []string{"apool/src"}); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(tr.Out.String(), "snapshot created 'apool/src@daily_2026-01-02_03.04.06'\n") || tr.RawErr.Len() != 0 {
		t.Errorf("stdout=%q raw stderr=%q", tr.Out.String(), tr.RawErr.String())
	}
	// An explicit endpoint snapshot is the user's choice.
	tr = app.NewTest("snapshot", nil)
	slept := fakeClock(t, tr)
	tr.Fake.On("zfs snapshot -r 'apool/src@mine'", collides("mine"))
	if err := Snapshot(context.Background(), tr.Runtime, []string{"apool/src@mine"}); app.Code(err) != 1 || len(*slept) != 0 {
		t.Errorf("explicit name retried: err=%v slept=%v", err, *slept)
	}
}
