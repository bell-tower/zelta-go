package backup

import (
	"fmt"
	"strings"
)

// FormatDryRun prints oracle-ish banner + "+ …" lines for snap + first-pass steps.
func FormatDryRun(p *Plan, srcEp, tgtEp string) (string, error) {
	return FormatDryRunDirection(p, srcEp, tgtEp, "PULL")
}

// FormatDryRunDirection is FormatDryRun with explicit dual-remote direction.
func FormatDryRunDirection(p *Plan, srcEp, tgtEp, direction string) (string, error) {
	var b strings.Builder
	if n := p.Full + p.Incr; n > 0 {
		b.WriteString(fmt.Sprintf("would sync %d datasets\n", n))
	}
	if p.SnapReason != "" && p.SnapSavepoint != "" && len(p.SnapArgv) > 0 {
		name := strings.TrimPrefix(p.SnapSavepoint, "@")
		b.WriteString(strings.Replace(p.SnapReason, "snapshotting: ", "would snapshot: ", 1))
		b.WriteString(name)
		b.WriteByte('\n')
	}
	lines, err := p.Commands(srcEp, tgtEp, direction)
	if err != nil {
		return "", err
	}
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, st := range p.Steps {
		if st.Kind != KindBlocked || st.Notice == "" {
			continue
		}
		b.WriteString(st.Notice)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func hasRecursive(argv []string) bool {
	for _, a := range argv {
		if a == "-r" {
			return true
		}
	}
	return false
}
