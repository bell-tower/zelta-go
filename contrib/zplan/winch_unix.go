//go:build unix

package main

import (
	"os"
	"os/signal"
	"syscall"
)

func winch() <-chan os.Signal {
	c := make(chan os.Signal, 1)
	signal.Notify(c, syscall.SIGWINCH)
	return c
}
