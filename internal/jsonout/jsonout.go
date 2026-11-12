// Package jsonout writes Zelta's JSON summary documents.
//
// The format matches what Zelta has always produced: a single-line object
// whose members are written as `"key": value` separated by commas, with an
// "output_version" header, followed by optional "sentStreams" and
// "errorMessages" arrays. Empty values are omitted, carriage returns are
// removed, and strings that look like numbers are written as numbers.
package jsonout

import (
	"fmt"
	"io"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/bell-tower/zelta-go/internal/units"
)

// Output format version reported in "output_version".
const (
	VersMajor = 1
	VersMinor = 1
)

// FieldOrder is the canonical order of summary fields.
var FieldOrder = []string{
	"startTime", "endTime", "runTime",
	"sourceUser", "sourceHost", "sourceDataset", "sourceSnapshot", "sourceEndpoint", "sourceListTime", "sourceWritten",
	"targetUser", "targetHost", "targetDataset", "targetSnapshot", "targetEndpoint", "targetListTime",
	"targetsCloned", "targetsResumed",
	"replicationSize", "replicationStreamsSent", "replicationStreamsReceived", "replicationErrorCode", "replicationTime",
}

// Document is a JSON summary.
type Document struct {
	// Command is reported as output_version.command, e.g. "zelta backup".
	Command string
	fields  map[string]string
	extra   []string
	// SentStreams lists replicated stream descriptions.
	SentStreams []string
	// ErrorMessages lists warnings and errors.
	ErrorMessages []string
}

// New returns an empty document for command.
func New(command string) *Document { return &Document{Command: command, fields: map[string]string{}} }

// Set records a field. Strings, integers and floats are accepted; empty
// strings remove the field.
func (d *Document) Set(key string, v any) {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case int:
		s = strconv.Itoa(x)
	case int64:
		s = strconv.FormatInt(x, 10)
	case float64:
		s = units.FormatNumber(x)
	default:
		s = fmt.Sprint(x)
	}
	s = strings.ReplaceAll(s, "\r", "")
	if s == "" {
		delete(d.fields, key)
		return
	}
	if _, ok := d.fields[key]; !ok && !known(key) {
		d.extra = append(d.extra, key)
	}
	d.fields[key] = s
}

// Get returns a field's rendered value.
func (d *Document) Get(key string) string { return d.fields[key] }

func known(key string) bool {
	return slices.Contains(FieldOrder, key)
}

var numberRe = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

func value(s string) string {
	if numberRe.MatchString(s) {
		return s
	}
	return String(s)
}

// String encodes s as a JSON string, dropping carriage returns.
func String(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\r':
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

// tokens renders the document as the token stream Zelta's writer used.
func (d *Document) tokens() []string {
	t := []string{"{", `"output_version": {`,
		`"command": ` + String(d.Command),
		fmt.Sprintf(`"vers_major": %d`, VersMajor),
		fmt.Sprintf(`"vers_minor": %d`, VersMinor),
		"}"}
	for _, k := range append(append([]string{}, FieldOrder...), d.extra...) {
		if v, ok := d.fields[k]; ok {
			t = append(t, String(k)+": "+value(v))
		}
	}
	for _, arr := range []struct {
		name string
		vals []string
	}{{"sentStreams", d.SentStreams}, {"errorMessages", d.ErrorMessages}} {
		if len(arr.vals) == 0 {
			continue
		}
		t = append(t, String(arr.name)+": [")
		for _, v := range arr.vals {
			t = append(t, value(strings.ReplaceAll(v, "\r", "")))
		}
		t = append(t, "]")
	}
	return append(t, "}")
}

// Write writes the document followed by a newline. With pretty set, each
// token is written on its own indented line.
func (d *Document) Write(w io.Writer, pretty bool) error {
	t := d.tokens()
	var b strings.Builder
	depth := 0
	for i, tok := range t {
		if strings.HasPrefix(tok, "]") || strings.HasPrefix(tok, "}") {
			depth--
		}
		if pretty {
			b.WriteString(strings.Repeat("  ", depth))
		}
		b.WriteString(tok)
		if i+1 < len(t) {
			next := t[i+1]
			opens := strings.HasSuffix(tok, "{") || strings.HasSuffix(tok, "[")
			closes := strings.HasPrefix(next, "]") || strings.HasPrefix(next, "}")
			if !opens && !closes {
				b.WriteByte(',')
			}
		}
		if pretty {
			b.WriteByte('\n')
		}
		if strings.HasSuffix(tok, "{") || strings.HasSuffix(tok, "[") {
			depth++
		}
	}
	if !pretty {
		b.WriteByte('\n')
	}
	_, err := io.WriteString(w, b.String())
	return err
}
