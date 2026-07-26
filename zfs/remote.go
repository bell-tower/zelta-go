package zfs

import (
	"fmt"
	"strconv"
	"strings"
)

// Role selects which remote wrapper to use (Awk REMOTE_DEFAULT / SEND / RECV).
type Role int

const (
	// RoleDefault is list, snapshot, and outer dual-remote shells (typically ssh -n).
	RoleDefault Role = iota
	// RoleSend is the send side of a replication pipe (typically ssh -n).
	RoleSend
	// RoleRecv is the recv side — stdin must stay open for the stream.
	RoleRecv
)

// Remote builds how a command runs on a remote host.
//
// Built-in implementations:
//   - SSHConfig — structured OpenSSH flags (Sylve and other strict-SSH callers)
//   - CommandRemote — raw prefix strings (Awk REMOTE_*; mbuffer, socat, …)
//
// Other backends (e.g. Go SSH libraries) can implement Remote later without
// changing backup/match callers.
type Remote interface {
	// Argv returns the local process argv that runs remoteCmd on host.
	// host is "user@host" or "host". remoteCmd is the remote command string
	// (already shell-joined zfs argv when produced by Real).
	Argv(host, remoteCmd string, role Role) ([]string, error)
	// Shell returns a shell snippet for nesting inside another remote pipeline
	// (dual-remote pull/push inner hop).
	Shell(host, remoteCmd string, role Role) (string, error)
}

// SSHConfig is structured OpenSSH configuration.
// Zero value uses bin "ssh", BatchMode=yes, ConnectTimeout=30.
type SSHConfig struct {
	// Bin is the ssh binary path; default "ssh".
	Bin string
	// Port is the remote port; passed as -p when non-zero.
	Port int
	// IdentityFile is the private key path; passed as -i.
	IdentityFile string
	// Options are extra -o key=value pairs (e.g. StrictHostKeyChecking=accept-new).
	Options []string
}

func (c SSHConfig) bin() string {
	if c.Bin != "" {
		return c.Bin
	}
	return "ssh"
}

func (c SSHConfig) flags() []string {
	args := []string{"-o", "BatchMode=yes", "-o", "ConnectTimeout=30"}
	if c.IdentityFile != "" {
		args = append(args, "-i", c.IdentityFile)
	}
	if c.Port > 0 {
		args = append(args, "-p", strconv.Itoa(c.Port))
	}
	for _, opt := range c.Options {
		args = append(args, "-o", opt)
	}
	return args
}

// Argv implements Remote with pure argv (no local shell).
func (c SSHConfig) Argv(host, remoteCmd string, role Role) ([]string, error) {
	if host == "" {
		return nil, fmt.Errorf("zfs remote: empty host")
	}
	args := []string{c.bin()}
	if role != RoleRecv {
		args = append(args, "-n")
	}
	args = append(args, c.flags()...)
	args = append(args, host, remoteCmd)
	return args, nil
}

// Shell implements Remote.
func (c SSHConfig) Shell(host, remoteCmd string, role Role) (string, error) {
	argv, err := c.Argv(host, remoteCmd, role)
	if err != nil {
		return "", err
	}
	return shellJoin(argv), nil
}

// CommandRemote is Awk-style REMOTE_* prefix strings.
//
// Use when the transport is not plain structured SSH — custom ssh lines,
// mbuffer, socat, viamillipede, kTLS wrappers, etc. Prefixes may contain
// shell metacharacters; Argv runs them via `sh -c`.
//
// Field mapping (upstream names):
//
//	Command → ZELTA_REMOTE_COMMAND (base, default "ssh")
//	Default → ZELTA_REMOTE_DEFAULT (empty → Command + " -n")
//	Send    → ZELTA_REMOTE_SEND    (empty → same as Default)
//	Recv    → ZELTA_REMOTE_RECV    (empty → Command)
type CommandRemote struct {
	Command string
	Default string
	Send    string
	Recv    string
}

func (c CommandRemote) prefix(role Role) string {
	switch role {
	case RoleRecv:
		if s := strings.TrimSpace(c.Recv); s != "" {
			return s
		}
		if s := strings.TrimSpace(c.Command); s != "" {
			return s
		}
		return "ssh"
	case RoleSend:
		if s := strings.TrimSpace(c.Send); s != "" {
			return s
		}
		return c.defaultPrefix()
	default:
		if s := strings.TrimSpace(c.Default); s != "" {
			return s
		}
		return c.defaultPrefix()
	}
}

func (c CommandRemote) defaultPrefix() string {
	if s := strings.TrimSpace(c.Default); s != "" {
		return s
	}
	base := strings.TrimSpace(c.Command)
	if base == "" {
		base = "ssh"
	}
	// Awk: REMOTE_DEFAULT="${REMOTE_COMMAND} -n"
	if hasSSHStdinNull(base) {
		return base
	}
	return base + " -n"
}

func hasSSHStdinNull(prefix string) bool {
	fields := strings.Fields(prefix)
	for _, f := range fields {
		if f == "-n" {
			return true
		}
	}
	return false
}

// Argv implements Remote via sh -c so pipelines in the prefix work.
func (c CommandRemote) Argv(host, remoteCmd string, role Role) ([]string, error) {
	line, err := c.Shell(host, remoteCmd, role)
	if err != nil {
		return nil, err
	}
	return []string{"sh", "-c", line}, nil
}

// Shell implements Remote: prefix + quoted host + quoted command.
func (c CommandRemote) Shell(host, remoteCmd string, role Role) (string, error) {
	if host == "" {
		return "", fmt.Errorf("zfs remote: empty host")
	}
	p := c.prefix(role)
	return p + " " + shellSingleQuote(host) + " " + shellSingleQuote(remoteCmd), nil
}
