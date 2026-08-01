package prune

import (
	"fmt"
	"strings"
	"time"
)

// PruneGuard selects how a match-endpoint protects snapshots (oracle GUARD_*).
type PruneGuard string

// Guard modes.
const (
	GuardNone     PruneGuard = "none"
	GuardLatest   PruneGuard = "latest"
	GuardUnsynced PruneGuard = "unsynced"
)

// ParsePruneGuard maps CLI/env/JSON strings to PruneGuard.
// Empty → GuardLatest (caller may force GuardNone when no match endpoint).
func ParsePruneGuard(s string) (PruneGuard, error) {
	g := PruneGuard(strings.ToLower(strings.TrimSpace(s)))
	if g == "" {
		return GuardLatest, nil
	}
	switch g {
	case GuardNone, GuardLatest, GuardUnsynced:
		return g, nil
	default:
		return "", fmt.Errorf("invalid prune-guard mode: %s", s)
	}
}

// ParsePruneTime maps oracle duration strings to a duration pointer.
// Empty → nil (unset). Non-empty including "0" → set (may be zero).
// Accepts the same units as ParseDuration (s, mi, h, d, w, mo, y).
func ParsePruneTime(s string) (*time.Duration, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	secs, err := ParseDuration(s)
	if err != nil {
		return nil, fmt.Errorf("invalid --prune-time: %s", s)
	}
	d := time.Duration(secs) * time.Second
	return &d, nil
}
