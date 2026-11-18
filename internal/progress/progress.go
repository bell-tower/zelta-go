// Package progress tracks replication streams in flight so a status
// request (SIGINFO from Ctrl+T on BSD and macOS terminals, or SIGUSR1) can
// describe them.
// All methods are safe on a nil *Board, which tracks nothing.
package progress

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"zelta.space/internal/config"
	"zelta.space/internal/units"
)

// Board holds the streams currently being sent.
type Board struct {
	// TTY reports that stderr is a terminal, which turns on sender
	// progress reports (zfs send -v) when SEND_PROGRESS is auto.
	TTY bool
	// Now returns the current time; nil uses time.Now.
	Now func() time.Time

	mu     sync.Mutex
	next   int
	active map[int]*stream
}

type stream struct {
	name        string
	size, bytes int64
	start       time.Time
	snap        string // snapshot the bytes count belongs to
	done        int64  // bytes of earlier snapshots in the stream

}

func (b *Board) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Enabled reports whether senders should report progress, given the
// SEND_PROGRESS option: unset or "auto" follows TTY, otherwise its truth
// value. A nil board is never enabled.
func (b *Board) Enabled(sendProgress string) bool {
	if b == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(sendProgress)) {
	case "", "auto":
		return b.TTY
	}
	return config.Truthy(sendProgress)
}

// Start records a stream and returns its handle.
func (b *Board) Start(name string) int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.active == nil {
		b.active = map[int]*stream{}
	}
	b.next++
	b.active[b.next] = &stream{name: name, start: b.now()}
	return b.next
}

func (b *Board) update(id int, f func(*stream)) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if s := b.active[id]; s != nil {
		f(s)
	}
}

// Size records a stream's estimated size in bytes.
func (b *Board) Size(id int, n int64) { b.update(id, func(s *stream) { s.size += n }) }

// Sent records that a stream has sent n bytes of snap. zfs send counts each
// snapshot of an intermediate (-I) stream from zero, so a new snap carries
// the previous count forward.
func (b *Board) Sent(id int, snap string, n int64) {
	b.update(id, func(s *stream) {
		if s.snap != "" && snap != s.snap {
			s.done = s.bytes
		}
		s.snap, s.bytes = snap, s.done+n
	})
}

// Done forgets a stream.
func (b *Board) Done(id int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.active, id)
}

// Write prints one status line per stream in flight.
func (b *Board) Write(w io.Writer) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.active) == 0 {
		fmt.Fprintln(w, "zelta: no streams in flight")
		return
	}
	now := b.now()
	for _, id := range slices.Sorted(maps.Keys(b.active)) {
		s := b.active[id]
		line := s.name + ": "
		switch {
		case s.bytes > 0 && s.size > 0:
			line += fmt.Sprintf("%s of %s sent (%d%%)", human(s.bytes), human(s.size), min(100, s.bytes*100/s.size))
		case s.bytes > 0:
			line += human(s.bytes) + " sent"
		case s.size > 0:
			line += "sending " + human(s.size)
		default:
			line += "starting"
		}
		line += ", " + now.Sub(s.start).Round(time.Second).String() + " elapsed"
		fmt.Fprintln(w, line)
	}
}

func human(n int64) string { return units.HumanBytes(float64(n)) }
