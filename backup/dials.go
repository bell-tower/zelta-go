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
// Empty → (SnapIfNeeded, nil). Unknown non-empty values error (strict import edge).
// SKIP is the rotate dialect alias for SnapNever.
func ParseSnapMode(s string) (SnapMode, error) {
	m := strings.ToUpper(strings.TrimSpace(s))
	switch m {
	case "", string(SnapIfNeeded):
		return SnapIfNeeded, nil
	case string(SnapNever), "0", "OFF", "NO", "FALSE", "SKIP":
		return SnapNever, nil
	case string(SnapAlways), "1", "YES", "TRUE":
		return SnapAlways, nil
	default:
		return "", fmt.Errorf("invalid snap mode: %q (want IF_NEEDED, ALWAYS, NEVER)", s)
	}
}

// normalizeSnapMode coerces a typed SnapMode to a known constant. Lenient —
// used inside actions where values are already typed; unknown → SnapIfNeeded.
func normalizeSnapMode(mode SnapMode) SnapMode {
	switch mode {
	case SnapNever, SnapAlways:
		return mode
	default:
		return SnapIfNeeded
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
// Empty → (DirectionPull, nil). Unknown values error (strict import edge).
// Falsey ("0", "no", …) → DirectionProxy (oracle dual-remote default).
func ParseSyncDirection(s string) (SyncDirection, error) {
	d := strings.TrimSpace(s)
	if d == "" {
		return DirectionPull, nil
	}
	switch strings.ToLower(d) {
	case "pull":
		return DirectionPull, nil
	case "push":
		return DirectionPush, nil
	case "proxy", "0", "no", "false", "off":
		return DirectionProxy, nil
	default:
		return "", fmt.Errorf("invalid sync direction: %q (want PULL, PUSH, PROXY)", s)
	}
}

// Normalize returns DirectionPull for the zero value; otherwise d.
func (d SyncDirection) Normalize() SyncDirection {
	if d == "" {
		return DirectionPull
	}
	return d
}

// PipeArg returns the token passed to zfs pipe helpers at the Executor edge
// (DirectionProxy → "", the controller/dual-remote form).
func (d SyncDirection) PipeArg() string {
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
