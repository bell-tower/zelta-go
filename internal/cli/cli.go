// Package cli implements the zelta command line: argv[0] dispatch for the
// historical command names, the cobra command tree with flags generated
// from the option registry, help, completion, and the internal ipc verbs.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/progress"
	"github.com/bell-tower/zelta-go/internal/version"
)

// App is the zelta command-line application.
type App struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// Exec runs external commands.
	Exec execx.Executor
	// NewLoader returns the option loader for the current environment.
	NewLoader func() *config.Loader
	// Now returns the current time.
	Now func() time.Time
	// TTY opens the terminal for prompts.
	TTY func() (io.ReadWriteCloser, error)
	// Engines implement the subcommands.
	Engines map[string]Engine
	// Policy implements "zelta policy"; it receives the option loader, the
	// command-line option values, and the backup engine for its jobs.
	Policy PolicyEngine
	// Columns lists "zelta match" column names for completion.
	Columns func() []string
	// PolicyObjects lists policy sites, hosts, and datasets for completion.
	PolicyObjects func(path string) []string
	// Man displays a manual page; nil uses the system man(1).
	Man func(ctx context.Context, a *App, topic string) int
	// Multiplex wraps the executor to share one ssh connection per host
	// (REMOTE_MULTIPLEX); nil disables it. The result is closed after the
	// command when it implements io.Closer.
	Multiplex func(execx.Executor) execx.Executor
	// Progress tracks replication streams for status requests; nil
	// disables tracking.
	Progress *progress.Board
}

// PolicyEngine runs "zelta policy".
type PolicyEngine func(ctx context.Context, rt *app.Runtime, args []string, loader *config.Loader, cli map[string]string, backup Engine) error

// New returns an App wired to the process environment and the given
// engines. The caller (package main) supplies the implementations.
func New(engines map[string]Engine, pol PolicyEngine) *App {
	return &App{
		Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr,
		Exec:      execx.OS{},
		NewLoader: config.NewLoader,
		Now:       time.Now,
		TTY:       app.DefaultTTY,
		Engines:   engines,
		Policy:    pol,
	}
}

// Main runs the command line and returns the process exit status.
func (a *App) Main(ctx context.Context, argv []string) int {
	prog := filepath.Base(argv[0])
	args := argv[1:]
	if name, ok := legacyNames[prog]; ok {
		args = append([]string{name}, args...)
		if name == "zprune" {
			return a.execute(ctx, args)
		}
	}
	if len(args) == 0 {
		rootUsage(a.Stderr)
		return 1
	}
	switch args[0] {
	case "-V", "--version", "version":
		fmt.Fprintln(a.Stdout, version.String())
		return 0
	case "usage", "-?", "-h":
		if args[0] == "-h" {
			rootHelp(a.Stdout)
			return 0
		}
		rootUsage(a.Stderr)
		return 1
	case "--help":
		rootHelp(a.Stdout)
		return 0
	case "ipc-log":
		return a.ipcLog()
	case "ipc-env":
		return a.ipcEnv(ctx, args[1:])
	case "ipc-run":
		if len(args) < 2 {
			rootUsage(a.Stderr)
			return 1
		}
		return a.executeWith(ctx, args[1:], true)
	case "time":
		return a.timeCommand(ctx, args[1:])
	}
	return a.execute(ctx, args)
}

func (a *App) execute(ctx context.Context, args []string) int {
	return a.executeWith(ctx, args, false)
}

// executeWith runs args through the cobra tree. internal selects ipc-run
// semantics, where internal option keys are read from the environment.
func (a *App) executeWith(ctx context.Context, args []string, internal bool) int {
	status := 0
	root := a.rootCommand(&status, internal)
	if len(args) > 0 {
		if v, ok := findVerb(args[0]); ok {
			args = append([]string{args[0]}, preprocess(args[1:], v.optionVerb())...)
		}
	}
	root.SetArgs(args)
	if err := root.ExecuteContext(ctx); err != nil {
		if fe, ok := errors.AsType[*flagErr](err); ok {
			fmt.Fprintf(a.Stderr, "error: %s\n", fe.msg)
			verbUsage(a.Stderr, fe.verb, fe.prog)
			return 1
		}
		fmt.Fprintf(a.Stderr, "error: %v\n", err)
		return 1
	}
	return status
}

type flagErr struct {
	msg  string
	verb verb
	prog string
}

func (e *flagErr) Error() string { return e.msg }

func (a *App) rootCommand(status *int, internal bool) *cobra.Command {
	root := &cobra.Command{
		Use:           "zelta",
		Short:         "Safe, recursive ZFS replication, recovery, and retention",
		SilenceErrors: true,
		SilenceUsage:  true,
		Args:          cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			*status = a.unknownCommand(args)
			return nil
		},
	}
	root.FParseErrWhitelist.UnknownFlags = true
	root.Flags().BoolP("help", "h", false, "show help")
	root.SetHelpFunc(func(cmd *cobra.Command, _ []string) {
		if cmd == root {
			rootHelp(a.Stdout)
			return
		}
		if v, ok := findVerb(cmd.Name()); ok {
			verbHelp(a.Stdout, v, progName(cmd))
			return
		}
		fmt.Fprint(a.Stdout, cmd.UsageString())
	})
	root.CompletionOptions.HiddenDefaultCmd = false
	for _, v := range verbs {
		cmd, _ := a.verbCommand(v, status, internal)
		root.AddCommand(cmd)
	}
	root.SetHelpCommand(a.helpCommand(status))
	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Show version information",
		Args:  cobra.ArbitraryArgs,
		Run:   func(*cobra.Command, []string) { fmt.Fprintln(a.Stdout, version.String()) },
	})
	root.SetIn(a.Stdin)
	root.SetOut(a.Stdout)
	root.SetErr(a.Stderr)
	root.InitDefaultCompletionCmd()
	return root
}

func progName(cmd *cobra.Command) string {
	if cmd.Name() == "zprune" {
		return "zprune"
	}
	return "zelta " + cmd.Name()
}

func (a *App) unknownCommand(args []string) int {
	if len(args) == 0 {
		rootUsage(a.Stderr)
		return 1
	}
	fmt.Fprintf(a.Stderr, "unrecognized command: '%s'\n", args[0])
	if hint := suggestion(args[0]); hint != "" {
		fmt.Fprintf(a.Stderr, "did you mean '%s'?\n", hint)
	}
	rootUsage(a.Stderr)
	return 1
}

// suggestion returns a likely intended command for a mistyped one.
func suggestion(name string) string {
	switch name {
	case "list":
		return "zelta match"
	case "send":
		return "zelta backup"
	case "rollback":
		return "zelta revert"
	}
	best, bestDist := "", 3
	for _, v := range verbs {
		if v.hidden {
			continue
		}
		if d := levenshtein(name, v.name); d < bestDist {
			best, bestDist = "zelta "+v.name, d
		}
	}
	return best
}

func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

func (a *App) verbCommand(v verb, status *int, internal bool) (*cobra.Command, *parser) {
	p := &parser{}
	cmd := &cobra.Command{
		Use:    v.name + " " + v.usage,
		Short:  v.short,
		Long:   v.long,
		Hidden: v.hidden,
		Args:   cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if v.name == "zprune" {
				if vf, _ := cmd.Flags().GetBool("version"); vf {
					fmt.Fprintln(a.Stdout, version.Zprune())
					return nil
				}
			}
			*status = a.runVerb(cmd.Context(), v, p, args, internal)
			return nil
		},
	}
	registerFlags(cmd, v.optionVerb(), p)
	if v.name == "zprune" {
		cmd.Flags().BoolP("version", "V", false, "show version information")
	}
	cmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return &flagErr{msg: flagError(p, err), verb: v, prog: progName(c)}
	})
	a.registerCompletions(cmd, v)
	return cmd, p
}

// runVerb resolves options, builds the runtime, and runs the engine.
func (a *App) runVerb(ctx context.Context, v verb, p *parser, args []string, internal bool) int {
	loader := a.NewLoader()
	loader.Internal = internal
	if a.Now != nil {
		loader.Clock = a.Now
	}
	optVerb := v.name
	if v.name == "zprune" {
		optVerb = "prune"
	}
	opts, warnings, err := loader.Load(ctx, optVerb, nil)
	if err != nil {
		fmt.Fprintf(a.Stderr, "error: %v\n", err)
		return 1
	}
	cli, flagWarnings, err := config.ApplyOps(opts, p.ops)
	if err != nil {
		fmt.Fprintf(a.Stderr, "error: %v\n", err)
		verbUsage(a.Stderr, v, prog(v))
		return 1
	}
	opts.Apply(cli)
	if internal && len(args) == 0 && opts.Has("OPERANDS") {
		args = strings.Split(opts.Get("OPERANDS"), "\x1c")
	}
	base := &app.Runtime{
		Opts: opts, Exec: a.Exec, Stdin: a.Stdin, Stdout: a.Stdout, Stderr: a.Stderr,
		Now: a.Now, Hostname: loader.Hostname, User: loader.User, TTY: a.TTY,
		Progress: a.Progress,
	}
	if a.Multiplex != nil && opts.Bool("REMOTE_MULTIPLEX") && !opts.Bool("DRYRUN") {
		base.Exec = a.Multiplex(a.Exec)
		if c, ok := base.Exec.(io.Closer); ok {
			defer c.Close()
		}
	}
	if base.Now == nil {
		base.Now = time.Now
	}
	if cmd := opts.Get("SYSTIME"); cmd != "" {
		base.Now = sysTime(ctx, a.Exec, cmd, base.Now)
	}
	rt, _ := app.Build(base, opts)
	for _, w := range append(warnings, flagWarnings...) {
		rt.Log.Warnf("%s", w)
	}
	if opts.Bool("USAGE") {
		verbHelp(a.Stderr, v, prog(v))
		rt.JSON = nil
		return app.Finish(rt, &app.ExitError{Code: 1})
	}
	reportEndpoints(rt, args)

	var runErr error
	if v.name == "policy" {
		if a.Policy == nil {
			runErr = app.Fail("zelta policy: not available")
		} else {
			runErr = a.Policy(ctx, rt, args, loader, cli, a.Engines["backup"])
		}
	} else if engine := a.Engines[v.engineKey()]; engine != nil {
		runErr = engine(ctx, rt, args)
	} else {
		runErr = app.Fail("zelta %s: not available", v.name)
	}
	if ue, ok := errors.AsType[*app.UsageError](runErr); ok {
		if ue.Msg != "" {
			fmt.Fprintln(a.Stderr, ue.Msg)
		}
		verbUsage(a.Stderr, v, prog(v))
		rt.JSON = nil
		return app.Finish(rt, &app.ExitError{Code: 1})
	}
	return app.Finish(rt, runErr)
}

func prog(v verb) string {
	if v.name == "zprune" {
		return "zprune"
	}
	return "zelta " + v.name
}

// reportEndpoints records the source and target endpoint fields of the JSON
// summary from the first two operands, as Zelta always has.
func reportEndpoints(rt *app.Runtime, args []string) {
	if rt.JSON == nil {
		return
	}
	for i, prefix := range []string{"source", "target"} {
		if i >= len(args) {
			break
		}
		ep := rt.Endpoint(args[i])
		rt.Report(prefix+"Endpoint", ep.ID)
		rt.Report(prefix+"User", ep.User)
		rt.Report(prefix+"Host", ep.Host)
		rt.Report(prefix+"Dataset", ep.DS)
		rt.Report(prefix+"Snapshot", ep.Snap)
	}
}

func (a *App) helpCommand(status *int) *cobra.Command {
	return &cobra.Command{
		Use:   "help [TOPIC]",
		Short: "Show the manual for zelta or a TOPIC",
		Args:  cobra.MaximumNArgs(1),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return helpTopics(), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			topic := ""
			if len(args) > 0 {
				topic = args[0]
			}
			man := a.Man
			if man == nil {
				man = systemMan
			}
			*status = man(cmd.Context(), a, topic)
			return nil
		},
	}
}

func helpTopics() []string {
	topics := []string{"options"}
	for _, v := range verbs {
		if !slices.Contains(topics, helpTopic(v)) {
			topics = append(topics, helpTopic(v))
		}
	}
	return topics
}

// sysTime returns a clock that asks the SYSTIME command for the current Unix
// time on every call, falling back to fallback when it prints no number.
func sysTime(ctx context.Context, ex execx.Executor, cmd string, fallback func() time.Time) func() time.Time {
	return func() time.Time {
		for _, l := range ex.Run(ctx, command.Script(cmd)).Stdout() {
			if n, err := strconv.ParseInt(strings.TrimSpace(l), 10, 64); err == nil {
				return time.Unix(n, 0)
			}
		}
		return fallback()
	}
}
