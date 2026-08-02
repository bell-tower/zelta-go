package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/internal/opt"
	"github.com/bell-tower/zelta-go/internal/zlog"
)

// parseEndpoint maps a CLI operand/env string to endpoint.Endpoint.
// Empty s returns a zero endpoint and nil error.
func parseEndpoint(s string) (endpoint.Endpoint, error) {
	if s == "" {
		return endpoint.Endpoint{}, nil
	}
	return endpoint.Parse(s)
}

// printWarns emits warnings through the sink (oracle LOG_WARNING; suppressed
// at LOG_LEVEL 0, e.g. `zelta VERB -qq`).
func printWarns(s *zlog.Sink, warns []string) {
	if s == nil {
		return
	}
	for _, w := range warns {
		s.Warning(w)
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
