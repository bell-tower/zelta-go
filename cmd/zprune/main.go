// Command zprune destroys the snapshots selected by "zelta prune". It is a
// separate binary so that zelta itself never destroys snapshots.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"zelta.space/internal/cli"
	"zelta.space/internal/execx"
	"zelta.space/internal/prune"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := newApp().Main(ctx, append([]string{"zprune"}, os.Args[1:]...))
	stop()
	os.Exit(code)
}

func newApp() *cli.App {
	a := cli.New(map[string]cli.Engine{"zprune": prune.Zprune}, nil)
	a.Multiplex = func(e execx.Executor) execx.Executor { return &execx.SSHMux{Inner: e} }
	return a
}
