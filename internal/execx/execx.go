// Package execx runs commands built by package command. The OS executor
// starts processes directly (connecting pipeline stages with OS pipes) and
// reports output line by line; the Fake executor scripts responses for tests.
package execx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"zelta.space/internal/command"
)

// Stream identifies where an output line came from.
type Stream uint8

const (
	Stdout Stream = iota + 1
	Stderr
)

// Line is one line of command output.
type Line struct {
	Stream Stream
	Text   string
}

// Result is the outcome of running a command or pipeline.
type Result struct {
	// Lines holds captured output in arrival order. Pipe only fills it when
	// no line callback is given.
	Lines []Line
	// Code is the exit status of the last stage (shell semantics); 127 when
	// a program could not be started.
	Code int
	// Codes holds the exit status of every stage.
	Codes []int
	// Err is set when execution was interrupted (for example by context
	// cancellation), could not be set up, or its output could not be read.
	Err error
}

// OK reports whether every stage exited successfully.
func (r Result) OK() bool {
	return r.Err == nil && r.FirstFailure() == 0
}

// FirstFailure returns the first nonzero stage status, or 0.
func (r Result) FirstFailure() int {
	for _, c := range r.Codes {
		if c != 0 {
			return c
		}
	}
	return r.Code
}

// Stdout returns the captured standard output lines.
func (r Result) Stdout() []string { return r.filter(Stdout) }

// Stderr returns the captured standard error lines.
func (r Result) Stderr() []string { return r.filter(Stderr) }

// Combined returns all captured lines in order.
func (r Result) Combined() []string { return r.filter(0) }

func (r Result) filter(s Stream) []string {
	var out []string
	for _, l := range r.Lines {
		if s == 0 || l.Stream == s {
			out = append(out, l.Text)
		}
	}
	return out
}

// Executor runs commands.
type Executor interface {
	// Run executes c to completion and captures its output.
	Run(ctx context.Context, c command.Cmd) Result
	// Pipe executes the stages of p connected by pipes. When onLine is
	// non-nil it receives each output line as it arrives (from any stage's
	// stderr and the last stage's stdout); otherwise lines are captured.
	Pipe(ctx context.Context, p command.Pipeline, onLine func(Line)) Result
}

// OS executes commands on the local machine.
type OS struct {
	// Env, if non-nil, replaces the environment of started processes.
	Env []string
	// Stdin, if non-nil, is connected to the first stage's input. When nil
	// the first stage reads from the null device.
	Stdin *os.File
}

// drainGrace bounds how long output readers may outlive their processes, in
// case a background grandchild (an ssh ControlMaster, say) inherited a pipe.
var drainGrace = 3 * time.Second

// Run implements Executor.
func (o OS) Run(ctx context.Context, c command.Cmd) Result {
	return o.Pipe(ctx, command.Pipeline{c}, nil)
}

// Pipe implements Executor.
func (o OS) Pipe(ctx context.Context, p command.Pipeline, onLine func(Line)) Result {
	res := Result{Codes: make([]int, len(p))}
	if len(p) == 0 {
		return res
	}
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		readers []*os.File
		procs   []*exec.Cmd
		stdin   = o.Stdin
		readErr error // output that could not be read in full
	)
	emit := func(l Line) {
		mu.Lock()
		defer mu.Unlock()
		if onLine != nil {
			onLine(l)
		} else {
			res.Lines = append(res.Lines, l)
		}
	}
	scan := func(r *os.File, s Stream) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		for sc.Scan() {
			emit(Line{s, sc.Text()})
		}
		// Closing r after drainGrace is not a failure; anything else lost output.
		if err := sc.Err(); err != nil && !errors.Is(err, os.ErrClosed) {
			err = fmt.Errorf("command output could not be read: %w", err)
			emit(Line{Stderr, "zelta: " + err.Error()})
			mu.Lock()
			readErr = errors.Join(readErr, err)
			mu.Unlock()
		}
		_, _ = io.Copy(io.Discard, r)
	}
	watch := func(r *os.File, s Stream) {
		readers = append(readers, r)
		wg.Add(1)
		go scan(r, s)
	}

	for i, c := range p {
		argv := c.Argv()
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.Env = o.Env
		if stdin != nil {
			cmd.Stdin = stdin
		}
		errR, errW, err := os.Pipe()
		if err != nil {
			res.Err = err
			break
		}
		outR, outW, err := os.Pipe()
		if err != nil {
			errR.Close()
			errW.Close()
			res.Err = err
			break
		}
		cmd.Stderr, cmd.Stdout = errW, outW
		if err := cmd.Start(); err != nil {
			res.Codes[i] = 127
			emit(Line{Stderr, "zelta: " + argv[0] + ": command not found"})
			errR.Close()
			cmd = nil
		} else {
			watch(errR, Stderr)
		}
		errW.Close()
		outW.Close()
		if stdin != nil && stdin != o.Stdin {
			stdin.Close()
		}
		if i == len(p)-1 {
			watch(outR, Stdout)
		} else {
			stdin = outR
		}
		procs = append(procs, cmd)
	}
	if res.Err != nil && stdin != nil && stdin != o.Stdin {
		stdin.Close()
	}
	for i, cmd := range procs {
		if cmd != nil {
			res.Codes[i] = exitCode(cmd.Wait())
		}
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(drainGrace):
		for _, r := range readers {
			r.Close()
		}
		<-done
	}
	if res.Err == nil && ctx.Err() != nil {
		res.Err = ctx.Err()
	}
	if res.Err == nil && readErr != nil {
		res.Err = readErr
	}
	res.Code = res.Codes[len(res.Codes)-1]
	return res
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := errors.AsType[*exec.ExitError](err); ok {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		return ee.ExitCode()
	}
	return 1
}
