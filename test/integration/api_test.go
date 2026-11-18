//go:build integration

package integration

import (
	"context"
	"errors"
	"log/slog"
	"reflect"
	"strings"
	"sync"
	"testing"

	zg "zelta.space/zelta"
)

// logSink collects slog messages.
type logSink struct {
	mu   sync.Mutex
	msgs []string
}

func (s *logSink) Enabled(context.Context, slog.Level) bool { return true }
func (s *logSink) WithAttrs([]slog.Attr) slog.Handler       { return s }
func (s *logSink) WithGroup(string) slog.Handler            { return s }
func (s *logSink) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.msgs = append(s.msgs, r.Level.String()+" "+r.Message)
	return nil
}

func (s *logSink) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return strings.Join(s.msgs, "\n")
}

// TestPublicAPI drives the Go package against real pools: snapshot,
// backup, match, guarded prune, and a remote comparison over the ssh
// stand-in.
func TestPublicAPI(t *testing.T) {
	ctx := context.Background()
	src, tgt := tree(t, "api")
	makeTree(t, src)
	sink := &logSink{}
	c, err := zg.New(zg.WithLogger(slog.New(sink)))
	if err != nil {
		t.Fatal(err)
	}

	name, err := c.Snapshot(ctx, src, &zg.SnapshotOptions{Name: "one"})
	if err != nil || name != src+"@one" {
		t.Fatalf("Snapshot = %q, %v\n%s", name, err, sink)
	}

	res, err := c.Backup(ctx, src, tgt, &zg.BackupOptions{Parallel: 4})
	if err != nil {
		t.Fatalf("Backup: %v\n%s", err, sink)
	}
	if len(res.Datasets) != 9 || !res.UpToDate() || res.StreamsReceived != 9 || res.Snapshot != "" || res.Bytes == 0 {
		t.Errorf("first backup = %+v", res)
	}
	for _, d := range res.Datasets {
		if !d.Synced || d.Target != tgt+d.Suffix {
			t.Errorf("dataset %+v", d)
		}
	}

	cmp, err := c.Match(ctx, src, tgt, nil)
	if err != nil || cmp.UpToDate != 9 || cmp.Datasets[0].Match != "@one" || cmp.Datasets[0].Source.Name != src {
		t.Fatalf("Match = %+v, %v", cmp, err)
	}

	for _, snap := range []string{"two", "three"} {
		res, err = c.Backup(ctx, src, tgt, &zg.BackupOptions{Snapshot: zg.SnapshotPolicy{Mode: zg.SnapshotAlways, Name: snap}})
		if err != nil || res.Snapshot != "@"+snap || res.StreamsReceived != 9 || !res.UpToDate() {
			t.Fatalf("backup %s = %+v, %v\n%s", snap, res, err, sink)
		}
	}
	res, err = c.Backup(ctx, src, tgt, &zg.BackupOptions{Snapshot: zg.SnapshotPolicy{Mode: zg.SnapshotNever}})
	if err != nil || res.StreamsReceived != 0 || !res.UpToDate() || res.Datasets[0].Synced {
		t.Errorf("no-op backup = %+v, %v", res, err)
	}

	opts := &zg.PruneOptions{Guard: tgt, Keep: 1}
	plan, err := c.PlanPrune(ctx, src, opts)
	if err != nil {
		t.Fatalf("PlanPrune: %v\n%s", err, sink)
	}
	if d := plan.Datasets[0]; d.Name != src || !reflect.DeepEqual(d.Destroy, []string{"@one"}) || !reflect.DeepEqual(d.Keep, []string{"@two"}) {
		t.Errorf("plan = %+v", plan.Datasets[0])
	}
	if _, err := c.Prune(ctx, src, &zg.PruneOptions{Keep: 1}); err == nil {
		t.Error("unguarded prune ran")
	}
	plan, err = c.Prune(ctx, src, opts)
	if err != nil || len(plan.Destroyed) != 9 {
		t.Fatalf("Prune = %+v, %v\n%s", plan, err, sink)
	}
	if got := snapshots(t, src+"/sub1"); !reflect.DeepEqual(got, []string{src + "/sub1@two", src + "/sub1@three"}) {
		t.Errorf("after prune: %q", got)
	}

	// The same comparison through the ssh stand-in.
	r, err := zg.New(zg.WithRemoteCommand(fakeSSH))
	if err != nil {
		t.Fatal(err)
	}
	cmp, err = r.Match(ctx, "remote:"+src, tgt, &zg.MatchOptions{NoSizes: true})
	if err != nil || cmp.UpToDate != 9 || cmp.Datasets[0].Source.First != "@two" {
		t.Errorf("remote Match = %+v, %v", cmp, err)
	}

	_, err = c.Backup(ctx, src, tgtPool+"/missing/parent/x", &zg.BackupOptions{Settings: map[string]string{"CREATE_PARENT": "0"}})
	var e *zg.Error
	if !errors.As(err, &e) || e.Code != 1 {
		t.Errorf("missing parent: %v", err)
	}
	if strings.Contains(sink.String(), "ERROR ") && !strings.Contains(sink.String(), "parent") {
		t.Errorf("unexpected errors:\n%s", sink)
	}
}
