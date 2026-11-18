//go:build !unix

package main

import "os"

// statusSignals is empty: there is no status signal off Unix.
var statusSignals []os.Signal
