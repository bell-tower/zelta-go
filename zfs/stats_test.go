package zfs

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseStatsLine(t *testing.T) {
	st := &PipeStats{}
	lines := []string{
		"incremental\tgo\tapool/treetop@zelta_2026-06-23_13.34.19.5334\t624",
		"size\t2496",
		"receiving incremental stream of apool/treetop@zelta_2026-06-23_13.34.19.5334 into cpool/treetopzelta-go@zelta_2026-06-23_13.34.19.5334",
		"snap cpool/treetopzelta-go@zelta_2026-06-23_13.34.19.5334 already exists; ignoring",
		"received 0B stream in 0.01 seconds (0B/sec)",
		"size\t512",
		"received 312B stream in 0.04 seconds (6.93K/sec)",
		"cannot receive new stream: checksum mismatch",
	}
	for _, l := range lines {
		parseStatsLine(st, l)
	}
	if st.Bytes != 3008 {
		t.Errorf("Bytes = %d, want 3008", st.Bytes)
	}
	if st.Streams != 2 {
		t.Errorf("Streams = %d, want 2", st.Streams)
	}
	if st.Secs < 0.049 || st.Secs > 0.051 {
		t.Errorf("Secs = %v, want ~0.05", st.Secs)
	}
}

// TestRealPipeStats runs a fake zfs binary through the local pipe path and
// checks TakeStats sees the send -P size header and the recv -v summary line.
func TestRealPipeStats(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "zfs")
	script := `#!/bin/sh
case "$1" in
  send) echo "size 2496" >&2; printf 'stream';;
  recv) cat >/dev/null; echo "received 2K stream in 0.03 seconds (66.7K/sec)";;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	r := &Real{}
	ctx := context.Background()
	if err := r.RunPipeDirection(
		ctx, "pool/ds", []string{"zfs", "send", "-P", "-v", "pool/ds@snap"},
		"pool/dst", []string{"zfs", "recv", "-v", "-s", "pool/dst"},
		"PULL",
	); err != nil {
		t.Fatalf("RunPipeDirection: %v", err)
	}
	st := r.TakeStats()
	if st.Bytes != 2496 {
		t.Errorf("Bytes = %d, want 2496", st.Bytes)
	}
	if st.Streams != 1 {
		t.Errorf("Streams = %d, want 1", st.Streams)
	}
	if st.Secs < 0.029 || st.Secs > 0.031 {
		t.Errorf("Secs = %v, want ~0.03", st.Secs)
	}
}

// TestRealPipeStatsReset verifies TakeStats consumes the counters.
func TestRealPipeStatsReset(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "zfs")
	script := `#!/bin/sh
case "$1" in
  send) echo "size 1" >&2;;
  recv) cat >/dev/null; echo "received 1B stream in 0.01 seconds (1B/sec)";;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	r := &Real{}
	ctx := context.Background()
	if err := r.RunPipeDirection(ctx, "pool/ds", []string{"zfs", "send", "pool/ds@snap"}, "pool/dst", []string{"zfs", "recv", "pool/dst"}, "PULL"); err != nil {
		t.Fatalf("RunPipeDirection: %v", err)
	}
	if st := r.TakeStats(); st.Streams != 1 {
		t.Fatalf("Streams = %d, want 1", st.Streams)
	}
	if st := r.TakeStats(); st.Bytes != 0 || st.Streams != 0 || st.Secs != 0 {
		t.Errorf("TakeStats after reset = %+v, want zero", st)
	}
}

// TestRealPipeStderrLog verifies the sink forwards complete lines to
// StderrLog when set.
func TestRealPipeStderrLog(t *testing.T) {
	dir := t.TempDir()
	fake := filepath.Join(dir, "zfs")
	script := `#!/bin/sh
case "$1" in
  send) echo "size 10" >&2;;
  recv) cat >/dev/null; echo "received 1B stream in 0.01 seconds (1B/sec)";;
esac
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	var log bytes.Buffer
	r := &Real{StderrLog: &log}
	ctx := context.Background()
	if err := r.RunPipeDirection(ctx, "pool/ds", []string{"zfs", "send", "pool/ds@snap"}, "pool/dst", []string{"zfs", "recv", "pool/dst"}, "PULL"); err != nil {
		t.Fatalf("RunPipeDirection: %v", err)
	}
	if got := log.String(); got != "size 10\nreceived 1B stream in 0.01 seconds (1B/sec)\n" {
		t.Errorf("StderrLog = %q", got)
	}
}
