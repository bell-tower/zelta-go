package main

import (
	"os"
	"unicode/utf8"

	"golang.org/x/term"
)

// screen is the terminal in raw mode on the alternate screen.
type screen struct {
	in, out *os.File
	saved   *term.State
}

func openScreen() (*screen, error) {
	saved, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return nil, err
	}
	s := &screen{in: os.Stdin, out: os.Stdout, saved: saved}
	s.out.WriteString("\x1b[?1049h\x1b[?25l\x1b[?7l")
	return s, nil
}

func (s *screen) close() {
	s.out.WriteString("\x1b[?7h\x1b[?25h\x1b[?1049l")
	term.Restore(int(s.in.Fd()), s.saved)
}

func (s *screen) size() (w, h int) {
	w, h, err := term.GetSize(int(s.out.Fd()))
	if err != nil || w < 20 || h < 10 {
		return 80, 24
	}
	return w, h
}

// draw replaces the screen with lines in one write, so it doesn't flicker.
func (s *screen) draw(lines []string) {
	buf := []byte("\x1b[H")
	for i, l := range lines {
		if i > 0 {
			buf = append(buf, "\r\n"...)
		}
		// Clear first: with autowrap off, clearing after a full-width
		// line would erase its last character.
		buf = append(buf, "\x1b[2K"...)
		buf = append(buf, l...)
		buf = append(buf, "\x1b[0m"...)
	}
	buf = append(buf, "\x1b[J"...)
	s.out.Write(buf)
}

// Keys other than printable text.
const (
	kUp        = "up"
	kDown      = "down"
	kLeft      = "left"
	kRight     = "right"
	kTab       = "tab"
	kBackTab   = "backtab"
	kPgUp      = "pgup"
	kPgDn      = "pgdn"
	kEnter     = "enter"
	kBack      = "backspace"
	kClear     = "ctrl-u"
	kEsc       = "esc"
	kInterrupt = "ctrl-c"
)

var sequences = map[string]string{
	"\x1b[A": kUp, "\x1b[B": kDown, "\x1b[C": kRight, "\x1b[D": kLeft,
	"\x1bOA": kUp, "\x1bOB": kDown, "\x1bOC": kRight, "\x1bOD": kLeft,
	"\x1b[Z": kBackTab, "\x1b[5~": kPgUp, "\x1b[6~": kPgDn,
}

// keys reads key presses until the terminal closes.
func (s *screen) keys(out chan<- string) {
	buf := make([]byte, 256)
	for {
		n, err := s.in.Read(buf)
		if err != nil {
			close(out)
			return
		}
		for _, k := range parseKeys(buf[:n]) {
			out <- k
		}
	}
}

// parseKeys splits one read into keys. A lone ESC is the Esc key; unknown
// escape sequences are dropped.
func parseKeys(b []byte) []string {
	var out []string
	for len(b) > 0 {
		switch c := b[0]; {
		case c == 0x1b:
			if len(b) == 1 {
				return append(out, kEsc)
			}
			end := 2
			if b[1] == '[' || b[1] == 'O' {
				for end < len(b) && (b[end] < 0x40 || b[end] > 0x7e) {
					end++
				}
				end = min(end+1, len(b))
			}
			if k, ok := sequences[string(b[:end])]; ok {
				out = append(out, k)
			}
			b = b[end:]
			continue
		case c == '\t':
			out = append(out, kTab)
		case c == '\r' || c == '\n':
			out = append(out, kEnter)
		case c == 0x7f || c == 0x08:
			out = append(out, kBack)
		case c == 0x15:
			out = append(out, kClear)
		case c == 0x03 || c == 0x04:
			out = append(out, kInterrupt)
		case c < 0x20:
		default:
			r, n := utf8.DecodeRune(b)
			if r != utf8.RuneError {
				out = append(out, string(r))
			}
			b = b[n:]
			continue
		}
		b = b[1:]
	}
	return out
}
