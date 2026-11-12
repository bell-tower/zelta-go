// Package command models the external commands Zelta runs. A Cmd is either
// an argv invocation or a shell script, optionally executed on a remote host
// through a remote shell such as ssh. Commands render to the exact shell text
// shown in dry-run and debug output, and to the argv used for execution.
//
// Local commands are executed directly without a shell. Remote commands pass
// their words to the remote shell command (ssh joins its arguments with
// spaces), so each word is rendered for the remote shell. ZFS names are
// always quoted in displayed commands, matching Zelta's historical output.
package command

import (
	"strings"

	"github.com/bell-tower/zelta-go/internal/shellwords"
)

// Arg is a single command word.
type Arg struct {
	// Lit is literal text prepended to S, such as "origin=" or "-o".
	Lit string
	// S is the word's value.
	S string
	// Name marks S as a ZFS dataset, snapshot, or bookmark name.
	Name bool
}

// Word returns a plain argument.
func Word(s string) Arg { return Arg{S: s} }

// Name returns a ZFS name argument.
func Name(s string) Arg { return Arg{S: s, Name: true} }

// LitName returns a ZFS name argument with a literal prefix, e.g.
// LitName("origin=", "pool/ds@snap") renders as origin='pool/ds@snap'.
func LitName(lit, s string) Arg { return Arg{Lit: lit, S: s, Name: true} }

// Words converts plain strings into arguments.
func Words(ws ...string) []Arg {
	a := make([]Arg, len(ws))
	for i, w := range ws {
		a[i] = Word(w)
	}
	return a
}

// Fragment splits a user-supplied shell fragment (for example an option
// string like "-L -c -e") into plain arguments.
func Fragment(s string) []Arg {
	return Words(shellwords.MustSplit(s)...)
}

// Value returns the argument's argv value.
func (a Arg) Value() string { return a.Lit + a.S }

// local renders the word for a local shell.
func (a Arg) local() string {
	if a.Name {
		return a.Lit + shellwords.SingleQuote(a.S)
	}
	return shellwords.Quote(a.Value())
}

// remote renders the word as passed to the remote shell command. ZFS names
// only contain shell-safe characters and spaces, so spaces are escaped with
// backslashes, which keeps nested remote commands readable.
func (a Arg) remote() string {
	if a.Name {
		if zfsNameSafe(a.S) {
			return a.Lit + strings.ReplaceAll(a.S, " ", `\ `)
		}
		return a.Lit + shellwords.SingleQuote(a.S)
	}
	return shellwords.Quote(a.Value())
}

// remoteDisplay renders a remote word as it would be typed locally.
func (a Arg) remoteDisplay() string {
	w := a.remote()
	if a.Name && zfsNameSafe(a.S) {
		return a.Lit + "'" + strings.ReplaceAll(a.S, " ", `\ `) + "'"
	}
	return shellwords.Quote(w)
}

func zfsNameSafe(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case strings.IndexByte("_-:.@#/% +=", c) >= 0:
		default:
			return false
		}
	}
	return true
}

// Remote describes how to reach a remote host.
type Remote struct {
	// Prefix is the remote shell command, e.g. ["ssh", "-n"].
	Prefix []string
	// Host is the [user@]host destination.
	Host string
}

// NewRemote builds a Remote from a shell fragment like "ssh -n" and a host.
func NewRemote(prefix, host string) *Remote {
	return &Remote{Prefix: shellwords.MustSplit(prefix), Host: host}
}

func (r *Remote) prefixArgv() []string {
	argv := append([]string{}, r.Prefix...)
	return append(argv, r.Host)
}

func (r *Remote) display() string {
	return shellwords.Join(r.prefixArgv())
}

// Cmd is one command invocation.
type Cmd struct {
	// Remote runs the command on another host; nil runs it locally.
	Remote *Remote
	// Args is the argv form of the command.
	Args []Arg
	// Script, when set, is shell text run with "sh -c" locally or passed
	// verbatim to the remote shell. Args is ignored for scripts.
	Script string
}

// New returns a local argv command.
func New(args ...Arg) Cmd { return Cmd{Args: args} }

// Script returns a local shell-script command.
func Script(script string) Cmd { return Cmd{Script: script} }

// On returns a copy of c that runs on r (nil for local).
func (c Cmd) On(r *Remote) Cmd {
	c.Remote = r
	return c
}

// With returns a copy of c with args appended.
func (c Cmd) With(args ...Arg) Cmd {
	c.Args = append(append([]Arg{}, c.Args...), args...)
	return c
}

// IsZero reports whether the command is empty.
func (c Cmd) IsZero() bool { return len(c.Args) == 0 && c.Script == "" }

// Argv returns the argument vector used to execute the command.
func (c Cmd) Argv() []string {
	if c.Remote == nil {
		if c.Script != "" {
			return []string{"sh", "-c", c.Script}
		}
		argv := make([]string, len(c.Args))
		for i, a := range c.Args {
			argv[i] = a.Value()
		}
		return argv
	}
	argv := c.Remote.prefixArgv()
	if c.Script != "" {
		return append(argv, c.Script)
	}
	for _, a := range c.Args {
		argv = append(argv, a.remote())
	}
	return argv
}

// String renders the command as shell text.
func (c Cmd) String() string {
	var b strings.Builder
	if c.Remote != nil {
		b.WriteString(c.Remote.display())
		b.WriteByte(' ')
		if c.Script != "" {
			b.WriteString(shellwords.DoubleQuote(c.Script))
			return b.String()
		}
		for i, a := range c.Args {
			if i > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(a.remoteDisplay())
		}
		return b.String()
	}
	if c.Script != "" {
		return c.Script
	}
	for i, a := range c.Args {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(a.local())
	}
	return b.String()
}

// Pipeline is a sequence of commands whose stdout feeds the next stdin. Its
// stages run as separate local processes; any stage may itself be remote.
type Pipeline []Cmd

// String renders the pipeline as a brace group, the form Zelta has always
// printed for local send/receive pipelines.
func (p Pipeline) String() string {
	if len(p) == 0 {
		return ""
	}
	if len(p) == 1 {
		return p[0].String()
	}
	parts := make([]string, len(p)-1)
	for i, c := range p[1:] {
		parts[i] = c.String()
	}
	return "{ " + p[0].String() + "|" + strings.Join(parts, " | ") + " ; }"
}
