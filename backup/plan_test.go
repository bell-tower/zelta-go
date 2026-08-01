package backup

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"git.belltower.it/djbell/zelta-go/match"
	"git.belltower.it/djbell/zelta-go/internal/opt"
	"git.belltower.it/djbell/zelta-go/zfs"
)

func defFlags() opt.SendRecv { return opt.Default() }

func TestPlanFullAndIncr(t *testing.T) {
	views := []PairView{
		{
			DSSuffix: "",
			Info:     "syncable (incremental)",
			Match:    "@a",
			SrcLast:  "@b",
			TgtLast:  "@a",
			SrcName:  "tank/src",
			TgtName:  "tank/tgt",
		},
		{
			DSSuffix: "/child",
			Info:     "syncable (full)",
			SrcLast:  "@a",
			SrcName:  "tank/src/child",
			TgtName:  "tank/tgt/child",
		},
		{
			DSSuffix: "/skip",
			Info:     "up-to-date",
			Match:    "@a",
			SrcLast:  "@a",
			SrcName:  "tank/src/skip",
			TgtName:  "tank/tgt/skip",
		},
		{
			DSSuffix: "/blocked",
			Info:     "blocked sync: target diverged",
			SrcName:  "tank/src/blocked",
			TgtName:  "tank/tgt/blocked",
		},
	}
	p, err := PlanFromMatch(views, true, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	if p.Full != 1 || p.Incr != 1 || p.Skip != 1 || p.Block != 1 {
		t.Fatalf("counts full=%d incr=%d skip=%d block=%d", p.Full, p.Incr, p.Skip, p.Block)
	}
	out, err := FormatDryRun(p, "tank/src", "tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "would sync 2 datasets") {
		t.Fatalf("banner:\n%s", out)
	}
	if !strings.Contains(out, "-I") || !strings.Contains(out, "tank/src@a") || !strings.Contains(out, "tank/src@b") {
		t.Fatalf("incr line missing:\n%s", out)
	}
	if !strings.Contains(out, "tank/src/child@a") || !strings.Contains(out, "tank/tgt/child") {
		t.Fatalf("full line missing:\n%s", out)
	}
	if !strings.Contains(out, "-L") || !strings.Contains(out, "-c") || !strings.Contains(out, "-e") {
		t.Fatalf("send flags missing:\n%s", out)
	}
	// Incr: -s only. Full child: FS flags, no readonly.
	if !strings.Contains(out, "recv -v -s tank/tgt ") && !strings.Contains(out, "recv -v -s tank/tgt|") &&
		!strings.Contains(out, "recv -v -s tank/tgt\n") {
		// soft-join may put space before |
		if !strings.Contains(out, "recv -v -s tank/tgt") {
			t.Fatalf("incr recv flags:\n%s", out)
		}
	}
	if !strings.Contains(out, "-u") || !strings.Contains(out, "canmount=noauto") {
		t.Fatalf("full child RECV_FS missing:\n%s", out)
	}
	if strings.Count(out, "readonly=on") != 0 {
		// root is incr; child full should not have TOP readonly
		t.Fatalf("unexpected readonly on child/incr:\n%s", out)
	}
}

func TestPlanEncryptionSendFallback(t *testing.T) {
	cases := []struct {
		name     string
		src      string
		tgt      string
		ivset    string
		wantE    bool
		wantWarn bool
	}{
		{name: "both unencrypted", src: "off", tgt: "off", wantE: true},
		{name: "encrypted target parent", src: "off", tgt: "aes-256-gcm", wantE: false},
		{name: "plaintext target", src: "aes-256-gcm", tgt: "off", wantE: false, wantWarn: true},
		{name: "different encrypted keys", src: "aes-256-gcm", tgt: "aes-256-gcm", wantE: false, wantWarn: true},
		{name: "same encrypted key", src: "aes-256-gcm", tgt: "aes-256-gcm", ivset: "iv-1", wantE: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := PlanFromMatch([]PairView{{
				Info: "syncable (incremental)", Match: "@base", SrcLast: "@new",
				SrcName: "tank/src", TgtName: "tank/tgt",
				SrcEncryption: tc.src, TgtEncryption: tc.tgt, MatchIVSet: tc.ivset,
			}}, true, defFlags())
			if err != nil {
				t.Fatal(err)
			}
			send := strings.Join(p.Steps[0].Send, " ")
			if got := strings.Contains(send, "-e"); got != tc.wantE {
				t.Fatalf("send=%q contains -e=%v want %v", send, got, tc.wantE)
			}
			if got := len(p.Warnings) > 0; got != tc.wantWarn {
				t.Fatalf("warnings=%v want warning=%v", p.Warnings, tc.wantWarn)
			}
		})
	}
}

func TestRemoveSendFeatureFromShortFlags(t *testing.T) {
	if got := removeSendFeature("-L -c -e", "-e"); got != "-L -c" {
		t.Fatalf("got %q", got)
	}
	if got := removeSendFeature("-Lce", "-e"); got != "-Lc" {
		t.Fatalf("bundled flags got %q", got)
	}
}

func TestRunEncryptionFallbackDropsEmbeddedDataFlag(t *testing.T) {
	fake := &zfs.Fake{Lists: map[string]string{
		"tank/src": "tank/src\tg1\t0\t1\t1M\tfilesystem\taes-256-gcm\t-\t-\n" +
			"tank/src@new\tg3\t0\t3\t1K\tsnapshot\taes-256-gcm\tiv-new\t-\n" +
			"tank/src@base\tg2\t0\t2\t1K\tsnapshot\taes-256-gcm\tiv-source\t-\n",
		"tank/tgt": "tank/tgt\tg1\t0\t1\t1M\tfilesystem\taes-256-gcm\t-\t-\n" +
			"tank/tgt@base\tg2\t0\t2\t1K\tsnapshot\taes-256-gcm\tiv-target\t-\n",
	}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt", SnapMode: SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Pipes) != 1 {
		t.Fatalf("pipes=%d", len(fake.Pipes))
	}
	send := strings.Join(fake.Pipes[0].Left, " ")
	if strings.Contains(send, "-e") {
		t.Fatalf("send retained -e: %q", send)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "falling back to decrypted send") {
		t.Fatalf("warnings=%v plan=%+v", res.Warnings, res.Plan.Steps[0])
	}
}

func TestPlanResumeTokenUsesTokenSendOnly(t *testing.T) {
	p, err := PlanFromMatch([]PairView{{
		DSSuffix: "", Info: "syncable (resume)", SrcName: "tank/src", TgtName: "tank/tgt",
		TgtReceiveResumeToken: "token-123",
	}}, true, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 1 || p.Steps[0].Kind != KindIncremental {
		t.Fatalf("steps=%+v", p.Steps)
	}
	if got := strings.Join(p.Steps[0].Send, " "); got != "zfs send -t token-123" {
		t.Fatalf("send=%q", got)
	}
	if strings.Contains(strings.Join(p.Steps[0].Send, " "), "-i") || strings.Contains(strings.Join(p.Steps[0].Send, " "), "-I") {
		t.Fatalf("resume send used incremental flags: %v", p.Steps[0].Send)
	}
	if got := strings.Join(p.Steps[0].Recv, " "); !strings.Contains(got, "-s tank/tgt") {
		t.Fatalf("recv=%q", got)
	}
}

func TestRunResumesTargetToken(t *testing.T) {
	fake := &zfs.Fake{Lists: map[string]string{
		"tank/src": "tank/src\tg1\t0\t1\t1M\tfilesystem\t-\ntank/src@a\tg2\t0\t2\t1K\tsnapshot\t-\n",
		"tank/tgt": "tank/tgt\tg1\t0\t1\t1M\tfilesystem\ttoken-123\n",
	}}
	_, err := Run(context.Background(), fake, Request{Source: "tank/src", Target: "tank/tgt", SnapMode: SnapNever})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Pipes) != 1 || strings.Join(fake.Pipes[0].Left, " ") != "zfs send -t token-123" {
		t.Fatalf("pipes=%v", fake.Pipes)
	}
}

func TestRunResumeFailureDoesNotRetryNormalSend(t *testing.T) {
	fake := &zfs.Fake{
		Lists: map[string]string{
			"tank/src": "tank/src\tg1\t0\t1\t1M\tfilesystem\t-\ntank/src@a\tg2\t0\t2\t1K\tsnapshot\t-\n",
			"tank/tgt": "tank/tgt\tg1\t0\t1\t1M\tfilesystem\ttoken-123\n",
		},
		PipeErrors: map[string]error{"token-123": fmt.Errorf("interrupted receive")},
	}
	_, err := Run(context.Background(), fake, Request{Source: "tank/src", Target: "tank/tgt", SnapMode: SnapNever})
	if err == nil {
		t.Fatal("expected resume failure")
	}
	if len(fake.Pipes) != 1 {
		t.Fatalf("resume failure retried: %v", fake.Pipes)
	}
}

func TestRecvFlagsTopFull(t *testing.T) {
	p, err := PlanFromMatch([]PairView{{
		DSSuffix: "",
		Info:     "syncable (full)",
		SrcLast:  "@a",
		SrcName:  "tank/src",
		TgtName:  "tank/tgt",
	}}, false, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	out, err := FormatDryRun(p, "tank/src", "tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"readonly=on", "-u", "canmount=noauto", "-s"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
}

func TestIntermediateFullFirstPass(t *testing.T) {
	p, err := PlanFromMatch([]PairView{{
		DSSuffix: "",
		Info:     "syncable (full)",
		SrcNext:  "@snap1",
		SrcLast:  "@latest",
		SrcName:  "tank/src",
		TgtName:  "tank/tgt",
	}}, true, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	st := p.Steps[0]
	if st.SourceEnd != "@snap1" || st.FinalEnd != "@latest" {
		t.Fatalf("end=%q final=%q", st.SourceEnd, st.FinalEnd)
	}
	out, err := FormatDryRun(p, "tank/src", "tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tank/src@snap1") {
		t.Fatalf("dry-run should show earliest:\n%s", out)
	}
	if strings.Contains(out, "tank/src@latest") {
		t.Fatalf("dry-run must not show second pass:\n%s", out)
	}
}

func TestIntermediateFullExecuteTwoPass(t *testing.T) {
	// Missing target → full; multi-snap src → two pipes when intermediate.
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\n" +
		"tank/src@latest\t103\t0\t4\t1K\tsnapshot\n" +
		"tank/src@mid\t102\t0\t3\t1K\tsnapshot\n" +
		"tank/src@snap1\t101\t0\t2\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": ""}}
	res, err := Run(context.Background(), fake, Request{
		Source:       "tank/src",
		Target:       "tank/tgt",
		DryRun:       false,
		Intermediate: true,
		SnapMode:     SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Pipes) != 2 {
		t.Fatalf("want 2 pipes (full+incr), got %d; plan=%+v out=%q", len(fake.Pipes), res.Plan.Steps, res.Output)
	}
	// First: full @snap1; second: -I @snap1 @latest
	joined := strings.Join(fake.Pipes[0].Left, " ") + " || " + strings.Join(fake.Pipes[1].Left, " ")
	if !strings.Contains(joined, "@snap1") || !strings.Contains(joined, "@latest") {
		t.Fatalf("pipes: %v", fake.Pipes)
	}
}

func TestPlanIncrementalFlag(t *testing.T) {
	views := []PairView{{
		Info:    "syncable (incremental)",
		Match:   "@a",
		SrcLast: "@b",
		SrcName: "tank/src",
		TgtName: "tank/tgt",
	}}
	p, err := PlanFromMatch(views, false, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	out, err := FormatDryRun(p, "tank/src", "tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-i") || strings.Contains(out, "-I") {
		t.Fatalf("want -i only:\n%s", out)
	}
}

func TestPlanTargetOriginUsesOriginSendBase(t *testing.T) {
	p, err := PlanFromMatch([]PairView{{
		Info:         "syncable (full)",
		SrcLast:      "@clone-snap",
		SrcName:      "tank/clone",
		TgtName:      "backup/clone",
		SrcOrigin:    "tank/original@base",
		TargetOrigin: "backup/original@base",
	}}, false, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 1 || p.Steps[0].Kind != KindIncremental {
		t.Fatalf("steps=%+v", p.Steps)
	}
	out, err := FormatDryRun(p, "tank/clone", "backup/clone")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "-i tank/original@base tank/clone@clone-snap") {
		t.Fatalf("origin send base missing:\n%s", out)
	}
	if !strings.Contains(out, "-o origin=backup/original@base") {
		t.Fatalf("origin receive property missing:\n%s", out)
	}
}

func TestRunTargetOriginRequiresBackedUpOrigin(t *testing.T) {
	fake := &zfs.Fake{Lists: map[string]string{
		"tank/clone":      "tank/clone\t1\t0\t100\t1K\tfilesystem\ttank/original@base\ntank/clone@clone-snap\t2\t0\t200\t1K\tsnapshot\t-",
		"backup/clone":    "",
		"backup/original": "backup/original\t1\t0\t100\t1K\tfilesystem\t-\nbackup/original@base\t2\t0\t200\t1K\tsnapshot\t-",
	}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/clone", Target: "backup/clone", TargetOrigin: "backup/original",
		SnapMode: SnapNever, DryRun: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "-i tank/original@base tank/clone@clone-snap") {
		t.Fatalf("origin send base missing:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "-o origin=backup/original@base") {
		t.Fatalf("origin receive property missing:\n%s", res.Output)
	}
}

func TestApplySnapUpToDate(t *testing.T) {
	views := []PairView{{
		Info:       "up-to-date",
		Match:      "@a",
		SrcLast:    "@a",
		SrcName:    "tank/src",
		TgtName:    "tank/tgt",
		SrcWritten: "4096",
	}}
	p, err := PlanFromMatch(views, true, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	if p.Skip != 1 {
		t.Fatal("expected skip before snap")
	}
	if err := p.ApplySourceSnap("@zelta_test", true); err != nil {
		t.Fatal(err)
	}
	if p.Incr != 1 {
		t.Fatalf("expected incr after snap, got incr=%d", p.Incr)
	}
	out, err := FormatDryRun(p, "tank/src", "tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "tank/src@a") || !strings.Contains(out, "tank/src@zelta_test") {
		t.Fatalf("range:\n%s", out)
	}
}

func TestShouldSnapshot(t *testing.T) {
	if ShouldSnapshot(SnapNever, []PairView{{SrcWritten: "1", SrcName: "t", SrcLast: "@a"}}) != "" {
		t.Fatal("never")
	}
	if ShouldSnapshot(SnapAlways, nil) == "" {
		t.Fatal("always")
	}
	r := ShouldSnapshot(SnapIfNeeded, []PairView{{SrcName: "t", SrcLast: "@a", SrcWritten: "100"}})
	if !strings.Contains(r, "written") {
		t.Fatalf("written: %q", r)
	}
	r = ShouldSnapshot(SnapIfNeeded, []PairView{{SrcName: "t", SrcLast: ""}})
	if !strings.Contains(r, "missing") {
		t.Fatalf("missing: %q", r)
	}
}

func TestShouldSnapshotThresholds(t *testing.T) {
	recent := time.Now().Add(-time.Minute).Unix()
	view := PairView{SrcName: "tank/src", SrcLast: "@a", SrcWritten: "100", SrcSnapshotsChanged: fmt.Sprint(recent)}
	if got := ShouldSnapshotWithThresholds(SnapIfNeeded, []PairView{view}, time.Hour, 200); got != "" {
		t.Fatalf("recent small change should skip: %q", got)
	}
	if got := ShouldSnapshotWithThresholds(SnapIfNeeded, []PairView{view}, time.Second, 200); got == "" {
		t.Fatal("stale time threshold should snapshot")
	}
	if got := ShouldSnapshotWithThresholds(SnapIfNeeded, []PairView{view}, time.Hour, 100); got == "" {
		t.Fatal("reached size threshold should snapshot")
	}
}

func TestRunDryExecute(t *testing.T) {
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\ntank/src@a\t101\t0\t2\t1K\tsnapshot\n"
	tgt := "tank/tgt\t100\t0\t1\t1M\tfilesystem\ntank/tgt@a\t101\t0\t2\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": tgt}}

	// up-to-date, no written → no snap, nothing to send
	res, err := Run(context.Background(), fake, Request{
		Source:       "tank/src",
		Target:       "tank/tgt",
		DryRun:       true,
		Intermediate: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "zfs send") {
		t.Fatalf("unexpected send:\n%s", res.Output)
	}

	// written → snap + incr dry-run
	srcW := "tank/src\t100\t4096\t1\t1M\tfilesystem\ntank/src@a\t101\t0\t2\t1K\tsnapshot\n"
	fake2 := &zfs.Fake{Lists: map[string]string{"tank/src": srcW, "tank/tgt": tgt}}
	res, err = Run(context.Background(), fake2, Request{
		Source:       "tank/src",
		Target:       "tank/tgt",
		DryRun:       true,
		Intermediate: true,
		SnapName:     "testsnap",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "would snapshot: testsnap") {
		t.Fatalf("snap notice:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "zfs snapshot") || !strings.Contains(res.Output, "tank/src@testsnap") {
		t.Fatalf("snap cmd:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "zfs send") || !strings.Contains(res.Output, "@testsnap") {
		t.Fatalf("send after snap:\n%s", res.Output)
	}

	// execute path records snapshot + pipe
	fake3 := &zfs.Fake{Lists: map[string]string{"tank/src": srcW, "tank/tgt": tgt}}
	res, err = Run(context.Background(), fake3, Request{
		Source:       "tank/src",
		Target:       "tank/tgt",
		DryRun:       false,
		Intermediate: true,
		SnapName:     "execsnap",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake3.Snapshots) != 1 || fake3.Snapshots[0] != "tank/src@execsnap" {
		t.Fatalf("snaps=%v", fake3.Snapshots)
	}
	if len(fake3.Pipes) != 1 {
		t.Fatalf("pipes=%d", len(fake3.Pipes))
	}
}

func TestParentDataset(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"pool/a/b", "pool/a"},
		{"pool/a", "pool"},
		{"pool", ""},
		{"", ""},
	} {
		if got := parentDataset(tc.in); got != tc.want {
			t.Fatalf("parentDataset(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}

func TestParentCreateAttempts(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"pool/a/b", 2},
		{"pool/a", 1},
		{"pool", 0},
		{"", 0},
	} {
		if got := parentCreateAttempts(tc.in); got != tc.want {
			t.Fatalf("parentCreateAttempts(%q)=%d want %d", tc.in, got, tc.want)
		}
	}
}

// roCreateFake fails with ErrReadOnlyCreate for the first N Creates, then succeeds.
type roCreateFake struct {
	zfs.Fake
	failLeft int
	calls    int
}

func (f *roCreateFake) Create(ctx context.Context, ep, ds string) error {
	f.calls++
	if f.failLeft > 0 {
		f.failLeft--
		return zfs.ErrReadOnlyCreate
	}
	return f.Fake.Create(ctx, ep, ds)
}

func TestCreateParentReadonlyRetry(t *testing.T) {
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\ntank/src@a\t101\t0\t2\t1K\tsnapshot\n"
	// parent tank/a/b → 2 attempts; fail once then OK
	fake := &roCreateFake{
		Fake:     zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/a/b/leaf": ""}},
		failLeft: 1,
	}
	_, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/a/b/leaf",
		DryRun: true, Intermediate: true, SnapMode: SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if fake.calls != 2 {
		t.Fatalf("create calls=%d want 2", fake.calls)
	}
	if len(fake.Creates) != 1 || fake.Creates[0] != "tank/a/b" {
		t.Fatalf("creates=%v", fake.Creates)
	}

	// exhaust retries
	fake2 := &roCreateFake{
		Fake:     zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/a/b/leaf": ""}},
		failLeft: 99,
	}
	_, err = Run(context.Background(), fake2, Request{
		Source: "tank/src", Target: "tank/a/b/leaf",
		DryRun: true, Intermediate: true, SnapMode: SnapNever,
	})
	if err == nil || !strings.Contains(err.Error(), "incomplete zfs create") {
		t.Fatalf("err=%v", err)
	}
}

func TestRunCreateParent(t *testing.T) {
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\ntank/src@a\t101\t0\t2\t1K\tsnapshot\n"
	// missing target → empty list
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/new/leaf": ""}}
	res, err := Run(context.Background(), fake, Request{
		Source:       "tank/src",
		Target:       "tank/new/leaf",
		DryRun:       true,
		Intermediate: true,
		SnapMode:     SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Creates) != 1 || fake.Creates[0] != "tank/new" {
		t.Fatalf("creates=%v want [tank/new]", fake.Creates)
	}
	if !strings.Contains(res.Output, "would sync") {
		t.Fatalf("output:\n%s", res.Output)
	}

	// CREATE_PARENT off + missing parent → error
	off := false
	fake2 := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/missing/x": ""}}
	_, err = Run(context.Background(), fake2, Request{
		Source:       "tank/src",
		Target:       "tank/missing/x",
		DryRun:       true,
		Intermediate: true,
		SnapMode:     SnapNever,
		CreateParent: &off,
	})
	if err == nil || !strings.Contains(err.Error(), "target has no parent dataset") {
		t.Fatalf("err=%v", err)
	}
	if len(fake2.Creates) != 0 {
		t.Fatalf("unexpected creates=%v", fake2.Creates)
	}

	// target exists → no create
	tgt := "tank/tgt\t100\t0\t1\t1M\tfilesystem\ntank/tgt@a\t101\t0\t2\t1K\tsnapshot\n"
	fake3 := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": tgt}}
	_, err = Run(context.Background(), fake3, Request{
		Source:       "tank/src",
		Target:       "tank/tgt",
		DryRun:       true,
		Intermediate: true,
		SnapMode:     SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake3.Creates) != 0 {
		t.Fatalf("creates=%v", fake3.Creates)
	}
}

func TestRunInheritsTargetParentEncryption(t *testing.T) {
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\toff\t-\t-\ntank/src@a\t101\t0\t2\t1K\tsnapshot\toff\t-\t-\n"
	parent := "tank/new\t200\t0\t1\t1M\tfilesystem\taes-256-gcm\t-\t-\n"
	fake := &zfs.Fake{Lists: map[string]string{
		"tank/src": src,
		"root@debian:tank/new/leaf/child:tank/new/leaf/child": "",
		"tank/new": parent,
	}}
	res, err := Run(context.Background(), fake, Request{
		Source:       "tank/src",
		Target:       "root@debian:tank/new/leaf/child",
		DryRun:       true,
		Intermediate: true,
		SnapMode:     SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "-e tank/src@a") {
		t.Fatalf("embedded-data flag retained for encrypted target parent:\n%s", res.Output)
	}
}

func TestVolumeRecvFlags(t *testing.T) {
	p, err := PlanFromMatch([]PairView{{
		DSSuffix: "/vol1",
		Info:     "syncable (full)",
		SrcLast:  "@a",
		SrcName:  "tank/src/vol1",
		TgtName:  "tank/tgt/vol1",
		SrcType:  "volume",
	}}, false, defFlags())
	if err != nil {
		t.Fatal(err)
	}
	out, err := FormatDryRun(p, "tank/src", "tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	// RECV_VOL default empty → no FS mountpoint flags
	if strings.Contains(out, "mountpoint") || strings.Contains(out, "canmount") {
		t.Fatalf("volume should not get RECV_FS:\n%s", out)
	}
	if !strings.Contains(out, "recv -v -s tank/tgt/vol1") {
		t.Fatalf("want partial-only recv:\n%s", out)
	}
}

func TestFilterWarnings(t *testing.T) {
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\ntank/src@a\t101\t0\t2\t1K\tsnapshot\n"
	tgt := "tank/tgt\t100\t0\t1\t1M\tfilesystem\ntank/tgt@a\t101\t0\t2\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": tgt}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt",
		DryRun: true, Intermediate: true, SnapMode: SnapNever,
		Exclude: []string{"foo*bar"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) == 0 {
		t.Fatal("want filter warning")
	}
	if !strings.Contains(res.Warnings[0], "invalid filter pattern 'foo*bar'") {
		t.Fatalf("warning=%q", res.Warnings[0])
	}
}

func TestVolumeTypeFromList(t *testing.T) {
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\n" +
		"tank/src@a\t101\t0\t2\t1K\tsnapshot\n" +
		"tank/src/vol1\t200\t0\t1\t1M\tvolume\n" +
		"tank/src/vol1@a\t201\t0\t2\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": ""}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt",
		DryRun: true, Intermediate: true, SnapMode: SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	var vol *Step
	for _, st := range res.Plan.Steps {
		if st.DSSuffix == "/vol1" {
			vol = st
			break
		}
	}
	if vol == nil {
		t.Fatal("missing /vol1 step")
	}
	if vol.SrcType != "volume" {
		t.Fatalf("SrcType=%q", vol.SrcType)
	}
	joined := strings.Join(vol.Recv, " ")
	if strings.Contains(joined, "mountpoint") || strings.Contains(joined, "canmount") {
		t.Fatalf("volume recv FS flags: %v", vol.Recv)
	}
}

func TestSyncDirectionWarningAndPipes(t *testing.T) {
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\ntank/src@a\t101\t0\t2\t1K\tsnapshot\n"
	mk := func() *zfs.Fake {
		return &zfs.Fake{Lists: map[string]string{
			"tank/src": src, "tank/tgt": "",
		}}
	}
	// both endpoints remote, no direction → proxy warning + direction "" to pipe
	res, err := Run(context.Background(), mk(), Request{
		Source: "root@debian:tank/src", Target: "root@vault:tank/tgt",
		DryRun: false, Intermediate: true, SnapMode: SnapNever,
		SyncDirection: DirectionProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, w := range res.Warnings {
		if strings.Contains(w, "syncing remote endpoints through localhost") {
			found = true
		}
	}
	if !found {
		t.Fatalf("want proxy warning: %v", res.Warnings)
	}
	// Same remote both ends (hairpin) → NO proxy warning (oracle).
	res, err = Run(context.Background(), mk(), Request{
		Source: "root@debian:tank/src", Target: "root@debian:tank/tgt",
		DryRun: false, Intermediate: true, SnapMode: SnapNever,
		SyncDirection: DirectionProxy,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range res.Warnings {
		if strings.Contains(w, "through localhost") {
			t.Fatalf("hairpin must not warn: %v", res.Warnings)
		}
	}
	fake := mk() // re-run execute to capture pipe direction
	_, err = Run(context.Background(), fake, Request{
		Source: "root@debian:tank/src", Target: "root@vault:tank/tgt",
		DryRun: false, Intermediate: true, SnapMode: SnapNever,
		SyncDirection: DirectionPush,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Pipes) != 1 || fake.Pipes[0].Direction != "PUSH" {
		t.Fatalf("pipes=%+v", fake.Pipes)
	}

	// dry-run PULL shape
	res, err = Run(context.Background(), mk(), Request{
		Source: "root@debian:tank/src", Target: "root@vault:tank/tgt",
		DryRun: true, Intermediate: true, SnapMode: SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "ssh -n root@vault") {
		t.Fatalf("default pull shape:\n%s", res.Output)
	}
}

func TestOptSendRecvFlags(t *testing.T) {
	f := opt.Default()
	f.SendDefault = "--raw"
	f.RecvTop = "-o readonly=off"
	f.Resume = false
	p, err := PlanFromMatch([]PairView{{
		DSSuffix: "",
		Info:     "syncable (full)",
		SrcLast:  "@a",
		SrcName:  "tank/src",
		TgtName:  "tank/tgt",
	}}, false, f)
	if err != nil {
		t.Fatal(err)
	}
	out, err := FormatDryRun(p, "tank/src", "tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--raw") {
		t.Fatalf("send override missing:\n%s", out)
	}
	if !strings.Contains(out, "readonly=off") {
		t.Fatalf("recv top missing:\n%s", out)
	}
	if strings.Contains(out, " recv -v -s ") || strings.HasSuffix(strings.TrimSpace(out), "-s tank/tgt") {
		// no partial when Resume off
	}
	// recv should not include lone -s from PARTIAL
	joined := strings.Join(p.Steps[0].Recv, " ")
	if strings.Contains(joined, " -s") || strings.HasSuffix(joined, " -s") || strings.Contains(joined, " -s ") {
		// check more carefully: DefaultRecvPartial is "-s" as own token
		for _, a := range p.Steps[0].Recv {
			if a == "-s" {
				t.Fatalf("partial -s present with Resume=false: %v", p.Steps[0].Recv)
			}
		}
	}

	// nil Flags uses built-in defaults only (no process env).
	t.Setenv("ZELTA_SEND_DEFAULT", "-p")
	src := "tank/src\t100\t0\t1\t1M\tfilesystem\ntank/src@a\t101\t0\t2\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": ""}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt",
		DryRun: true, Intermediate: true, SnapMode: SnapNever,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Output, "zfs send -P -p") {
		t.Fatalf("env must not affect nil Flags path:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "zfs send -P -L -c -e") {
		t.Fatalf("want built-in send defaults:\n%s", res.Output)
	}
	// explicit Flags still apply
	flags := opt.Default()
	flags.SendDefault = "-p"
	res, err = Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt",
		DryRun: true, Intermediate: true, SnapMode: SnapNever,
		Flags: &flags,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "zfs send -P -p") {
		t.Fatalf("explicit Flags not applied:\n%s", res.Output)
	}
}

func TestRecvProperties(t *testing.T) {
	f := opt.Default()
	f.RecvPropsAdd = []string{"compression=lz4", "quota=10G"}
	f.RecvPropsDel = []string{"mountpoint", "canmount"}
	p, err := PlanFromMatch([]PairView{{
		DSSuffix: "", Info: "syncable (full)", SrcLast: "@a",
		SrcName: "tank/src", TgtName: "tank/tgt",
	}}, false, f)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(p.Steps[0].Recv, " ")
	want := "-o readonly=on -u -x mountpoint -o canmount=noauto -o compression=lz4 -o quota=10G -x mountpoint -x canmount -s"
	if got != "zfs recv -v "+want+" tank/tgt" {
		t.Fatalf("recv argv=%q", got)
	}

	f.RecvOverride = "-u"
	p, err = PlanFromMatch([]PairView{{
		DSSuffix: "", Info: "syncable (full)", SrcLast: "@a",
		SrcName: "tank/src", TgtName: "tank/tgt",
	}}, false, f)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Steps[0].Recv, " "); got != "zfs recv -v -u tank/tgt" {
		t.Fatalf("override should replace properties: %q", got)
	}
}

func TestCreateBookmarksAfterVerifiedReceive(t *testing.T) {
	fake := &zfs.Fake{Lists: map[string]string{"tank/tgt@daily": "tank/tgt@daily\n"}}
	plan := &Plan{Steps: []*Step{{Kind: KindIncremental, SourceEnd: "@daily", SrcName: "tank/src", TgtName: "tank/tgt"}}}
	flags := opt.Default()
	flags.BookmarkMode = "1"
	bookmarks, err := buildBookmarkPlans(plan, "root@src:tank/src", "root@dst:tank/tgt", flags.BookmarkPrefix, "dst")
	if err != nil {
		t.Fatal(err)
	}
	plan.Bookmarks = bookmarks
	if errors := createBookmarks(context.Background(), fake, Request{}, plan); len(errors) != 0 {
		t.Fatal(errors)
	}
	if len(fake.Bookmarks) != 1 || fake.Bookmarks[0].Bookmark != "tank/src#dst_daily" {
		t.Fatalf("bookmarks=%v", fake.Bookmarks)
	}
}

func TestBookmarkDryRunUsesLatestIntermediateSnapshot(t *testing.T) {
	plan, err := PlanFromMatch([]PairView{{
		DSSuffix: "", Info: "syncable (full)", SrcLast: "@new", SrcNext: "@old",
		SrcName: "tank/src", TgtName: "tank/tgt",
	}}, true, opt.Default())
	if err != nil {
		t.Fatal(err)
	}
	plan.Bookmarks, err = buildBookmarkPlans(plan, "root@src:tank/src", "root@dst:tank/tgt", "", "dst")
	if err != nil {
		t.Fatal(err)
	}
	out, err := FormatDryRun(plan, "root@src:tank/src", "root@dst:tank/tgt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "zfs list -Ho name tank/tgt@new") || !strings.Contains(out, "zfs bookmark tank/src@new tank/src#dst_new") {
		t.Fatalf("bookmark dry-run=%s", out)
	}
}

func TestBookmarkFailuresContinueAndReport(t *testing.T) {
	plan := &Plan{Bookmarks: []BookmarkPlan{
		{VerifyEndpoint: "tank/tgt", SourceEndpoint: "tank/src", Verify: []string{"zfs", "list", "-Ho", "name", "tank/tgt@a"}, Create: []string{"zfs", "bookmark", "tank/src@a", "tank/src#dst_a"}},
		{VerifyEndpoint: "tank/tgt", SourceEndpoint: "tank/src", Verify: []string{"zfs", "list", "-Ho", "name", "tank/tgt@b"}, Create: []string{"zfs", "bookmark", "tank/src@b", "tank/src#dst_b"}},
	}}
	fake := &zfs.Fake{
		Lists:      map[string]string{"tank/tgt@b": "tank/tgt@b\n"},
		ListErrors: map[string]error{"tank/tgt@a": fmt.Errorf("missing")},
	}
	errs := createBookmarks(context.Background(), fake, Request{}, plan)
	if len(errs) != 1 || len(fake.Bookmarks) != 1 || fake.Bookmarks[0].Bookmark != "tank/src#dst_b" {
		t.Fatalf("errors=%v bookmarks=%v", errs, fake.Bookmarks)
	}
}

func TestFilteredIntermediatePlansPerDataset(t *testing.T) {
	views := []PairView{
		{DSSuffix: "", Info: "syncable (full)", SrcName: "tank/src", TgtName: "tank/tgt", FilteredActive: true, FilteredEnds: []string{"@old", "@new"}},
		{DSSuffix: "/child", Info: "syncable (incremental)", Match: "@child-m", SrcName: "tank/src/child", TgtName: "tank/tgt/child", FilteredActive: true, FilteredEnds: []string{"@child-new"}},
	}
	p, err := PlanFromMatch(views, true, opt.Default())
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Steps) != 3 || p.Steps[0].Kind != KindFull || p.Steps[1].Kind != KindIncremental || p.Steps[2].SourceStart != "@child-m" {
		t.Fatalf("steps=%+v", p.Steps)
	}
	for _, st := range p.Steps {
		if st.Filtered && strings.Contains(strings.Join(st.Send, " "), " -I ") {
			t.Fatalf("filtered step used -I: %v", st.Send)
		}
		if st.Filtered && !containsArg(st.Recv, "-s") {
			t.Fatalf("filtered step lost resume receive flag: %v", st.Recv)
		}
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func TestFilteredEndsRespectSnapshotExclusion(t *testing.T) {
	v := PairView{
		DSSuffix: "/child", SrcName: "tank/src/child", Match: "@m",
		SrcSavepoints: []string{"@new", "@skip", "@m", "@old"},
	}
	f := match.ParseFilter(nil, []string{"@skip"})
	got := filteredEnds(v, f)
	if want := []string{"@new"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("filtered ends=%v want=%v", got, want)
	}
}

func TestRunFilteredIntermediateKeepsExcludedHistoryOutOfStream(t *testing.T) {
	src := "tank/src\t1\t0\t1\t1K\tfilesystem\n" +
		"tank/src@new\t4\t0\t4\t1K\tsnapshot\n" +
		"tank/src@skip\t3\t0\t3\t1K\tsnapshot\n" +
		"tank/src@m\t2\t0\t2\t1K\tsnapshot\n"
	tgt := "tank/tgt\t10\t0\t1\t0\tfilesystem\n" +
		"tank/tgt@m\t2\t0\t2\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": tgt}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt", Intermediate: true,
		SnapMode: SnapNever, Exclude: []string{"@skip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Pipes) != 1 {
		t.Fatalf("pipes=%d plan=%+v", len(fake.Pipes), res.Plan.Steps)
	}
	joined := strings.Join(fake.Pipes[0].Left, " ")
	if !strings.Contains(joined, "-i tank/src@m tank/src@new") || strings.Contains(joined, "@skip") {
		t.Fatalf("filtered send=%s", joined)
	}
}

func TestFilteredIntermediateIncludesCreatedSnapshot(t *testing.T) {
	src := "tank/src\t1\t1\t1\t1K\tfilesystem\n" +
		"tank/src@m\t2\t0\t2\t1K\tsnapshot\n"
	tgt := "tank/tgt\t10\t0\t1\t0\tfilesystem\n" +
		"tank/tgt@m\t2\t0\t2\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": tgt}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt", Intermediate: true,
		SnapMode: SnapAlways, SnapName: "created", Exclude: []string{"@skip"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Snapshots) != 1 || len(fake.Pipes) != 1 {
		t.Fatalf("snapshots=%v pipes=%v plan=%+v", fake.Snapshots, fake.Pipes, res.Plan.Steps)
	}
	joined := strings.Join(fake.Pipes[0].Left, " ")
	if !strings.Contains(joined, "-i tank/src@m tank/src@created") {
		t.Fatalf("created filtered send=%s", joined)
	}
}

func TestRunFilteredDatasetExclusionWinsOverParentInclude(t *testing.T) {
	src := "tank/src\t1\t0\t1\t1K\tfilesystem\n" +
		"tank/src@new\t4\t0\t4\t1K\tsnapshot\n" +
		"tank/src/child\t2\t0\t2\t1K\tfilesystem\n" +
		"tank/src/child@new\t5\t0\t5\t1K\tsnapshot\n"
	tgt := "tank/tgt\t10\t0\t1\t0\tfilesystem\n" +
		"tank/tgt@old\t1\t0\t1\t1K\tsnapshot\n" +
		"tank/tgt/child\t20\t0\t1\t0\tfilesystem\n" +
		"tank/tgt/child@old\t2\t0\t1\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": tgt}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt", Intermediate: true, SnapMode: SnapNever,
		Include: []string{"tank/src"}, Exclude: []string{"tank/src/child"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Pipes) != 1 || len(res.Plan.Steps) != 1 {
		t.Fatalf("pipes=%v steps=%+v", fake.Pipes, res.Plan.Steps)
	}
	if res.Plan.Steps[0].DSSuffix != "" {
		t.Fatalf("steps=%+v", res.Plan.Steps)
	}
}

func TestRunFilteredWithNoEligibleSnapshotsIsNoOp(t *testing.T) {
	src := "tank/src\t1\t0\t1\t1K\tfilesystem\n" +
		"tank/src@new\t4\t0\t4\t1K\tsnapshot\n"
	tgt := "tank/tgt\t10\t0\t1\t0\tfilesystem\n" +
		"tank/tgt@new\t4\t0\t4\t1K\tsnapshot\n"
	fake := &zfs.Fake{Lists: map[string]string{"tank/src": src, "tank/tgt": tgt}}
	res, err := Run(context.Background(), fake, Request{
		Source: "tank/src", Target: "tank/tgt", Intermediate: true, SnapMode: SnapNever,
		Exclude: []string{"@new"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fake.Pipes) != 0 || len(res.Plan.Steps) != 0 {
		t.Fatalf("filtered no-op produced work: pipes=%v steps=%+v", fake.Pipes, res.Plan.Steps)
	}
}

func TestFilteredBookmarksOnlyLatestPerDataset(t *testing.T) {
	plan := &Plan{Steps: []*Step{
		{DSSuffix: "/child", Kind: KindIncremental, SourceEnd: "@a", SrcName: "tank/src/child", TgtName: "tank/tgt/child"},
		{DSSuffix: "/child", Kind: KindIncremental, SourceEnd: "@b", SrcName: "tank/src/child", TgtName: "tank/tgt/child"},
	}}
	got, err := buildBookmarkPlans(plan, "tank/src", "tank/tgt", "", "dst")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || !strings.HasSuffix(got[0].Create[len(got[0].Create)-1], "dst_b") {
		t.Fatalf("bookmarks=%v", got)
	}
}
