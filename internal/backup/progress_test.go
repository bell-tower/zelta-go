package backup

import (
	"context"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/progress"
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
	board := &progress.Board{Live: true, Now: f.tr.Now}
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
