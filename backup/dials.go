package backup

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SnapMode controls whether backup creates a source snapshot before send.
type SnapMode string

// Snap modes (oracle SNAP_MODE).
const (
	SnapIfNeeded SnapMode = "IF_NEEDED"
	SnapAlways   SnapMode = "ALWAYS"
	SnapNever    SnapMode = "NEVER"
)

// ParseSnapMode maps CLI/env/JSON strings to SnapMode.
// Empty and unknown aliases become SnapIfNeeded.
func ParseSnapMode(s string) SnapMode {
	switch normalizeSnapMode(s) {
	case SnapAlways:
		return SnapAlways
	case SnapNever:
		return SnapNever
	default:
		return SnapIfNeeded
	}
}

func normalizeSnapMode(mode string) SnapMode {
	m := strings.ToUpper(strings.TrimSpace(mode))
	switch m {
	case "", string(SnapIfNeeded):
		return SnapIfNeeded
	case "0", "OFF", "NO", "FALSE", string(SnapNever):
		return SnapNever
	case "1", "YES", "TRUE", string(SnapAlways):
		return SnapAlways
	default:
		return SnapMode(m)
	}
}

// SyncDirection selects dual-remote pipe placement (oracle SYNC_DIRECTION).
type SyncDirection string

const (
	// DirectionPull runs the pipe on the target (default; zero value).
	DirectionPull SyncDirection = "PULL"
	// DirectionPush runs the pipe on the source.
	DirectionPush SyncDirection = "PUSH"
	// DirectionProxy runs send|recv on the controller between two remotes.
	DirectionProxy SyncDirection = "PROXY"
)

// ParseSyncDirection maps CLI/env/JSON strings to SyncDirection.
// Empty → DirectionPull. Falsey ("0", "no", …) → DirectionProxy.
func ParseSyncDirection(s string) SyncDirection {
	d := strings.TrimSpace(s)
	if d == "" {
		return DirectionPull
	}
	switch strings.ToLower(d) {
	case "0", "no", "false", "off", "proxy":
		return DirectionProxy
	default:
		return SyncDirection(strings.ToUpper(d))
	}
}

// Normalize returns DirectionPull for the zero value; otherwise d.
func (d SyncDirection) Normalize() SyncDirection {
	if d == "" {
		return DirectionPull
	}
	return d
}

// pipeArg is the string passed to zfs pipe helpers (proxy → "").
func (d SyncDirection) pipeArg() string {
	switch d.Normalize() {
	case DirectionPush:
		return "PUSH"
	case DirectionProxy:
		return ""
	default:
		return "PULL"
	}
}

// ParseSnapTime maps a duration string or integer seconds to time.Duration.
// Empty → 0 (unset). Absolute Unix epochs are not accepted; use a duration.
func ParseSnapTime(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	if d, err := time.ParseDuration(s); err == nil {
		if d < 0 {
			return 0, fmt.Errorf("snap time: negative duration %q", s)
		}
		return d, nil
	}
	sec, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("snap time: %q (want Go duration or seconds)", s)
	}
	if sec < 0 {
		return 0, fmt.Errorf("snap time: negative seconds %q", s)
	}
	return time.Duration(sec) * time.Second, nil
}

// ParseSnapSize maps a decimal byte count string to int64. Empty → 0 (unset).
func ParseSnapSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("snap size: %w", err)
	}
	if n < 0 {
		return 0, fmt.Errorf("snap size: negative %d", n)
	}
	return n, nil
}
