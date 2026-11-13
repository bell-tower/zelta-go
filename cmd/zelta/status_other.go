//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd)

package main

import (
	"os"
	"syscall"
)

// statusSignals request a status line. There is no SIGINFO here, so only
// SIGUSR1 (as with dd(1)).
var statusSignals = []os.Signal{syscall.SIGUSR1}
