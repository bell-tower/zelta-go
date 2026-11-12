// Package logging implements Zelta's leveled output.
//
// Levels are 0 (errors), 1 (warnings), 2 (notices, the default), 3 (info),
// and 4 (debug). In the default and "text" modes, notices are written plainly
// to standard output and info messages plainly to standard error; everything
// else goes to standard error with a "level: " prefix. In "json" mode, or
// when a log file is used, every message is prefixed and written to the
// error stream (or the file). Warnings and errors are also collected so they
// can be reported in JSON output.
package logging

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
)

// Level is a message severity.
type Level int

// Message levels.
const (
	Error   Level = 0
	Warning Level = 1
	Notice  Level = 2
	Info    Level = 3
	Debug   Level = 4
)

// Prefix returns the label used when a message is written with a prefix.
func (l Level) Prefix() string {
	switch l {
	case Warning:
		return "warning: "
	case Notice:
		return "notice: "
	case Info:
		return "info: "
	case Debug:
		return "debug: "
	default:
		return "error: "
	}
}

// Config configures a Logger.
type Config struct {
	// Level is the maximum level written (LOG_LEVEL).
	Level int
	// Mode is the LOG_MODE: "", "text", or "json".
	Mode string
	// Prefix is prepended to each message (LOG_PREFIX). Ignored in text mode.
	Prefix string
	// File, when set, receives all messages (LOG_FILE). Callers should leave
	// it empty in json mode, where the file receives the JSON document.
	File string
	// Stdout and Stderr default to the process streams.
	Stdout, Stderr io.Writer
	// Handler, when set, receives every message at or below Level instead
	// of the streams (used by the public Go API to forward to slog).
	Handler func(Level, string)
}

type sink struct {
	mu       sync.Mutex
	out, err io.Writer
	term     bool
	closer   io.Closer
	handler  func(Level, string)
}

// Logger writes leveled messages. It is safe for concurrent use; derived
// loggers share the underlying streams.
type Logger struct {
	sink   *sink
	level  int
	prefix string
	mu     sync.Mutex
	msgs   []string
}

// New creates a logger. It fails only when the log file cannot be opened.
func New(cfg Config) (*Logger, error) {
	s := &sink{out: cfg.Stdout, err: cfg.Stderr, handler: cfg.Handler}
	if s.out == nil {
		s.out = os.Stdout
	}
	if s.err == nil {
		s.err = os.Stderr
	}
	prefix := cfg.Prefix
	if cfg.File != "" {
		f, err := os.OpenFile(cfg.File, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o644)
		if err != nil {
			return nil, err
		}
		s.out, s.err, s.closer = f, f, f
	} else {
		switch cfg.Mode {
		case "":
			s.term = true
		case "text":
			s.term = true
			prefix = ""
		}
	}
	return &Logger{sink: s, level: cfg.Level, prefix: prefix}, nil
}

// Discard returns a logger that writes nothing.
func Discard() *Logger {
	l, _ := New(Config{Level: -100, Stdout: io.Discard, Stderr: io.Discard})
	return l
}

// Close releases a log file, if one was opened.
func (l *Logger) Close() error {
	if l.sink.closer != nil {
		return l.sink.closer.Close()
	}
	return nil
}

// With returns a logger sharing l's streams with a different message prefix
// and its own collected messages.
func (l *Logger) With(prefix string) *Logger {
	return &Logger{sink: l.sink, level: l.level, prefix: prefix}
}

// WithLevel returns a logger sharing l's streams with a different level.
func (l *Logger) WithLevel(level int) *Logger {
	return &Logger{sink: l.sink, level: level, prefix: l.prefix}
}

// Level returns the configured maximum level.
func (l *Logger) Level() int { return l.level }

// Enabled reports whether messages at level would be written.
func (l *Logger) Enabled(level Level) bool { return int(level) <= l.level }

// Log writes a message at the given level.
func (l *Logger) Log(level Level, msg string) {
	if level <= Warning {
		l.mu.Lock()
		l.msgs = append(l.msgs, msg)
		l.mu.Unlock()
	}
	if int(level) > l.level {
		return
	}
	if msg == "" {
		msg = "missing log message"
	} else {
		msg = l.prefix + msg
	}
	s := l.sink
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.handler != nil {
		s.handler(level, msg)
		return
	}
	switch {
	case s.term && level == Notice:
		fmt.Fprintln(s.out, msg)
	case s.term && level == Info:
		fmt.Fprintln(s.err, msg)
	default:
		fmt.Fprintln(s.err, level.Prefix()+msg)
	}
}

// Errorf logs an error.
func (l *Logger) Errorf(format string, a ...any) { l.Log(Error, sprintf(format, a)) }

// Warnf logs a warning.
func (l *Logger) Warnf(format string, a ...any) { l.Log(Warning, sprintf(format, a)) }

// Noticef logs a notice.
func (l *Logger) Noticef(format string, a ...any) { l.Log(Notice, sprintf(format, a)) }

// Infof logs an informational message.
func (l *Logger) Infof(format string, a ...any) { l.Log(Info, sprintf(format, a)) }

// Debugf logs a debug message.
func (l *Logger) Debugf(format string, a ...any) { l.Log(Debug, sprintf(format, a)) }

func sprintf(format string, a []any) string {
	if len(a) == 0 {
		return format
	}
	return fmt.Sprintf(format, a...)
}

// Messages returns the warnings and errors logged so far, regardless of
// level, in order.
func (l *Logger) Messages() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.msgs...)
}

// Stdout returns the stream notices are written to in terminal modes. Use
// it for output that must bypass leveled logging (e.g. visual prune output).
func (l *Logger) Stdout() io.Writer { return lockedWriter{l.sink, false} }

// Stderr returns the error stream.
func (l *Logger) Stderr() io.Writer { return lockedWriter{l.sink, true} }

type lockedWriter struct {
	s   *sink
	err bool
}

func (w lockedWriter) Write(p []byte) (int, error) {
	w.s.mu.Lock()
	defer w.s.mu.Unlock()
	if w.err {
		return w.s.err.Write(p)
	}
	return w.s.out.Write(p)
}

// Parse decodes an "ipc-log" record: a level and message separated by the
// Awk SUBSEP character (\034). Records without a numeric level are notices.
func Parse(record string) (Level, string) {
	lvl, msg, found := strings.Cut(record, "\x1c")
	if !found {
		return Notice, record
	}
	if lvl == "" {
		return Notice, msg
	}
	n := 0
	for _, c := range lvl {
		if c < '0' || c > '9' {
			return Notice, strings.TrimSpace(lvl + " " + msg)
		}
		n = n*10 + int(c-'0')
	}
	return Level(n), msg
}
