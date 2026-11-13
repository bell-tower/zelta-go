//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/bell-tower/zelta-go/internal/progress"
)

// statusDelay lets local zfs send processes, which receive the terminal's
// SIGINFO too, report their byte counts before the status is printed.
const statusDelay = 250 * time.Millisecond

// statusBoard prints the streams in flight to stderr on SIGINFO (Ctrl+T).
// Remote senders report progress only when stderr is a terminal.
func statusBoard() *progress.Board {
	fi, err := os.Stderr.Stat()
	b := &progress.Board{Live: err == nil && fi.Mode()&os.ModeCharDevice != 0}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINFO)
	go func() {
		for range ch {
			time.Sleep(statusDelay)
			b.Write(os.Stderr)
		}
	}()
	return b
}
