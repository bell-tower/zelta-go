// Package app holds the runtime shared by every Zelta command: resolved
// options, logging, command execution, standard streams, and the clock.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/endpoint"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/jsonout"
	"github.com/bell-tower/zelta-go/internal/logging"
	"github.com/bell-tower/zelta-go/internal/progress"
)

// Runtime is the environment a command runs in.
type Runtime struct {
	// Opts holds the resolved options (keys without the ZELTA_ prefix).
	Opts *config.Options
	// Log writes leveled messages.
	Log *logging.Logger
	// Exec runs external commands.
	Exec execx.Executor
	// Stdin, Stdout and Stderr are the process streams for output that
	// bypasses leveled logging (JSON documents, visual prune output,
	// interactive prompts).
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Now returns the current time.
	Now func() time.Time
	// Hostname and User describe the local machine.
	Hostname, User string
	// TTY opens the controlling terminal for prompts; nil uses /dev/tty.
	TTY func() (io.ReadWriteCloser, error)
	// JSON collects the summary document in json log mode; nil otherwise.
	JSON *jsonout.Document
	// LogHandler, when set, receives log messages instead of the streams,
	// including those of loggers Build creates for nested runs.
	LogHandler func(logging.Level, string)
	// Progress tracks streams in flight for status requests; nil tracks
	// nothing.
	Progress *progress.Board
}

// Endpoint parses an endpoint string for this runtime.
func (rt *Runtime) Endpoint(id string) endpoint.Endpoint {
	host := rt.Opts.Get("HOSTNAME")
	if host == "" {
		host = rt.Hostname
	}
	return endpoint.Parse(id, host, rt.User)
}

// Remote returns the remote shell for ep using the remote command option of
// the given kind: "DEFAULT", "SEND", or "RECV".
func (rt *Runtime) Remote(ep endpoint.Endpoint, kind string) *command.Remote {
	if kind == "" {
		kind = "DEFAULT"
	}
	return ep.RemoteFor(rt.Opts.Get("REMOTE_" + kind))
}

// Unix returns the current time in seconds.
func (rt *Runtime) Unix() int64 { return rt.Now().Unix() }

// DryRun reports whether commands should only be displayed.
func (rt *Runtime) DryRun() bool { return rt.Opts.Bool("DRYRUN") }

// Run executes c, logging it at debug level first.
func (rt *Runtime) Run(ctx context.Context, c command.Cmd) execx.Result {
	rt.Log.Debugf("`%s`", c.String())
	return rt.Exec.Run(ctx, c)
}

// ExitError carries a process exit status. Messages should already have been
// logged; Msg, when set, is logged by the caller that converts the error.
type ExitError struct {
	Code int
	Msg  string
}

func (e *ExitError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return fmt.Sprintf("exit status %d", e.Code)
}

// Exit returns an ExitError with code and an optional message.
func Exit(code int, format string, a ...any) error {
	msg := format
	if len(a) > 0 {
		msg = fmt.Sprintf(format, a...)
	}
	return &ExitError{Code: code, Msg: msg}
}

// Fail returns an exit-status-1 error with a message to be logged.
func Fail(format string, a ...any) error { return Exit(1, format, a...) }

// UsageError asks the caller to print the command's usage and exit 1.
type UsageError struct{ Msg string }

func (e *UsageError) Error() string { return e.Msg }

// Usage returns a UsageError.
func Usage(format string, a ...any) error {
	if len(a) > 0 {
		format = fmt.Sprintf(format, a...)
	}
	return &UsageError{Msg: format}
}

// Code maps an error to a process exit status.
func Code(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[*ExitError](err); ok {
		return ee.Code
	}
	return 1
}

// SanitizeLine strips carriage returns from command output.
func SanitizeLine(s string) string { return strings.ReplaceAll(s, "\r", "") }

// DefaultTTY opens /dev/tty.
func DefaultTTY() (io.ReadWriteCloser, error) { return os.OpenFile("/dev/tty", os.O_RDWR, 0) }

// OpenTTY opens the controlling terminal.
func (rt *Runtime) OpenTTY() (io.ReadWriteCloser, error) {
	if rt.TTY != nil {
		return rt.TTY()
	}
	return DefaultTTY()
}

// Report records a JSON summary field when JSON output is enabled.
func (rt *Runtime) Report(key string, v any) {
	if rt.JSON != nil {
		rt.JSON.Set(key, v)
	}
}

// Build creates a runtime for opts that shares base's executor, streams,
// clock, and identity, with a logger configured from opts (LOG_LEVEL,
// LOG_MODE, LOG_PREFIX, LOG_FILE) and a JSON document in json mode.
func Build(base *Runtime, opts *config.Options) (*Runtime, error) {
	mode := opts.Get("LOG_MODE")
	file := opts.Get("LOG_FILE")
	if mode == "json" {
		file = "" // the JSON document goes to the log file instead
	}
	log, err := logging.New(logging.Config{
		Level: opts.Int("LOG_LEVEL"), Mode: mode, Prefix: opts.Get("LOG_PREFIX"), File: file,
		Stdout: base.Stdout, Stderr: base.Stderr, Handler: base.LogHandler,
	})
	if err != nil {
		log, _ = logging.New(logging.Config{Level: opts.Int("LOG_LEVEL"), Mode: mode, Stdout: base.Stdout, Stderr: base.Stderr, Handler: base.LogHandler})
		log.Errorf("cannot open log file '%s': %v", file, err)
	}
	rt := *base
	rt.Opts, rt.Log, rt.JSON = opts, log, nil
	if mode == "json" {
		rt.JSON = jsonout.New("zelta " + opts.Get("VERB"))
	}
	return &rt, nil
}

// Finish logs a fatal error message carried by err, writes the JSON
// document if enabled, closes the log, and returns the exit status.
func Finish(rt *Runtime, err error) int {
	var ee *ExitError
	if errors.As(err, &ee) && ee.Msg != "" {
		rt.Log.Errorf("%s", ee.Msg)
	} else if err != nil && !errors.As(err, &ee) {
		if _, ok := errors.AsType[*UsageError](err); !ok {
			rt.Log.Errorf("%s", err.Error())
		}
	}
	if rt.JSON != nil {
		rt.JSON.ErrorMessages = rt.Log.Messages()
		w := rt.Stdout
		if file := rt.Opts.Get("LOG_FILE"); file != "" {
			if f, ferr := os.OpenFile(file, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644); ferr == nil {
				defer f.Close()
				w = f
			} else {
				rt.Log.Errorf("cannot open log file '%s': %v", file, ferr)
			}
		}
		_ = rt.JSON.Write(w, rt.Opts.Bool("JSON_PRETTY"))
	}
	_ = rt.Log.Close()
	return Code(err)
}
