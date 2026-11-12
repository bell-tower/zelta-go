package zelta

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/backup"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/failover"
	"github.com/bell-tower/zelta-go/internal/logging"
	"github.com/bell-tower/zelta-go/internal/version"
)

func init() {
	if failover.Backup == nil {
		failover.Backup = backup.Run
	}
}

// LevelTrace is the slog level of Zelta's debug messages, which include
// every command Zelta runs. Notices are logged at slog.LevelInfo and
// verbose (info) messages at slog.LevelDebug.
const LevelTrace = slog.LevelDebug - 4

// Version returns the Zelta version.
func Version() string { return version.Version }

// Client runs Zelta operations. A Client is safe for concurrent use; each
// operation resolves its own options.
type Client struct {
	log      *slog.Logger
	env      bool
	settings map[string]string
	exec     execx.Executor
	now      func() time.Time
}

// Option configures a Client.
type Option func(*Client) error

// New returns a client. Without options it uses Zelta's built-in defaults
// only, ignoring ZELTA_* environment variables and zelta.env, and discards
// log messages.
func New(opts ...Option) (*Client, error) {
	c := &Client{settings: map[string]string{}, exec: execx.OS{}, now: time.Now}
	for _, o := range opts {
		if err := o(c); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// WithLogger sends Zelta's messages to l: errors at slog.LevelError,
// warnings at slog.LevelWarn, notices at slog.LevelInfo, verbose messages
// at slog.LevelDebug, and debug messages (such as each command run) at
// LevelTrace. Each record has an "op" attribute naming the operation.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) error {
		c.log = l
		return nil
	}
}

// WithEnvironment makes the client resolve options the way the zelta
// command does on this host: from ZELTA_* environment variables and
// zelta.env (ZELTA_ENV, by default in ZELTA_ETC) as well as the defaults.
// Settings given to the client and to each operation still take precedence.
func WithEnvironment() Option {
	return func(c *Client) error {
		c.env = true
		return nil
	}
}

// WithSettings sets Zelta options by name, as in zelta.env and the
// zelta-options(7) manual: for example "RECV_TOP" or "SEND_DEFAULT". The
// ZELTA_ prefix is optional. Options that control logging and command
// dispatch are managed by the client and cannot be set.
func WithSettings(settings map[string]string) Option {
	return func(c *Client) error {
		return mergeSettings(c.settings, settings)
	}
}

// WithRemoteCommand sets the remote shell used to reach remote endpoints
// (REMOTE_COMMAND, by default "ssh").
func WithRemoteCommand(cmd string) Option {
	return WithSettings(map[string]string{"REMOTE_COMMAND": cmd})
}

// withExecutor replaces the command executor (for tests).
func withExecutor(e execx.Executor) Option {
	return func(c *Client) error {
		c.exec = e
		return nil
	}
}

// withClock replaces the clock (for tests).
func withClock(now func() time.Time) Option {
	return func(c *Client) error {
		c.now = now
		return nil
	}
}

// managedKeys are options the client controls.
var managedKeys = map[string]bool{
	"VERB": true, "USAGE": true, "OPERANDS": true, "LOG_LEVEL": true, "LOG_MODE": true,
	"LOG_FILE": true, "JSON_PRETTY": true, "MATCH_PIPE": true, "PRUNE_VISUAL": true,
}

func mergeSettings(dst, src map[string]string) error {
	for k, v := range src {
		key := strings.TrimPrefix(k, config.EnvPrefix)
		if !validKey(key) {
			return fmt.Errorf("zelta: invalid option name %q", k)
		}
		if managedKeys[key] {
			return fmt.Errorf("zelta: option %s is managed by the client", key)
		}
		dst[key] = v
	}
	return nil
}

func validKey(k string) bool {
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		return false
	}
	for _, r := range k {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

// level is the Zelta log level matching the logger. Errors and warnings
// are always delivered so operations can report them.
func (c *Client) level() int {
	if c.log == nil {
		return int(logging.Warning)
	}
	ctx := context.Background()
	switch {
	case c.log.Enabled(ctx, LevelTrace):
		return int(logging.Debug)
	case c.log.Enabled(ctx, slog.LevelDebug):
		return int(logging.Info)
	case c.log.Enabled(ctx, slog.LevelInfo):
		return int(logging.Notice)
	}
	return int(logging.Warning)
}

func slogLevel(l logging.Level) slog.Level {
	switch l {
	case logging.Error:
		return slog.LevelError
	case logging.Warning:
		return slog.LevelWarn
	case logging.Notice:
		return slog.LevelInfo
	case logging.Info:
		return slog.LevelDebug
	}
	return LevelTrace
}

// op is one running operation.
type op struct {
	c    *Client
	ctx  context.Context
	verb string
	rt   *app.Runtime

	loader *config.Loader

	mu       sync.Mutex
	errs     []string
	messages []string
	closer   io.Closer
}

// start resolves the options of an operation and builds its runtime.
// Options are applied in this order: defaults, environment (WithEnvironment
// only), client settings, then the operation's settings.
func (c *Client) start(ctx context.Context, verb string, settings map[string]string) (*op, error) {
	l := config.NewLoader()
	if !c.env {
		l.Env = map[string]string{"HOME": l.Env["HOME"]}
		l.SkipEnvFile = true
	}
	l.Clock = c.now
	l.Overrides = maps.Clone(c.settings)
	if err := mergeSettings(l.Overrides, settings); err != nil {
		return nil, err
	}
	l.Overrides["LOG_LEVEL"] = fmt.Sprint(c.level())
	l.Overrides["LOG_MODE"] = ""
	l.Overrides["LOG_FILE"] = ""
	opts, warnings, _ := l.Load(ctx, verb, nil)

	o := &op{c: c, ctx: ctx, verb: verb, loader: l}
	base := &app.Runtime{
		Opts: opts, Exec: c.exec, Stdin: strings.NewReader(""),
		Stdout: &lineWriter{emit: func(s string) { o.handle(logging.Notice, s) }},
		Stderr: &lineWriter{emit: func(s string) { o.handle(logging.Warning, s) }},
		Now:    c.now, Hostname: l.Hostname, User: l.User,
		TTY:        func() (io.ReadWriteCloser, error) { return nil, errors.New("no terminal") },
		LogHandler: o.handle,
	}
	if opts.Bool("REMOTE_MULTIPLEX") && !opts.Bool("DRYRUN") {
		mux := &execx.SSHMux{Inner: c.exec}
		base.Exec, o.closer = mux, mux
	}
	o.rt, _ = app.Build(base, opts)
	for _, w := range warnings {
		o.rt.Log.Warnf("%s", w)
	}
	return o, nil
}

// handle receives every message of the operation, including those of
// nested runs.
func (o *op) handle(level logging.Level, msg string) {
	if level <= logging.Warning {
		o.mu.Lock()
		o.messages = append(o.messages, msg)
		if level == logging.Error {
			o.errs = append(o.errs, msg)
		}
		o.mu.Unlock()
	}
	if l := o.c.log; l != nil {
		l.Log(o.ctx, slogLevel(level), msg, "op", o.verb)
	}
}

// finish releases the operation's resources and converts err.
func (o *op) finish(err error) error {
	if o.closer != nil {
		o.closer.Close()
	}
	if err == nil {
		return nil
	}
	e := &Error{Op: o.verb, Code: app.Code(err), err: err}
	var ee *app.ExitError
	var ue *app.UsageError
	switch {
	case errors.As(err, &ee):
		e.Message = ee.Msg
	case errors.As(err, &ue):
		e.Message = ue.Msg
	default:
		e.Message = err.Error()
	}
	if e.Message != "" {
		o.rt.Log.Errorf("%s", e.Message)
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if e.Message == "" && len(o.errs) > 0 {
		e.Message = o.errs[len(o.errs)-1]
	}
	e.Messages = slices.Clone(o.messages)
	if ctxErr := o.ctx.Err(); ctxErr != nil {
		e.err = ctxErr
	}
	return e
}

// lineWriter emits complete lines written to it.
type lineWriter struct {
	mu   sync.Mutex
	buf  []byte
	emit func(string)
}

func (w *lineWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := strings.TrimRight(string(w.buf[:i]), "\r")
		w.buf = w.buf[i+1:]
		if line != "" {
			w.emit(line)
		}
	}
	return len(p), nil
}

// Error is returned when an operation fails. Code is the exit status the
// zelta command would have returned for the same failure.
type Error struct {
	// Op is the operation, such as "backup".
	Op string
	// Code is the zelta exit status: usually 1, or 2 when a backup
	// finished with sync errors.
	Code int
	// Message describes the failure.
	Message string
	// Messages are the errors and warnings logged by the operation, in
	// order (the "errorMessages" of zelta's JSON output).
	Messages []string

	err error
}

func (e *Error) Error() string {
	msg := e.Message
	if msg == "" {
		msg = fmt.Sprintf("exit status %d", e.Code)
	}
	return "zelta " + e.Op + ": " + msg
}

// Unwrap returns the context error when the operation was canceled.
func (e *Error) Unwrap() error {
	if errors.Is(e.err, context.Canceled) || errors.Is(e.err, context.DeadlineExceeded) {
		return e.err
	}
	return nil
}
