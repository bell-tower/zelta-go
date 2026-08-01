package zlog

import (
	"fmt"
	"io"
	"os"
)

// Levels match the oracle LOG_* constants (zelta-common.awk).
const (
	Error   Level = 0
	Warning Level = 1
	Notice  Level = 2
	Info    Level = 3
	Debug   Level = 4
)

// Level is a log severity 0-4. Messages above Sink.max are dropped.
type Level int

// prefix returns the oracle `zelta_log_prefixed` label for the level.
func (l Level) prefix() string {
	switch l {
	case Warning:
		return "warning: "
	case Notice:
		return "notice: "
	case Info:
		return "info: "
	case Debug:
		return "debug: "
	}
	return "error: "
}

// Sink filters and formats leveled messages like `zelta ipc-log`.
//
// Emit is not goroutine-safe; the CLI and libraries emit sequentially.
type Sink struct {
	max    Level
	mode   string // "", "text", "json"
	prefix string // ZELTA_LOG_PREFIX; cleared by mode "text"
	file   *os.File
}

// New builds a sink from oracle option values. maxLevel is LOG_LEVEL,
// mode is LOG_MODE, prefix is ZELTA_LOG_PREFIX, and logFile is LOG_FILE
// ("" → stderr routing; non-empty → append to the file, all levels prefixed).
func New(maxLevel Level, mode, prefix, logFile string) (*Sink, error) {
	s := &Sink{
		max:    maxLevel,
		mode:   mode,
		prefix: prefix,
	}
	// Oracle quirk: LOG_MODE=text unsets ZELTA_LOG_PREFIX.
	if mode == "text" {
		s.prefix = ""
	}
	if logFile != "" {
		f, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			return nil, err
		}
		s.file = f
	}
	return s, nil
}

// Close releases the log file (no-op when logging to stderr).
func (s *Sink) Close() {
	if s.file != nil {
		s.file.Close()
		s.file = nil
	}
}

// Limit returns a shallow copy that drops messages above max (oracle
// ipc-run children pin --log-level; backup's inner match runs at notice).
func (s *Sink) Limit(max Level) *Sink {
	if s == nil {
		return nil
	}
	c := *s
	c.max = max
	return &c
}

// Enabled reports whether messages at level would be emitted.
func (s *Sink) Enabled(level Level) bool {
	return s != nil && level <= s.max
}

// Emit filters and routes one leveled message.
func (s *Sink) Emit(level Level, msg string) {
	if !s.Enabled(level) {
		return
	}
	if msg == "" {
		// Oracle ipc-log: empty message → "missing log message".
		msg = "missing log message"
	}
	line := s.prefix + msg
	if s.file != nil {
		fmt.Fprintf(s.file, "%s%s\n", level.prefix(), line)
		return
	}
	if s.mode == "" || s.mode == "text" {
		switch level {
		case Notice:
			// "Notice" is regular stdout on a term.
			fmt.Fprintf(os.Stdout, "%s\n", line)
			return
		case Info:
			// "Info" is regular stderr on a term (no prefix).
			fmt.Fprintf(os.Stderr, "%s\n", line)
			return
		}
	}
	fmt.Fprintf(s.out(), "%s%s\n", level.prefix(), line)
}

// out returns the current stderr (referenced at emit time so tests can
// redirect os.Stderr after New).
func (s *Sink) out() io.Writer {
	return os.Stderr
}

// Convenience wrappers for the five levels.
func (s *Sink) Error(msg string)   { s.Emit(Error, msg) }
func (s *Sink) Warning(msg string) { s.Emit(Warning, msg) }
func (s *Sink) Notice(msg string)  { s.Emit(Notice, msg) }
func (s *Sink) Info(msg string)    { s.Emit(Info, msg) }
func (s *Sink) Debug(msg string)   { s.Emit(Debug, msg) }
