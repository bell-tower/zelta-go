package execx

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/bell-tower/zelta-go/internal/command"
)

func TestOSRun(t *testing.T) {
	ctx := context.Background()
	r := OS{}.Run(ctx, command.Script("echo out; echo err >&2; exit 3"))
	if r.Code != 3 || r.OK() || r.FirstFailure() != 3 {
		t.Fatalf("code = %d ok=%v", r.Code, r.OK())
	}
	if !reflect.DeepEqual(r.Stdout(), []string{"out"}) || !reflect.DeepEqual(r.Stderr(), []string{"err"}) {
		t.Errorf("lines = %#v", r.Lines)
	}
	if len(r.Combined()) != 2 {
		t.Errorf("combined = %#v", r.Combined())
	}
	r = OS{}.Run(ctx, command.New(command.Words("printf", "%s|", "a b", "c")...))
	if !r.OK() || !reflect.DeepEqual(r.Stdout(), []string{"a b|c|"}) {
		t.Errorf("argv run = %#v", r)
	}
}

func TestOSRunMissingBinary(t *testing.T) {
	r := OS{}.Run(context.Background(), command.New(command.Word("/nonexistent/zelta-test-binary")))
	if r.Code != 127 || r.OK() {
		t.Fatalf("code = %d", r.Code)
	}
	if !strings.Contains(strings.Join(r.Stderr(), "\n"), "command not found") {
		t.Errorf("stderr = %#v", r.Stderr())
	}
}

func TestOSPipe(t *testing.T) {
	p := command.Pipeline{
		command.Script("printf 'a\\nb\\nc\\n'; echo s1 >&2"),
		command.Script("tr a-z A-Z"),
		command.Script("cat; echo s3 >&2"),
	}
	var lines []Line
	r := OS{}.Pipe(context.Background(), p, func(l Line) { lines = append(lines, l) })
	if !r.OK() || len(r.Codes) != 3 {
		t.Fatalf("result = %#v", r)
	}
	var out, errs []string
	for _, l := range lines {
		if l.Stream == Stdout {
			out = append(out, l.Text)
		} else {
			errs = append(errs, l.Text)
		}
	}
	if !reflect.DeepEqual(out, []string{"A", "B", "C"}) {
		t.Errorf("stdout = %#v", out)
	}
	if len(errs) != 2 {
		t.Errorf("stderr = %#v", errs)
	}
}

func TestOSPipeFailures(t *testing.T) {
	ctx := context.Background()
	// A failing middle stage is reported even though the last stage succeeds.
	r := OS{}.Pipe(ctx, command.Pipeline{command.Script("echo x"), command.Script("exit 4"), command.Script("cat")}, nil)
	if r.Code != 0 || r.OK() || r.FirstFailure() != 4 {
		t.Errorf("codes = %#v", r.Codes)
	}
	// A missing middle program is reported as 127 and does not hang.
	r = OS{}.Pipe(ctx, command.Pipeline{command.Script("yes | head -1000"), command.New(command.Word("/nonexistent/x")), command.Script("cat")}, nil)
	if r.Codes[1] != 127 {
		t.Errorf("codes = %#v", r.Codes)
	}
	if r := (OS{}).Pipe(ctx, nil, nil); len(r.Codes) != 0 {
		t.Errorf("empty pipeline = %#v", r)
	}
}

func TestOSCancel(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	r := OS{}.Run(ctx, command.Script("sleep 10"))
	if r.Err == nil || r.OK() {
		t.Errorf("expected cancellation, got %#v", r)
	}
	if time.Since(start) > 5*time.Second {
		t.Errorf("cancellation took too long")
	}
}

func TestOSDrainGrace(t *testing.T) {
	old := drainGrace
	drainGrace = 200 * time.Millisecond
	defer func() { drainGrace = old }()
	start := time.Now()
	// The background sleep inherits stderr and would otherwise block reading.
	r := OS{}.Run(context.Background(), command.Script("(sleep 5 &) ; echo done"))
	if !r.OK() || !reflect.DeepEqual(r.Stdout(), []string{"done"}) {
		t.Errorf("result = %#v", r)
	}
	if time.Since(start) > 3*time.Second {
		t.Errorf("drain grace not honored")
	}
}

func TestOSEnv(t *testing.T) {
	r := OS{Env: []string{"ZELTA_TEST_VAR=hello"}}.Run(context.Background(), command.Script("echo $ZELTA_TEST_VAR"))
	if !reflect.DeepEqual(r.Stdout(), []string{"hello"}) {
		t.Errorf("env = %#v", r.Stdout())
	}
}

func TestFake(t *testing.T) {
	f := NewFake().
		Once("zfs list 'a'", Text("first\n")).
		On("zfs list 'a'", Out("again")).
		OnPrefix("zfs get", Fail(1, "cannot open")).
		OnContains("rename", Out("renamed")).
		OnRegexp(`^zfs snapshot .*@x'$`, Out()).
		OnFunc(func(s string) bool { return strings.HasPrefix(s, "echo") }, func(s string) Result { return Out(strings.TrimPrefix(s, "echo ")) })
	ctx := context.Background()
	list := command.New(command.Word("zfs"), command.Word("list"), command.Name("a"))
	if got := f.Run(ctx, list).Stdout(); !reflect.DeepEqual(got, []string{"first"}) {
		t.Errorf("once = %#v", got)
	}
	if got := f.Run(ctx, list).Stdout(); !reflect.DeepEqual(got, []string{"again"}) {
		t.Errorf("on = %#v", got)
	}
	if r := f.Run(ctx, command.New(command.Words("zfs", "get", "all")...)); r.Code != 1 || r.Stderr()[0] != "cannot open" {
		t.Errorf("prefix = %#v", r)
	}
	if r := f.Run(ctx, command.New(command.Words("zfs", "rename")...)); r.Stdout()[0] != "renamed" {
		t.Errorf("contains = %#v", r)
	}
	if r := f.Run(ctx, command.New(command.Word("zfs"), command.Word("snapshot"), command.Name("p@x"))); !r.OK() {
		t.Errorf("regexp = %#v", r)
	}
	if r := f.Run(ctx, command.New(command.Words("echo", "hi")...)); r.Stdout()[0] != "hi" {
		t.Errorf("func = %#v", r)
	}
	if r := f.Run(ctx, command.New(command.Word("unknown"))); r.Code != 127 || len(f.Unmatched) != 1 {
		t.Errorf("unmatched = %#v %v", r, f.Unmatched)
	}
	var got []string
	r := f.Pipe(ctx, command.Pipeline{command.New(command.Words("echo", "piped")...)}, func(l Line) { got = append(got, l.Text) })
	if !r.OK() || !reflect.DeepEqual(got, []string{"piped"}) || r.Lines != nil {
		t.Errorf("pipe = %#v %#v", r, got)
	}
	if len(f.Calls) != 8 || len(f.CallsMatching("zfs list")) != 2 {
		t.Errorf("calls = %#v", f.Calls)
	}
	if r := Fail(2).WithStderr("x"); r.Stderr()[0] != "x" || r.Code != 2 {
		t.Errorf("WithStderr = %#v", r)
	}
	if r := Text(""); len(r.Lines) != 0 || !r.OK() {
		t.Errorf("Text empty = %#v", r)
	}
}

func TestOSLineTooLong(t *testing.T) {
	// Output beyond the 16 MiB line limit must fail the run, not truncate it.
	r := OS{}.Run(context.Background(), command.Script("head -c 17000000 /dev/zero | tr '\\0' x; echo; echo after"))
	if r.OK() || r.Err == nil || !strings.Contains(strings.Join(r.Stderr(), "\n"), "zelta: command output could not be read") {
		t.Errorf("code=%d err=%v stderr=%q", r.Code, r.Err, r.Stderr())
	}
}
