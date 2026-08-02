package report

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/bell-tower/zelta-go/backup"
	"github.com/bell-tower/zelta-go/endpoint"
	"github.com/bell-tower/zelta-go/zfs"
)

// HumanBytes renders a byte count like the upstream Awk h_num() helper:
// binary units (B, K, M, G, T, P, E), value truncated toward zero.
func HumanBytes(n int64) string {
	if n < 0 {
		return "-" + HumanBytes(-n)
	}
	suffix := "B"
	num := float64(n)
	for _, s := range []string{"K", "M", "G", "T", "P", "E"} {
		if num < 1024 {
			break
		}
		num /= 1024
		suffix = s
	}
	return strconv.FormatInt(int64(num), 10) + suffix
}

// PlanSummary is a short human line for a backup plan.
func PlanSummary(p *backup.Plan) string {
	if p == nil {
		return ""
	}
	var parts []string
	if p.SnapSavepoint != "" {
		parts = append(parts, "snapshot "+p.SnapSavepoint)
	}
	if p.Full > 0 {
		parts = append(parts, fmt.Sprintf("%d full", p.Full))
	}
	if p.Incr > 0 {
		parts = append(parts, fmt.Sprintf("%d incremental", p.Incr))
	}
	if p.Skip > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", p.Skip))
	}
	if p.Block > 0 {
		parts = append(parts, fmt.Sprintf("%d blocked", p.Block))
	}
	return strings.Join(parts, ", ")
}

// FormatBackupDryRun prints oracle-ish banner + "+ …" lines for snap + first-pass steps.
func FormatBackupDryRun(p *backup.Plan, src, tgt endpoint.Endpoint, direction string) (string, error) {
	if p == nil {
		return "", nil
	}
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
	cmds := p.Commands(src, tgt, direction)
	for _, c := range cmds {
		line, err := c.ShellLine()
		if err != nil {
			return "", err
		}
		b.WriteString("+ ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	for _, st := range p.Steps {
		if st.Kind != backup.KindBlocked || st.Notice == "" {
			continue
		}
		b.WriteString(st.Notice)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// FormatBackupRun builds oracle human progress text from a completed backup Result
// (syncing / up-to-date / sent summary). Dry-run "+ …" lines use FormatBackupDryRun.
func FormatBackupRun(res *backup.Result) string {
	if res == nil || res.Plan == nil {
		return ""
	}
	p := res.Plan
	var b strings.Builder
	work := p.Full + p.Incr
	total := work + p.Skip + p.Block
	if work > 0 && total > 0 {
		b.WriteString(fmt.Sprintf("syncing %d datasets\n", total))
	}
	if work == 0 && p.Skip > 0 {
		if p.Skip == 1 {
			b.WriteString("dataset up-to-date\n")
		} else {
			b.WriteString(fmt.Sprintf("%d datasets up-to-date\n", p.Skip))
		}
	}
	if work > 0 {
		secs := 0.0
		if !res.StartTime.IsZero() && !res.EndTime.IsZero() {
			secs = res.EndTime.Sub(res.StartTime).Seconds()
		}
		streams := work
		stats := res.Stats
		if stats.Streams > 0 {
			streams = stats.Streams
		}
		if stats.Secs > 0 {
			secs = stats.Secs
		}
		b.WriteString(fmt.Sprintf("%s sent, %d streams received in %g seconds\n", HumanBytes(stats.Bytes), streams, secs))
	}
	return b.String()
}

// FormatCommandLines renders structured commands as "+ …" dry-run lines.
func FormatCommandLines(cmds []zfs.Command) (string, error) {
	var b strings.Builder
	for _, c := range cmds {
		line, err := c.ShellLine()
		if err != nil {
			return "", err
		}
		b.WriteString("+ ")
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String(), nil
}
