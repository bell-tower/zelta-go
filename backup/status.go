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

// ErrCodeFromPlan classifies a plan from step kinds and notices (library path).
// Prefer this over ErrCodeFromOutput for all new code.
func ErrCodeFromPlan(p *Plan) ErrCode {
	if p == nil {
		return ErrCodeNone
	}
	for _, st := range p.Steps {
		if st == nil {
			continue
		}
		if st.Kind == KindBlocked {
			if c := classifyNotice(st.Notice); c != ErrCodeNone {
				return c
			}
			if c := classifyNotice(st.Info); c != ErrCodeNone {
				return c
			}
			// Generic blocked without a known pattern still signals divergence-ish stop.
			if strings.Contains(strings.ToLower(st.Notice+st.Info), "diverg") {
				return ErrCodeDiverged
			}
		}
		if st.Notice == "no source snapshot to send" {
			return ErrCodeNoSourceSnapshot
		}
	}
	if p.Full+p.Incr == 0 && p.Skip > 0 {
		return ErrCodeUpToDate
	}
	return ErrCodeNone
}

// ErrCodeFromOutput classifies backup human/log output.
// Legacy adapter for external scrapers (e.g. Sylve); library paths use ErrCodeFromPlan.
func ErrCodeFromOutput(output string) ErrCode {
	return classifyNotice(output)
}

func classifyNotice(s string) ErrCode {
	lower := strings.ToLower(s)
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
