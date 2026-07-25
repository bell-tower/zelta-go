package backup

import (
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/internal/zfs"
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
		dsSnap := p.SnapArgv[len(p.SnapArgv)-1]
		sh, err := zfs.SnapshotShell(srcEp, dsSnap, hasRecursive(p.SnapArgv))
		if err != nil {
			return "", err
		}
		b.WriteString("+ ")
		b.WriteString(sh)
		b.WriteByte('\n')
	}
	for _, st := range p.Steps {
		if st.Kind != KindFull && st.Kind != KindIncremental {
			continue
		}
		// Oracle dry-run shows first pass only (second pass is execute-time).
		body, err := zfs.PipeShellDirection(srcEp, tgtEp, st.Send, st.Recv, direction)
		if err != nil {
			return "", err
		}
		b.WriteString("+ ")
		b.WriteString(body)
		b.WriteByte('\n')
	}
	for _, bm := range p.Bookmarks {
		verify, err := zfs.CommandShell(bm.VerifyEndpoint, bm.Verify)
		if err != nil {
			return "", err
		}
		create, err := zfs.CommandShell(bm.SourceEndpoint, bm.Create)
		if err != nil {
			return "", err
		}
		b.WriteString("+ ")
		b.WriteString(verify)
		b.WriteByte('\n')
		b.WriteString("+ ")
		b.WriteString(create)
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
