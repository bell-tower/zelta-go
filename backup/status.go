package backup

import "strings"

// ErrCode classifies a backup outcome for programmatic handling.
// Empty (ErrCodeNone) means no known failure pattern matched — not a guarantee
// that streams were sent. Check Plan or Stats for transfer counts.
type ErrCode string

const (
	// ErrCodeNone means no known failure classification matched.
	ErrCodeNone ErrCode = ""
	// ErrCodeUpToDate: target already matches source; nothing to send.
	ErrCodeUpToDate ErrCode = "up_to_date"
	// ErrCodeNoSource: source dataset does not exist.
	ErrCodeNoSource ErrCode = "no_source"
	// ErrCodeNoSourceSnapshot: source has no eligible snapshots.
	ErrCodeNoSourceSnapshot ErrCode = "no_source_snapshot"
	// ErrCodeSourceSnapshot: snapshot create/verify failed.
	ErrCodeSourceSnapshot ErrCode = "source_snapshot_failed"
	// ErrCodeTargetLocalWrites: target has local writes blocking receive.
	ErrCodeTargetLocalWrites ErrCode = "target_local_writes"
	// ErrCodeDiverged: target diverged from source.
	ErrCodeDiverged ErrCode = "target_diverged"
	// ErrCodeNoCommonSnapshot: no common snapshot between source and target.
	ErrCodeNoCommonSnapshot ErrCode = "no_common_snapshot"
)

// Blocked reports whether the code is a hard failure (not none/up-to-date).
func (c ErrCode) Blocked() bool {
	return c != ErrCodeNone && c != ErrCodeUpToDate
}

// ErrCodeFromOutput classifies backup human/log output.
// Pattern set aligned with Sylve's classifyBackupOutput.
func ErrCodeFromOutput(output string) ErrCode {
	lower := strings.ToLower(output)
	switch {
	case strings.Contains(lower, "no common snapshot (diverged)"):
		return ErrCodeNoCommonSnapshot
	case strings.Contains(lower, "no snapshot; target diverged"):
		return ErrCodeDiverged
	case strings.Contains(lower, "target has local writes"):
		return ErrCodeTargetLocalWrites
	case strings.Contains(lower, "target has diverged"), strings.Contains(lower, "target diverged"):
		return ErrCodeDiverged
	case strings.Contains(lower, "no source snapshot"):
		return ErrCodeNoSourceSnapshot
	case strings.Contains(lower, "source_snapshot_creation_failed"),
		strings.Contains(lower, "source_snapshot_verification_failed"):
		return ErrCodeSourceSnapshot
	case strings.Contains(lower, "no source:"):
		return ErrCodeNoSource
	case strings.Contains(lower, "up-to-date"):
		return ErrCodeUpToDate
	default:
		return ErrCodeNone
	}
}
