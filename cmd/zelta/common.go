package main

import (
	"fmt"
	"os"
	"strconv"

	"git.belltower.it/djbell/zelta-go/internal/opt"
)

func printWarns(warns []string) {
	for _, w := range warns {
		fmt.Fprintf(os.Stderr, "warning: %s\n", w)
	}
}

// depthFrom resolves DEPTH ("" = unlimited → 0). On invalid input it prints
// the error and returns a non-zero exit code as the second value.
func depthFrom(e opt.Env, verb string) (int, int) {
	s := e.Get("DEPTH")
	if s == "" {
		return 0, 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 {
		fmt.Fprintf(os.Stderr, "error: depth of '%s' invalid; must be positive\n", s) // oracle stop() prefix
		return 0, 1
	}
	return n, 0
}
