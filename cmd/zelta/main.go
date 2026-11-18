// Command zelta is the Zelta command line. It also runs as zprune and the
// historical command names (zmatch, zpull, zsync, ...) when invoked through
// a link with that name.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"zelta.space/internal/app"
	"zelta.space/internal/backup"
	"zelta.space/internal/cli"
	"zelta.space/internal/config"
	"zelta.space/internal/execx"
	"zelta.space/internal/failover"
	"zelta.space/internal/match"
	"zelta.space/internal/policy"
	"zelta.space/internal/prune"
	"zelta.space/internal/report"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := newApp().Main(ctx, os.Args)
	stop()
	os.Exit(code)
}

// newApp wires the command line to the command implementations.
func newApp() *cli.App {
	failover.Backup = backup.Run
	a := cli.New(engines(), runPolicy)
	a.Multiplex = func(e execx.Executor) execx.Executor { return &execx.SSHMux{Inner: e} }
	a.Columns = matchColumns
	a.Progress = statusBoard()
	a.PolicyObjects = policy.Objects
	return a
}

func engines() map[string]cli.Engine {
	return map[string]cli.Engine{
		"match":    match.Run,
		"backup":   backup.Run,
		"snapshot": backup.Snapshot,
		"clone":    backup.Run,
		"revert":   backup.Run,
		"rotate":   backup.Run,
		"rebase":   backup.Rebase,
		"lock":     failover.Lock,
		"unlock":   failover.Unlock,
		"failover": failover.Failover,
		"propsync": failover.Propsync,
		"prune":    prune.Run,
		"zprune":   prune.Zprune,
		"report":   report.Run,
	}
}

func runPolicy(ctx context.Context, rt *app.Runtime, args []string, loader *config.Loader, cliOpts map[string]string, bk cli.Engine) error {
	return policy.Run(ctx, rt, args, policy.Deps{Loader: loader, CLI: cliOpts, Backup: bk})
}

func matchColumns() []string {
	var names []string
	for _, c := range match.Columns() {
		names = append(names, c.Name)
		names = append(names, c.Synonyms...)
	}
	return names
}
