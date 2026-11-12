// Package shellwords splits and quotes POSIX shell words without performing
// any expansion. Zelta options such as SEND_DEFAULT="-L -c -e" or
// REMOTE_COMMAND="ssh -p 2202" are shell fragments; Split turns them into
// argv words and Quote renders words back into shell text.
package shellwords

import (
	"fmt"
	"strings"
)

// Split breaks s into words using POSIX shell quoting rules: whitespace
// separates words, single quotes preserve everything literally, double
// quotes allow backslash escapes of $ ` " \ and newline, and an unquoted
// backslash escapes the next character. No expansion is performed.
func Split(s string) ([]string, error) {
	var (
		words   []string
		cur     strings.Builder
		inWord  bool
		quote   byte
		escaped bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case escaped:
			escaped = false
			if quote == '"' && !strings.ContainsRune("$`\"\\\n", rune(c)) {
				cur.WriteByte('\\')
			}
			if !(quote == 0 && c == '\n') {
				cur.WriteByte(c)
			}
			inWord = true
		case quote == '\'':
			if c == '\'' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\\':
			escaped = true
			inWord = true
		case quote == '"':
			if c == '"' {
				quote = 0
			} else {
				cur.WriteByte(c)
			}
		case c == '\'' || c == '"':
			quote = c
			inWord = true
		case c == ' ' || c == '\t' || c == '\n':
			if inWord {
				words = append(words, cur.String())
				cur.Reset()
				inWord = false
			}
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	if escaped && quote == 0 {
		return nil, fmt.Errorf("trailing backslash in %q", s)
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated %c quote in %q", quote, s)
	}
	if inWord {
		words = append(words, cur.String())
	}
	return words, nil
}

// MustSplit is Split for trusted input; malformed input falls back to
// whitespace splitting so a bad option never panics.
func MustSplit(s string) []string {
	w, err := Split(s)
	if err != nil {
		return strings.Fields(s)
	}
	return w
}

// IsSafe reports whether s can appear in shell text without quoting.
func IsSafe(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !safeByte(s[i]) {
			return false
		}
	}
	return true
}

func safeByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("_-+=:,./@#%^", c) >= 0
}

// Quote returns s quoted for a POSIX shell, leaving safe words bare.
func Quote(s string) string {
	if IsSafe(s) {
		return s
	}
	return SingleQuote(s)
}

// SingleQuote always wraps s in single quotes, escaping embedded quotes.
func SingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// DoubleQuote wraps s in double quotes, escaping only what a POSIX shell
// would otherwise interpret inside them.
func DoubleQuote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '"', '$', '`':
			b.WriteByte('\\')
		case '\\':
			if i+1 == len(s) || strings.IndexByte("$`\"\\\n", s[i+1]) >= 0 {
				b.WriteByte('\\')
			}
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

// Join quotes each word and joins them with spaces.
func Join(words []string) string {
	q := make([]string, len(words))
	for i, w := range words {
		q[i] = Quote(w)
	}
	return strings.Join(q, " ")
}
