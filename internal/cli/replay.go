package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bell-tower/zelta-go/internal/cache"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/units"
)

// replayFile is the cache entry holding replayable commands.
const replayFile = "replay"

// replayMax bounds the number of remembered source/target pairs.
const replayMax = 200

// replayVerbs are the commands "zelta replay" remembers.
var replayVerbs = []string{"backup", "sync", "replicate"}

// replayEntry is the last invocation for one source and target.
type replayEntry struct {
	Source string   `json:"source"`
	Target string   `json:"target"`
	Args   []string `json:"args"`
	// Options are the option values set on the command line.
	Options map[string]string `json:"options,omitempty"`
	Last    int64             `json:"last"`
	Status  int               `json:"status"`
}

func (e replayEntry) label() string { return e.Source + " -> " + e.Target }

// cacheStore returns the cache for opts, or nil when caching is off.
func (a *App) cacheStore(opts *config.Options) *cache.Store {
	if a.CacheDir == nil || !opts.Bool("CACHE") {
		return nil
	}
	dir := opts.Get("CACHE_DIR")
	if dir == "" {
		dir = a.CacheDir()
	}
	if dir == "" {
		return nil
	}
	s := &cache.Store{Dir: dir, Now: a.Now}
	if secs, err := units.ParseDuration(opts.Get("CACHE_TIMEOUT")); err == nil {
		s.TTL = time.Duration(secs) * time.Second
	}
	return s
}

// remember records a finished replayable command.
func (a *App) remember(store *cache.Store, v verb, operands []string, cli map[string]string, code int) {
	if store == nil || len(operands) != 2 || len(a.invocation) == 0 || !slices.Contains(replayVerbs, v.name) {
		return
	}
	var list []replayEntry
	store.Load(replayFile, &list)
	list = slices.DeleteFunc(list, func(e replayEntry) bool { return e.Source == operands[0] && e.Target == operands[1] })
	e := replayEntry{Source: operands[0], Target: operands[1], Args: slices.Clone(a.invocation), Options: maps.Clone(cli), Status: code, Last: a.now().Unix()}
	list = append([]replayEntry{e}, list...)
	if len(list) > replayMax {
		list = list[:replayMax]
	}
	_ = store.Save(replayFile, list)
}

func (a *App) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// replayList loads the remembered commands, most recent first.
func (a *App) replayList() []replayEntry {
	opts, _, err := a.NewLoader().Load(context.Background(), "backup", nil)
	if err != nil {
		return nil
	}
	var list []replayEntry
	a.cacheStore(opts).Load(replayFile, &list)
	return list
}

func (a *App) replayCommand(status *int) *cobra.Command {
	return &cobra.Command{
		Use:                "replay [PATTERN] [OPTIONS]",
		Short:              "Repeat a previous backup",
		Long:               "Repeat the last backup, sync, or replicate command between a source and target whose endpoints contain PATTERN. Options are added to the repeated command.",
		Args:               cobra.ArbitraryArgs,
		DisableFlagParsing: true,
		ValidArgsFunction: func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			if len(args) > 0 {
				return nil, cobra.ShellCompDirectiveNoFileComp
			}
			var cands []string
			for _, e := range a.replayList() {
				cands = append(cands, e.Source, e.Target)
			}
			return filterPrefix(dedupe(cands), toComplete), cobra.ShellCompDirectiveNoFileComp
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			*status = a.replay(cmd.Context(), args)
			return nil
		},
	}
}

// replay runs "zelta replay [PATTERN] [OPTIONS]".
func (a *App) replay(ctx context.Context, args []string) int {
	pattern, extra := "", args
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		pattern, extra = args[0], args[1:]
	}
	if slices.Contains(extra, "-h") || slices.Contains(extra, "--help") {
		fmt.Fprintln(a.Stdout, "usage: zelta replay [PATTERN] [OPTIONS]")
		return 0
	}
	var found []replayEntry
	for _, e := range a.replayList() {
		if strings.Contains(e.label(), pattern) {
			found = append(found, e)
		}
	}
	switch {
	case len(found) == 0 && pattern == "":
		fmt.Fprintln(a.Stderr, "error: no backups to replay")
		return 1
	case len(found) == 0:
		fmt.Fprintf(a.Stderr, "error: no backup matches '%s'\n", pattern)
		return 1
	}
	choice, ok := a.pickReplay(found)
	if !ok {
		return 1
	}
	argv := append([]string{choice.Args[0]}, extra...)
	argv = append(argv, choice.Args[1:]...)
	fmt.Fprintf(a.Stderr, "replaying: zelta %s\n", strings.Join(argv, " "))
	return a.execute(ctx, argv)
}

func (a *App) pickReplay(found []replayEntry) (replayEntry, bool) {
	labels := make([]string, len(found))
	for i, e := range found {
		labels[i] = e.label()
	}
	i, ok := a.choose(labels)
	if !ok {
		return replayEntry{}, false
	}
	return found[i], true
}

// choose returns the only label, or lists ambiguous labels and reads a
// choice from the terminal.
func (a *App) choose(labels []string) (int, bool) {
	if len(labels) == 1 {
		return 0, true
	}
	var tty io.ReadWriteCloser
	if a.TTY != nil {
		tty, _ = a.TTY()
	}
	out := a.Stdout
	if tty != nil {
		defer tty.Close()
		out = tty
	}
	for i, l := range labels {
		fmt.Fprintf(out, "%d. %s\n", i+1, l)
	}
	if tty == nil {
		fmt.Fprintln(a.Stderr, "error: pattern is ambiguous")
		return 0, false
	}
	fmt.Fprint(tty, "Choose your fate: ")
	line, _ := bufio.NewReader(tty).ReadString('\n')
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > len(labels) {
		fmt.Fprintln(a.Stderr, "error: no backup chosen")
		return 0, false
	}
	return n - 1, true
}
