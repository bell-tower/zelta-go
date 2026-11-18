package backup

import (
	"context"
	"strings"
	"testing"

	"zelta.space/internal/command"
	"zelta.space/internal/execx"
	"zelta.space/internal/progress"
)

// peekExec runs after once a pipeline has delivered its output, while the
// stream is still in flight.
type peekExec struct {
	execx.Executor
	after func()
}

func (p peekExec) Pipe(ctx context.Context, pl command.Pipeline, onLine func(execx.Line)) execx.Result {
	res := p.Executor.Pipe(ctx, pl, onLine)
	p.after()
	return res
}

func TestRemoteSendReportsProgressWhenLive(t *testing.T) {
	f := incrementalOn(t, "ssh -n me@s ", "ssh -n me@t ", nil)
	f.on(`ssh -n me@t "ssh -n me@s zfs send -Pv `+incr+` | zfs recv -v -s 'bpool/tgt'"`,
		execx.Out("size\t4096", "03:04:05\t2048\tapool/src@b", "receiving incremental stream of x@s into y@s", "received 4K stream in 1 seconds (4K/sec)"))
	board := &progress.Board{TTY: true, Now: f.tr.Now}
	var status strings.Builder
	f.tr.Progress = board
	f.tr.Exec = peekExec{f.tr.Exec, func() { board.Write(&status) }}
	if err := f.backup("me@s:apool/src", "me@t:bpool/tgt"); err != nil {
		t.Fatalf("err = %v\n%s", err, f.tr.Err.String())
	}
	f.noUnmatched()
	eqLines(t, "status", status.String(), "apool/src@a%b: 2K of 4K sent (50%), 0s elapsed")
	eqLines(t, "stdout", f.tr.Out.String(), "syncing 1 datasets", "dataset up-to-date", "4K sent, 1 streams received in 1 seconds")
	status.Reset()
	board.Write(&status)
	eqLines(t, "after", status.String(), "zelta: no streams in flight")
}

func TestSendProgressOption(t *testing.T) {
	for _, tt := range []struct {
		tty  bool
		opt  string
		send string
	}{
		{true, "", "zfs send -Pv"},
		{false, "", "zfs send -P"},
		{true, "0", "zfs send -P"},
		{false, "1", "zfs send -Pv"},
	} {
		opts := map[string]string{}
		if tt.opt != "" {
			opts["SEND_PROGRESS"] = tt.opt
		}
		f := incrementalOn(t, "", "", opts)
		f.on("{ "+tt.send+" "+incr+"|zfs recv -v -s 'bpool/tgt' ; }", received("incremental", 1024, 1))
		f.tr.Progress = &progress.Board{TTY: tt.tty}
		if err := f.backup("apool/src", "bpool/tgt"); err != nil {
			t.Fatalf("TTY=%v SEND_PROGRESS=%q: %v\n%s", tt.tty, tt.opt, err, f.tr.Err.String())
		}
		f.noUnmatched()
	}
}
