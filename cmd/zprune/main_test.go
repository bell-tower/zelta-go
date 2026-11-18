package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"zelta.space/internal/version"
)

func TestWiring(t *testing.T) {
	a := newApp()
	if len(a.Engines) != 1 || a.Engines["zprune"] == nil {
		t.Fatalf("engines = %v, want zprune only", a.Engines)
	}
	if a.Multiplex == nil {
		t.Errorf("ssh multiplexing not wired")
	}
}

func TestHelpAndVersion(t *testing.T) {
	for _, tc := range []struct{ arg, want string }{
		{"-V", version.Zprune() + "\n"},
		{"--help", "usage: zprune [OPTIONS] ENDPOINT"},
	} {
		a := newApp()
		var out bytes.Buffer
		a.Stdout, a.Stderr = &out, &out
		if code := a.Main(context.Background(), []string{"zprune", tc.arg}); code != 0 || !strings.Contains(out.String(), tc.want) {
			t.Errorf("zprune %s: code=%d %q", tc.arg, code, out.String())
		}
	}
}
