//go:build darwin || dragonfly || freebsd || netbsd || openbsd

package main

import (
	"os"
	"syscall"
)

// statusSignals request a status line: Ctrl+T (SIGINFO) or SIGUSR1.
var statusSignals = []os.Signal{syscall.SIGINFO, syscall.SIGUSR1}
