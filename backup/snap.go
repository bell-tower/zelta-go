package backup

import (
	"strconv"
	"strings"
	"time"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
)

// Snap modes (oracle SNAP_MODE).
const (
	SnapIfNeeded = "IF_NEEDED"
	SnapAlways   = "ALWAYS"
	SnapNever    = "NEVER"
)

// DefaultSnapName matches bin/zelta: date -u +zelta_%Y-%m-%d_%H.%M.%S
func DefaultSnapName() string {
	return time.Now().UTC().Format("zelta_2006-01-02_15.04.05")
}

// ShouldSnapshot returns a human reason prefix if a source snap is needed.
// mode empty → IF_NEEDED.
func ShouldSnapshot(mode string, views []PairView) string {
	return ShouldSnapshotWithThresholds(mode, views, "", "")
}

// ShouldSnapshotWithThresholds applies SNAP_TIME and SNAP_SIZE only to
// IF_NEEDED. Every configured threshold must allow skipping; invalid or
// missing threshold data conservatively requires a snapshot.
func ShouldSnapshotWithThresholds(mode string, views []PairView, snapTime, snapSize string) string {
	mode = normalizeSnapMode(mode)
	if mode == SnapNever {
		return ""
	}
	prefix := "snapshotting: "
	if mode == SnapAlways {
		return prefix
	}
	// IF_NEEDED
	for _, v := range views {
		if v.SrcName != "" && v.SrcLast == "" {
			return "missing source snapshot; " + prefix
		}
	}
	needsThresholds := strings.TrimSpace(snapTime) != "" || strings.TrimSpace(snapSize) != ""
	if needsThresholds {
		if thresholdsAllowSkip(views, snapTime, snapSize) {
			return ""
		}
		return "snapshot threshold reached; " + prefix
	}
	for _, v := range views {
		if v.SrcName == "" {
			continue
		}
		if truthyWritten(v.SrcWritten) {
			return "source is written; " + prefix
		}
	}
	return ""
}

func thresholdsAllowSkip(views []PairView, snapTime, snapSize string) bool {
	if strings.TrimSpace(snapTime) != "" {
		cutoff, ok := snapshotTimeCutoff(snapTime, time.Now())
		if !ok {
			return false
		}
		for _, v := range views {
			if v.SrcName == "" {
				continue
			}
			ts, err := strconv.ParseInt(strings.TrimSpace(v.SrcSnapshotsChanged), 10, 64)
			if err != nil || time.Unix(ts, 0).Before(cutoff) {
				return false
			}
		}
	}
	if strings.TrimSpace(snapSize) != "" {
		threshold, err := strconv.ParseInt(strings.TrimSpace(snapSize), 10, 64)
		if err != nil || threshold < 0 {
			return false
		}
		var written int64
		for _, v := range views {
			if v.SrcName == "" {
				continue
			}
			n, err := strconv.ParseInt(strings.TrimSpace(v.SrcWritten), 10, 64)
			if err != nil || n < 0 {
				return false
			}
			written += n
		}
		if written >= threshold {
			return false
		}
	}
	return true
}

func snapshotTimeCutoff(value string, now time.Time) (time.Time, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}, false
	}
	if epoch, err := strconv.ParseInt(value, 10, 64); err == nil {
		return time.Unix(epoch, 0), true
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration < 0 {
		return time.Time{}, false
	}
	return now.Add(-duration), true
}

func normalizeSnapMode(mode string) string {
	m := strings.ToUpper(strings.TrimSpace(mode))
	switch m {
	case "", SnapIfNeeded:
		return SnapIfNeeded
	case "0", "OFF", "NO", "FALSE", SnapNever:
		return SnapNever
	case "1", "YES", "TRUE", SnapAlways:
		return SnapAlways
	default:
		return m
	}
}

func truthyWritten(w string) bool {
	if w == "" || w == "-" {
		return false
	}
	n, err := strconv.ParseInt(w, 10, 64)
	if err == nil {
		return n != 0
	}
	return w != "0"
}

// BuildSnapArgv returns zfs snapshot -r root@name argv (cmds.tsv SNAP).
func BuildSnapArgv(srcDataset, savepoint string) ([]string, error) {
	if savepoint == "" || savepoint[0] != '@' {
		savepoint = "@" + strings.TrimPrefix(savepoint, "@")
	}
	return cmdbuild.SnapArgv(srcDataset + savepoint)
}
