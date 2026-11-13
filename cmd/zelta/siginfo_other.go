//go:build !(darwin || dragonfly || freebsd || netbsd || openbsd)

package main

import "github.com/bell-tower/zelta-go/internal/progress"

// statusBoard returns nil: this platform has no SIGINFO.
func statusBoard() *progress.Board { return nil }
