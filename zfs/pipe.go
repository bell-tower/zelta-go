package zfs

import (
	"fmt"
	"strings"

	"github.com/bell-tower/zelta-go/endpoint"
)

// PipeShell formats a dry-run "+ …" body (no leading "+ ") for send|recv.
// Matches oracle shape for same-host remote: ssh -n host "{ send | recv ; }"
func PipeShell(leftEp, rightEp string, leftArgv, rightArgv []string) (string, error) {
	return PipeShellDirection(leftEp, rightEp, leftArgv, rightArgv, "PULL")
}

// PipeShellDirection adds dual-remote direction handling (oracle get_sync_command):
// PULL → ssh -n TGT "ssh -n SRC send | recv"; PUSH → ssh -n SRC "send | ssh TGT recv";
// else (proxy) → { ssh -n SRC send|ssh TGT recv ; } on the controller.
func PipeShellDirection(leftEp, rightEp string, leftArgv, rightArgv []string, direction string) (string, error) {
	lep, err := endpoint.Parse(leftEp)
	if err != nil {
		return "", fmt.Errorf("pipe left endpoint: %w", err)
	}
	rep, err := endpoint.Parse(rightEp)
	if err != nil {
		return "", fmt.Errorf("pipe right endpoint: %w", err)
	}
	lt, lok := sshTarget(lep)
	rt, rok := sshTarget(rep)
	// Soft quote for readability (oracle quotes paths, not every token).
	left := SoftJoin(leftArgv)
	right := SoftJoin(rightArgv)

	switch {
	case !lok && !rok:
		return left + " | " + right, nil
	case lok && rok && lt == rt:
		// Inside ssh, still soft-join; whole remote is one shell string.
		return "ssh -n " + lt + " \"{ " + left + " | " + right + " ; }\"", nil
	case lok && !rok:
		return "ssh -n " + lt + " " + shellSingleQuote(left) + " | " + right, nil
	case !lok && rok:
		return left + " | ssh -n " + rt + " " + shellSingleQuote(right), nil
	default:
		// dual-remote
		switch strings.ToUpper(direction) {
		case "PULL":
			return "ssh -n " + rt + " \"ssh -n " + lt + " " + left + " | " + right + "\"", nil
		case "PUSH":
			return "ssh -n " + lt + " \"" + left + " | ssh " + rt + " " + right + "\"", nil
		default:
			return "{ ssh -n " + lt + " " + left + "|ssh " + rt + " " + right + " ; }", nil
		}
	}
}

// SnapshotShell formats zfs snapshot for dry-run (no leading "+ ").
func SnapshotShell(epStr, datasetSnap string, recursive bool) (string, error) {
	args := []string{"zfs", "snapshot"}
	if recursive {
		args = append(args, "-r")
	}
	args = append(args, datasetSnap)
	return CommandShell(epStr, args)
}

// CommandShell formats one zfs command for an endpoint, including its remote
// ssh wrapper when needed.
func CommandShell(epStr string, argv []string) (string, error) {
	ep, err := endpoint.Parse(epStr)
	if err != nil {
		return "", err
	}
	cmd := SoftJoin(argv)
	if target, ok := sshTarget(ep); ok {
		return "ssh -n " + target + " " + shellSingleQuote(cmd), nil
	}
	return cmd, nil
}

// CommandDebug renders argv as an oracle-style debug echo: backticks around
// the command with the ` 2>&1` capture suffix (zelta-common CAPTURE_OUTPUT).
func CommandDebug(ep endpoint.Endpoint, argv []string) string {
	sh, err := CommandShell(ep.String(), argv)
	if err != nil {
		return "`" + SoftJoin(argv) + "`"
	}
	return "`" + sh + "  2>&1`"
}

// ShellJoin quotes argv for display/ssh (exported for backup dry-run of plain cmds).
func ShellJoin(args []string) string { return shellJoin(args) }

// quote sparingly for local dry-run lines (only when needed).
func SoftJoin(args []string) string {
	parts := make([]string, len(args))
	for i, a := range args {
		if a == "" || strings.ContainsAny(a, " \t'\"\\") {
			parts[i] = shellSingleQuote(a)
		} else {
			parts[i] = a
		}
	}
	return strings.Join(parts, " ")
}
