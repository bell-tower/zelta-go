package backup

import (
	"strconv"
	"strings"
	"time"

	"git.belltower.it/djbell/zelta-go/internal/cmdbuild"
)

// DefaultSnapName matches bin/zelta: date -u +zelta_%Y-%m-%d_%H.%M.%S
func DefaultSnapName() string {
	return time.Now().UTC().Format("zelta_2006-01-02_15.04.05")
}

// ShouldSnapshot returns a human reason prefix if a source snap is needed.
// Zero mode → IF_NEEDED.
func ShouldSnapshot(mode SnapMode, views []PairView) string {
	return ShouldSnapshotWithThresholds(mode, views, 0, 0)
}

// ShouldSnapshotWithThresholds applies SnapTime and SnapSize only to
// IF_NEEDED. Every configured threshold must allow skipping; missing
// threshold data conservatively requires a snapshot.
// snapTime/snapSize zero means unset.
func ShouldSnapshotWithThresholds(mode SnapMode, views []PairView, snapTime time.Duration, snapSize int64) string {
	mode = ParseSnapMode(string(mode))
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
	needsThresholds := snapTime > 0 || snapSize > 0
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

func thresholdsAllowSkip(views []PairView, snapTime time.Duration, snapSize int64) bool {
	if snapTime > 0 {
		cutoff := time.Now().Add(-snapTime)
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
	if snapSize > 0 {
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
		if written >= snapSize {
			return false
		}
	}
	return true
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
