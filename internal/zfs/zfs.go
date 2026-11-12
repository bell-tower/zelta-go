// Package zfs builds zfs(8) commands for endpoints and classifies the
// feedback that zfs, ssh, and the shell print.
package zfs

import (
	"regexp"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/logging"
)

// Remote command kinds, selecting REMOTE_DEFAULT, REMOTE_SEND, or REMOTE_RECV.
const (
	RemoteDefault = "DEFAULT"
	RemoteSend    = "SEND"
	RemoteRecv    = "RECV"
)

// Command returns "zfs args..." to run on ep's host through the remote shell
// selected by kind.
func Command(rt *app.Runtime, ep endpoint.Endpoint, kind string, args ...command.Arg) command.Cmd {
	return command.New(append([]command.Arg{command.Word("zfs")}, args...)...).On(rt.Remote(ep, kind))
}

// Local returns "zfs args..." without a remote.
func Local(args ...command.Arg) command.Cmd {
	return command.New(append([]command.Arg{command.Word("zfs")}, args...)...)
}

var (
	// ConnectionError matches ssh and shell failures that make further
	// commands on an endpoint pointless.
	ConnectionError = regexp.MustCompile(`[Nn]o route to host|[Cc]ould not resolve|[Cc]ommand not found|[Cc]onnection (closed|refused)|[Nn]etwork.*unreachable|timed out|Permission denied.*publickey|[Hh]ost key verification failed`)
	debugFeedback   = regexp.MustCompile(`Permanently added.*to the list`)
)

// Classify returns the log level for a line of unexpected command output:
// connection failures are errors, ssh host-key notes are debug output, and
// anything else is informational.
func Classify(line string) logging.Level {
	switch {
	case line == "":
		return logging.Notice
	case ConnectionError.MatchString(line):
		return logging.Error
	case debugFeedback.MatchString(line):
		return logging.Debug
	default:
		return logging.Info
	}
}

// Feedback logs a line of unexpected command output at its classified level
// and returns that level. Blank lines are ignored.
func Feedback(log *logging.Logger, line string) logging.Level {
	lvl := Classify(line)
	if line != "" {
		log.Log(lvl, line)
	}
	return lvl
}

// Fields splits a tab-separated zfs output line.
func Fields(line string) []string { return strings.Split(line, "\t") }

// DoesNotExist reports whether a line is zfs's missing-dataset error.
func DoesNotExist(line string) bool { return strings.Contains(line, "dataset does not exist") }

// Child reports whether name is root or a descendant of root, and returns
// the suffix relative to root ("" for root itself, "/child" for children).
func Child(root, name string) (string, bool) {
	if name == root {
		return "", true
	}
	if strings.HasPrefix(name, root) && len(name) > len(root) && strings.ContainsRune("/@#", rune(name[len(root)])) {
		return name[len(root):], true
	}
	return "", false
}
