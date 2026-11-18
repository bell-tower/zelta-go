package cli

import (
	"bufio"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"zelta.space/internal/command"
	"zelta.space/internal/config"
	"zelta.space/internal/endpoint"
	"zelta.space/internal/execx"
	"zelta.space/internal/logging"
	"zelta.space/internal/shellwords"
)

// ipcLog implements "zelta ipc-log": it reads level/message records (the
// Awk SUBSEP separates them) from standard input and logs them using the
// LOG_LEVEL, LOG_MODE, LOG_PREFIX, and LOG_FILE settings of the environment.
func (a *App) ipcLog() int {
	env := a.NewLoader().EnvOptions()
	level := 2
	if v, err := strconv.Atoi(strings.TrimSpace(env["LOG_LEVEL"])); err == nil {
		level = v
	}
	log, err := logging.New(logging.Config{Level: level, Mode: env["LOG_MODE"], Prefix: env["LOG_PREFIX"], File: env["LOG_FILE"], Stdout: a.Stdout, Stderr: a.Stderr})
	if err != nil {
		fmt.Fprintf(a.Stderr, "error: %v\n", err)
		return 1
	}
	defer log.Close()
	sc := bufio.NewScanner(a.Stdin)
	sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for sc.Scan() {
		lvl, msg := logging.Parse(sc.Text())
		log.Log(lvl, msg)
	}
	if err := sc.Err(); err != nil {
		fmt.Fprintf(a.Stderr, "error: reading log records: %v\n", err)
		return 1
	}
	return 0
}

// ipcEnv implements "zelta ipc-env VERB [ARGS]": it prints the options
// resolved for VERB and ARGS as shell export statements, including the
// endpoint fields (SRC_*, TGT_*) of the first two operands.
func (a *App) ipcEnv(ctx context.Context, args []string) int {
	if len(args) == 0 {
		rootUsage(a.Stderr)
		return 1
	}
	v, ok := findVerb(args[0])
	if !ok {
		v = verb{name: args[0]}
	}
	cmd, p := a.verbCommand(v, new(int), false)
	flagArgs := preprocess(args[1:], v.optionVerb())
	if err := cmd.Flags().Parse(flagArgs); err != nil {
		fmt.Fprintf(a.Stderr, "error: %s\n", flagError(p, err))
		return 1
	}
	loader := a.NewLoader()
	opts, warnings, err := loader.Load(ctx, v.optionVerb(), nil)
	if err == nil {
		var cli map[string]string
		var w []string
		cli, w, err = config.ApplyOps(opts, p.ops)
		warnings = append(warnings, w...)
		opts.Apply(cli)
	}
	for _, w := range warnings {
		fmt.Fprintf(a.Stderr, "warning: %s\n", w)
	}
	if err != nil {
		fmt.Fprintf(a.Stderr, "error: %v\n", err)
		return 1
	}
	if opts.Bool("USAGE") {
		return 1
	}
	operands := cmd.Flags().Args()
	if len(operands) > 0 {
		opts.Set("OPERANDS", strings.Join(operands, "\x1c"), config.FromCLI)
	}
	for i, prefix := range []string{"SRC_", "TGT_"} {
		if i >= len(operands) {
			break
		}
		e := endpoint.Parse(operands[i], opts.Get("HOSTNAME"), loader.User)
		for k, val := range map[string]string{"ID": e.ID, "REMOTE": e.Remote, "USER": e.User, "HOST": e.Host, "DS": e.DS, "SNAP": e.Snap} {
			opts.Set(prefix+k, val, config.FromCLI)
		}
	}
	for _, k := range opts.Keys() {
		fmt.Fprintf(a.Stdout, "export %s%s=%s\n", config.EnvPrefix, k, setQuote(opts.Get(k)))
	}
	return 0
}

// timeCommand implements "zelta time CMD [ARGS]": run a command, copy its
// output, and report the elapsed time in the format of "time -p".
func (a *App) timeCommand(ctx context.Context, args []string) int {
	if len(args) == 0 {
		return 0
	}
	start := time.Now()
	res := a.Exec.Pipe(ctx, command.Pipeline{command.New(command.Words(args...)...)}, func(l execx.Line) {
		if l.Stream == execx.Stderr {
			fmt.Fprintln(a.Stderr, l.Text)
		} else {
			fmt.Fprintln(a.Stdout, l.Text)
		}
	})
	fmt.Fprintf(a.Stderr, "real %.2f\nuser 0.00\nsys 0.00\n", time.Since(start).Seconds())
	return res.Code
}

// setSafe holds the bytes FreeBSD sh leaves unquoted in "set" output.
const setSafe = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!%+,-./:@]^_{}"

// setQuote renders an ipc-env value as Zelta 1.2 did on FreeBSD, where it
// printed the shell's "set" output: bare when every byte is safe, otherwise
// (including when empty) single-quoted.
func setQuote(v string) string {
	if v != "" && strings.Trim(v, setSafe) == "" {
		return v
	}
	return shellwords.SingleQuote(v)
}
