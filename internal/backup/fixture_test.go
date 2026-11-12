package backup

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/execx"
)

// fx scripts a fake ZFS environment for one command.
type fx struct {
	t  *testing.T
	tr *app.TestRuntime
}

func newFx(t *testing.T, verb string, opts map[string]string) *fx {
	t.Helper()
	return &fx{t: t, tr: app.NewTest(verb, opts)}
}

func getCmd(prefix, ds string, depth string) string {
	d := ""
	if depth != "" {
		d = " -d" + depth
	}
	return prefix + "zfs get -Hpr -s local,none -t filesystem,volume -o name,property,value" + d + " " + plannedProperties + " '" + ds + "'"
}

func listCmd(prefix, ds, props, depth string) string {
	d := ""
	if depth != "" {
		d = " -d" + depth
	}
	return prefix + "zfs list -Hpr -o " + props + " -t all -Screatetxg" + d + " '" + ds + "'"
}

// props registers "zfs get" output. Each row is "suffix prop value".
func (f *fx) props(ds string, rows ...string) *fx { return f.propsOn("", ds, "", rows...) }

func (f *fx) propsOn(prefix, ds, depth string, rows ...string) *fx {
	var lines []string
	for _, row := range rows {
		parts := strings.SplitN(row, " ", 3)
		if i := strings.LastIndex(row, " "); i > 0 {
			if j := strings.LastIndex(row[:i], " "); j >= 0 {
				parts = []string{row[:j], row[j+1 : i], row[i+1:]}
			}
		}
		suffix := parts[0]
		if suffix == "." {
			suffix = ""
		}
		lines = append(lines, ds+suffix+"\t"+parts[1]+"\t"+parts[2])
	}
	f.tr.Fake.On(getCmd(prefix, ds, depth), execx.Out(lines...))
	return f
}

// missing registers a nonexistent dataset for "zfs get".
func (f *fx) missing(ds string) *fx { return f.missingOn("", ds, "") }

func (f *fx) missingOn(prefix, ds, depth string) *fx {
	f.tr.Fake.On(getCmd(prefix, ds, depth), execx.Fail(1, "cannot open '"+ds+"': dataset does not exist"))
	return f
}

// list registers the match listing. Rows are "suffix guid", newest first.
func (f *fx) list(ds string, rows ...string) *fx { return f.listOn("", ds, "name,guid", "", rows...) }

func (f *fx) listOn(prefix, ds, props, depth string, rows ...string) *fx {
	var lines []string
	for _, row := range rows {
		suffix, rest := row, ""
		for n := strings.Count(props, ","); n > 0; n-- {
			i := strings.LastIndex(suffix, " ")
			suffix, rest = suffix[:i], strings.TrimSpace(suffix[i+1:]+" "+rest)
		}
		if suffix == "." {
			suffix = ""
		}
		lines = append(lines, ds+suffix+"\t"+strings.ReplaceAll(rest, " ", "\t"))
	}
	f.tr.Fake.On(listCmd(prefix, ds, props, depth), execx.Out(lines...))
	return f
}

// on registers any command.
func (f *fx) on(cmd string, res execx.Result) *fx {
	f.tr.Fake.On(cmd, res)
	return f
}

// received is the output of a successful send/receive of n streams.
func received(kind string, size int, n int) execx.Result {
	lines := []string{fmt.Sprintf("size\t%d", size)}
	for range n {
		lines = append(lines, "receiving "+kind+" stream of x@s into y@s", "received 1K stream in 0.5 seconds (2K/sec)")
	}
	return execx.Out(lines...)
}

func (f *fx) run(fn func(context.Context, *app.Runtime, []string) error, args ...string) error {
	f.t.Helper()
	return fn(context.Background(), f.tr.Runtime, args)
}

func (f *fx) backup(args ...string) error { return f.run(Run, args...) }

func (f *fx) mutations() []string {
	var out []string
	for _, c := range f.tr.Fake.Calls {
		if strings.Contains(c, "zfs get ") || strings.Contains(c, "zfs list ") {
			continue
		}
		out = append(out, c)
	}
	return out
}

func (f *fx) noUnmatched() {
	f.t.Helper()
	if len(f.tr.Fake.Unmatched) > 0 {
		f.t.Fatalf("unmatched commands:\n%s\nlog:\n%s%s", strings.Join(f.tr.Fake.Unmatched, "\n"), f.tr.Out.String(), f.tr.Err.String())
	}
}

func eqLines(t *testing.T, what, got string, want ...string) {
	t.Helper()
	w := strings.Join(want, "\n")
	if len(want) > 0 {
		w += "\n"
	}
	if got != w {
		t.Errorf("%s:\n--- got ---\n%s--- want ---\n%s", what, got, w)
	}
}

func eqSlice(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("%s:\n--- got ---\n%s\n--- want ---\n%s", what, strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func contains(t *testing.T, what, got string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if !strings.Contains(got, s) {
			t.Errorf("%s does not contain %q:\n%s", what, s, got)
		}
	}
}

func notContains(t *testing.T, what, got string, subs ...string) {
	t.Helper()
	for _, s := range subs {
		if strings.Contains(got, s) {
			t.Errorf("%s unexpectedly contains %q:\n%s", what, s, got)
		}
	}
}

func exitCode(err error) int { return app.Code(err) }

// finish converts a command error the way the CLI does and returns the
// exit status.
func finish(f *fx, err error) int { return app.Finish(f.tr.Runtime, err) }
