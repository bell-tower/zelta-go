package backup

import (
	"context"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/cache"
	"github.com/bell-tower/zelta-go/internal/execx"
)

func pipe(from, to string) string {
	return "{ zfs send -P -L -c -e -I 'apool/src@" + from + "' 'apool/src@" + to + "'|zfs recv -v -s 'bpool/tgt' ; }"
}

// rememberFx is a run whose source has the given snapshots (newest first,
// "name guid") and whose target is only scripted when tgt is non-nil.
func rememberFx(t *testing.T, store *cache.Store, src []string, tgt []string) *fx {
	f := newFx(t, "backup", map[string]string{"REMEMBER": "1"})
	f.tr.Runtime.Cache = store
	f.props("apool/src", ". type filesystem")
	f.list("apool/src", append(src, ". 9")...)
	if tgt != nil {
		f.props("bpool/tgt", ". type filesystem")
		f.list("bpool/tgt", append(tgt, ". 8")...)
	}
	return f
}

func touchedTarget(f *fx) bool {
	for _, c := range f.tr.Fake.Calls {
		if (strings.Contains(c, "zfs get ") || strings.Contains(c, "zfs list ")) && strings.Contains(c, "bpool/tgt") {
			return true
		}
	}
	return false
}

func TestRemember(t *testing.T) {
	store := &cache.Store{Dir: t.TempDir()}

	// A normal run reads the target and remembers @b.
	f := rememberFx(t, store, []string{"@b 2", "@a 1"}, []string{"@a 1"})
	f.on(pipe("a", "b"), received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()

	// The next run sends from @b without reading the target.
	f = rememberFx(t, store, []string{"@c 3", "@b 2", "@a 1"}, nil)
	f.on(pipe("b", "c"), received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	if touchedTarget(f) {
		t.Errorf("remembered run read the target:\n%s", strings.Join(f.tr.Fake.Calls, "\n"))
	}
	eqLines(t, "stderr", f.tr.Err.String())

	// A refused receive starts over with a full comparison, silently.
	f = rememberFx(t, store, []string{"@d 4", "@c 3", "@b 2", "@a 1"}, []string{"@c 3", "@a 1"})
	f.tr.Fake.Once(pipe("c", "d"), execx.Fail(1, "cannot receive incremental stream: most recent snapshot of bpool/tgt does not match incremental source"))
	f.on(pipe("c", "d"), received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatalf("%v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "stale stderr", f.tr.Err.String())
	if !touchedTarget(f) || strings.Count(strings.Join(f.tr.Fake.Calls, "\n"), pipe("c", "d")) != 2 {
		t.Errorf("stale memory must be tried, then fall back to reading the target:\n%s", strings.Join(f.tr.Fake.Calls, "\n"))
	}
	eqLines(t, "stale stdout", f.tr.Out.String(), "syncing 1 datasets", "dataset up-to-date", "1K sent, 1 streams received in 0.5 seconds")
}

// A refused receive never turns a blocked pair into a send: the full
// comparison decides, and a diverged target is left alone.
func TestRememberDivergedTargetBlocks(t *testing.T) {
	store := &cache.Store{Dir: t.TempDir()}
	f := rememberFx(t, store, []string{"@b 2", "@a 1"}, []string{"@a 1"})
	f.on(pipe("a", "b"), received("incremental", 1024, 1))
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}

	f = rememberFx(t, store, []string{"@c 3", "@b 2", "@a 1"}, []string{"@x 99", "@b 2"})
	f.tr.Fake.Once(pipe("b", "c"), execx.Fail(1, "cannot receive incremental stream: most recent snapshot of bpool/tgt does not match incremental source"))
	f.backup("apool/src", "bpool/tgt")
	f.noUnmatched()
	sends := 0
	for _, c := range f.tr.Fake.Calls {
		if strings.Contains(c, "zfs send") {
			sends++
		}
	}
	if sends != 1 {
		t.Errorf("diverged target must not be sent to after the refusal; sends=%d\n%s", sends, strings.Join(f.tr.Fake.Calls, "\n"))
	}
	contains(t, "diverged", f.tr.Out.String()+f.tr.Err.String(), "diverged")

	// Without REMEMBER nothing is read from or written to the cache.
	f = newFx(t, "backup", nil)
	f.tr.Runtime.Cache = &cache.Store{Dir: t.TempDir()}
	f.props("apool/src", ". type filesystem").props("bpool/tgt", ". type filesystem").
		list("apool/src", "@b 2", "@a 1", ". 9").list("bpool/tgt", "@b 2", ". 8")
	if err := f.backup("apool/src", "bpool/tgt"); err != nil {
		t.Fatal(err)
	}
	var m memory
	if _, ok := f.tr.Runtime.Cache.Load(f.newRunKey(), &m); ok {
		t.Error("memory saved without REMEMBER")
	}
}

func (f *fx) newRunKey() string {
	r := newRun(context.Background(), f.tr.Runtime, []string{"apool/src", "bpool/tgt"})
	r.loadEndpoints()
	return r.memoryKey()
}

func TestRememberNeverWithForcedReceive(t *testing.T) {
	for _, opts := range []map[string]string{
		{"REMEMBER": "1", "RECV_OVERRIDE": "-F"},
		{"REMEMBER": "1", "RECV_TOP": "-uF -o readonly=on"},
	} {
		f := newFx(t, "backup", opts)
		f.tr.Runtime.Cache = &cache.Store{Dir: t.TempDir()}
		r := newRun(context.Background(), f.tr.Runtime, nil)
		if r.rememberEnabled() {
			t.Errorf("REMEMBER enabled with %v", opts)
		}
	}
}
