package execx

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"zelta.space/internal/command"
)

// Out returns a successful result with the given stdout lines.
func Out(lines ...string) Result {
	r := Result{Codes: []int{0}}
	for _, l := range lines {
		r.Lines = append(r.Lines, Line{Stdout, l})
	}
	return r
}

// Fail returns a result with the given exit code and stderr lines.
func Fail(code int, lines ...string) Result {
	r := Result{Code: code, Codes: []int{code}}
	for _, l := range lines {
		r.Lines = append(r.Lines, Line{Stderr, l})
	}
	return r
}

// Text splits multi-line text into a successful stdout result. A trailing
// newline is ignored.
func Text(s string) Result {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return Out()
	}
	return Out(strings.Split(s, "\n")...)
}

// WithStderr appends stderr lines to r.
func (r Result) WithStderr(lines ...string) Result {
	for _, l := range lines {
		r.Lines = append(r.Lines, Line{Stderr, l})
	}
	return r
}

type rule struct {
	match   func(string) bool
	respond func(string) Result
	times   int // remaining uses; <0 means unlimited
}

// Fake is a scriptable Executor. Commands are matched by their rendered
// String() form against rules in registration order; the first rule with
// remaining uses wins, so register Once rules before an On rule for the same
// command. Every executed command is recorded in Calls.
type Fake struct {
	mu    sync.Mutex
	rules []*rule
	// Calls lists rendered commands in execution order.
	Calls []string
	// Strict makes unmatched commands fail the test via Unmatched.
	Unmatched []string
}

// NewFake returns an empty fake executor.
func NewFake() *Fake { return &Fake{} }

func (f *Fake) add(r *rule) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rules = append(f.rules, r)
	return f
}

// On responds to an exact command string.
func (f *Fake) On(cmd string, res Result) *Fake {
	return f.add(&rule{match: func(s string) bool { return s == cmd }, respond: func(string) Result { return res }, times: -1})
}

// Once responds to an exact command string a single time.
func (f *Fake) Once(cmd string, res Result) *Fake {
	return f.add(&rule{match: func(s string) bool { return s == cmd }, respond: func(string) Result { return res }, times: 1})
}

// OnPrefix responds to commands starting with prefix.
func (f *Fake) OnPrefix(prefix string, res Result) *Fake {
	return f.add(&rule{match: func(s string) bool { return strings.HasPrefix(s, prefix) }, respond: func(string) Result { return res }, times: -1})
}

// OnContains responds to commands containing substr.
func (f *Fake) OnContains(substr string, res Result) *Fake {
	return f.add(&rule{match: func(s string) bool { return strings.Contains(s, substr) }, respond: func(string) Result { return res }, times: -1})
}

// OnRegexp responds to commands matching the regular expression.
func (f *Fake) OnRegexp(expr string, res Result) *Fake {
	re := regexp.MustCompile(expr)
	return f.add(&rule{match: re.MatchString, respond: func(string) Result { return res }, times: -1})
}

// OnFunc responds with fn for commands where match returns true.
func (f *Fake) OnFunc(match func(string) bool, fn func(string) Result) *Fake {
	return f.add(&rule{match: match, respond: fn, times: -1})
}

func (f *Fake) respond(s string) Result {
	f.mu.Lock()
	f.Calls = append(f.Calls, s)
	var hit *rule
	for _, r := range f.rules {
		if r.times != 0 && r.match(s) {
			hit = r
			if r.times > 0 {
				r.times--
			}
			break
		}
	}
	if hit == nil {
		f.Unmatched = append(f.Unmatched, s)
	}
	f.mu.Unlock()
	if hit == nil {
		return Fail(127, fmt.Sprintf("fake: no rule for: %s", s))
	}
	res := hit.respond(s)
	if res.Codes == nil {
		res.Codes = []int{res.Code}
	}
	return res
}

// Run implements Executor.
func (f *Fake) Run(_ context.Context, c command.Cmd) Result {
	return f.respond(c.String())
}

// Pipe implements Executor.
func (f *Fake) Pipe(_ context.Context, p command.Pipeline, onLine func(Line)) Result {
	res := f.respond(p.String())
	if onLine != nil {
		for _, l := range res.Lines {
			onLine(l)
		}
		res.Lines = nil
	}
	return res
}

// CallsMatching returns recorded calls containing substr.
func (f *Fake) CallsMatching(substr string) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.Calls {
		if strings.Contains(c, substr) {
			out = append(out, c)
		}
	}
	return out
}
