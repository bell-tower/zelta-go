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
		if v.SrcName == "" {
			continue
		}
		if v.SrcLast == "" {
			return "missing source snapshot; " + prefix
		}
		if truthyWritten(v.SrcWritten) {
			return "source is written; " + prefix
		}
	}
	return ""
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
