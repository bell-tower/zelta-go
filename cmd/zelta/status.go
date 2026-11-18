package main

import (
	"os"
	"os/signal"

	"zelta.space/internal/progress"
)

// statusBoard prints the streams in flight to stderr on each status signal.
func statusBoard() *progress.Board {
	fi, err := os.Stderr.Stat()
	b := &progress.Board{TTY: err == nil && fi.Mode()&os.ModeCharDevice != 0}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, statusSignals...)
	go func() {
		for range ch {
			b.Write(os.Stderr)
		}
	}()
	return b
}
