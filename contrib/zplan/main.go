// Command zplan tunes zelta prune's retention options interactively and
// shows each snapshot's fate as you type. It never destroys anything: it
// prints the zprune command for the plan on Enter.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/pflag"
	"golang.org/x/term"

	"zelta.space/zelta"
)

const usage = `usage: zplan [OPTIONS] ENDPOINT [GUARD]

Plan a zelta prune interactively. Each change re-plans with Zelta and
shows every snapshot: destroyed, kept by retention, or guarded by the
match with GUARD. Enter prints the zprune command; nothing is destroyed.
Without a terminal, zplan prints the plan once.

Options:
`

func main() {
	os.Exit(run())
}

func run() int {
	fl := pflag.NewFlagSet("zplan", pflag.ContinueOnError)
	fl.Usage = func() {
		fmt.Fprint(os.Stderr, usage)
		fl.PrintDefaults()
	}
	guard := fl.String("match-endpoint", "", "protect the snapshots the replica `GUARD` needs")
	val := map[int]*string{
		fNum:   fl.String("prune-num", "", "keep the newest `N` snapshots older than the match"),
		fTime:  fl.String("prune-time", "", "keep snapshots newer than `DURATION`"),
		fGrid:  fl.String("prune-grid", "", "GFS `GRID` retention such as '30x1 day, 52x1 week, 1 year'"),
		fSize:  fl.String("prune-size", "", "select the oldest snapshots until `SIZE` is reclaimed"),
		fGuard: fl.String("prune-guard", "", "guard `MODE`: latest, unsynced, or none"),
		fDepth: fl.StringP("depth", "d", "", "limit the recursion depth to `LEVELS`"),
	}
	exclude := fl.StringArrayP("exclude", "X", nil, "exclude dataset or snapshot `PATTERN`s")
	include := fl.StringArray("include", nil, "include only dataset or snapshot `PATTERN`s")
	noGuard := fl.Bool("no-prune-guard", false, "disable match-endpoint safety checks")
	if err := fl.Parse(os.Args[1:]); err != nil {
		if err == pflag.ErrHelp {
			return 0
		}
		return 2
	}
	args := fl.Args()
	if len(args) == 2 && *guard == "" {
		*guard, args = args[1], args[:1]
	}
	if len(args) != 1 {
		fl.Usage()
		return 2
	}
	endpoint := args[0]

	fs := newFields(*guard)
	for i, v := range val {
		if *v != "" {
			fs[i].val = *v
		}
	}
	if *noGuard {
		fs[fGuard].val = "none"
	}
	fs[fExclude].val = strings.Join(*exclude, ",")
	fs[fInclude].val = strings.Join(*include, ",")

	c, err := zelta.New(zelta.WithEnvironment())
	if err != nil {
		fmt.Fprintln(os.Stderr, "zplan:", err)
		return 1
	}
	a := &app{c: c, endpoint: endpoint, guard: *guard, fields: fs, gen: 1}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return a.print()
	}
	return a.interactive()
}

// app is the planner's state.
type app struct {
	c               *zelta.Client
	endpoint, guard string
	fields          []field
	focus, scroll   int
	// sel is the selected dataset row, or -1 while editing fields; zoom
	// shows it alone. page is how many rows the last view showed.
	sel, page int
	zoom      bool

	gen, planGen int
	busy         bool
	err          error
	rows         []row
	empty        int // datasets without snapshots, not shown
	st           style
}

type planned struct {
	gen   int
	rows  []row
	empty int
	err   error
}

func (a *app) plan(ctx context.Context, gen int) planned {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	p, err := a.c.PlanPrune(ctx, a.endpoint, options(a.fields, a.guard))
	if err != nil {
		return planned{gen: gen, err: err}
	}
	r, empty := rows(p)
	return planned{gen: gen, rows: r, empty: empty}
}

// print writes the plan once, for pipes and scripts.
func (a *app) print() int {
	p := a.plan(context.Background(), 0)
	if p.err != nil {
		fmt.Fprintln(os.Stderr, "zplan:", p.err)
		return 1
	}
	st := style{color: false}
	lines, per := bars(st, p.rows, 0, -1, 80, len(p.rows))
	if per > 1.05 {
		fmt.Printf("# each cell is about %.3g snapshots\n", per)
	}
	for _, l := range lines {
		fmt.Println(strings.TrimRight(l, " "))
	}
	fmt.Println(command(a.fields, a.endpoint, a.guard))
	return 0
}

func (a *app) interactive() int {
	scr, err := openScreen()
	if err != nil {
		fmt.Fprintln(os.Stderr, "zplan:", err)
		return 1
	}
	_, noColor := os.LookupEnv("NO_COLOR")
	a.st = style{color: !noColor && os.Getenv("TERM") != "dumb"}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	keys := make(chan string, 64)
	go scr.keys(keys)
	results := make(chan planned, 1)
	resize := winch()
	debounce := time.NewTimer(0)

	start := func() {
		a.busy = true
		gen := a.gen
		go func() { results <- a.plan(ctx, gen) }()
	}
	replan := func() {
		a.gen++
		debounce.Reset(300 * time.Millisecond)
	}

	a.sel = -1
	print := false
loop:
	for {
		scr.draw(a.view(scr.size()))
		select {
		case <-debounce.C:
			if !a.busy {
				start()
			}
		case r := <-results:
			a.busy = false
			if r.gen == a.gen {
				a.err, a.planGen = r.err, r.gen
				if r.err == nil {
					a.rows, a.empty = r.rows, r.empty
					if a.sel >= len(a.rows) || len(a.rows) < 2 {
						a.sel, a.zoom = -1, false
					}
				}
			} else {
				start()
			}
		case <-resize:
		case k, ok := <-keys:
			if !ok {
				break loop
			}
			switch a.key(k) {
			case actQuit:
				break loop
			case actPrint:
				print = true
				break loop
			case actReplan:
				replan()
			}
		}
	}
	cancel()
	scr.close()
	if print {
		fmt.Println(command(a.fields, a.endpoint, a.guard))
	}
	return 0
}

type action int

const (
	actNone action = iota
	actReplan
	actQuit
	actPrint
)

func (a *app) key(k string) action {
	switch k {
	case kInterrupt:
		return actQuit
	case kEnter:
		return actPrint
	}
	if a.sel >= 0 {
		return a.datasetKey(k)
	}
	f := &a.fields[a.focus]
	switch k {
	case kEsc:
		return actQuit
	case kUp, kBackTab:
		a.focus = (a.focus + len(a.fields) - 1) % len(a.fields)
	case kDown, kTab:
		if a.focus == len(a.fields)-1 && len(a.rows) > 1 {
			a.sel = a.scroll
			return actNone
		}
		a.focus = (a.focus + 1) % len(a.fields)
	case kLeft, kRight:
		if len(f.presets) == 0 {
			return actNone
		}
		i := indexOf(f.presets, f.val)
		step := 1
		if k == kLeft {
			i, step = max(i, 0), len(f.presets)-1
		}
		f.val = f.presets[(i+step)%len(f.presets)]
		return actReplan
	case kPgUp:
		a.scroll = max(0, a.scroll-a.page)
	case kPgDn:
		a.scroll += a.page
	case kBack:
		if f.cycleOnly || f.val == "" {
			return actNone
		}
		r := []rune(f.val)
		f.val = string(r[:len(r)-1])
		return actReplan
	case kClear:
		if f.cycleOnly || f.val == "" {
			return actNone
		}
		f.val = ""
		return actReplan
	default:
		if f.cycleOnly || len([]rune(k)) != 1 {
			return actNone
		}
		f.val += k
		return actReplan
	}
	return actNone
}

// datasetKey moves through the dataset rows below the fields.
func (a *app) datasetKey(k string) action {
	switch k {
	case kEsc, kLeft:
		if !a.zoom {
			if k == kEsc {
				return actQuit
			}
			return actNone
		}
		a.zoom = false
	case kRight, " ":
		a.zoom = true
	case kTab, kBackTab:
		a.sel, a.zoom = -1, false
	case kUp:
		if a.sel == 0 {
			a.sel, a.zoom = -1, false
		} else {
			a.sel--
		}
	case kDown:
		a.sel = min(a.sel+1, len(a.rows)-1)
	case kPgUp:
		a.sel = max(0, a.sel-a.page)
	case kPgDn:
		a.sel = min(len(a.rows)-1, a.sel+a.page)
	}
	return actNone
}

// indexOf returns the preset index of v, or -1 so the next preset is the
// first.
func indexOf(list []string, v string) int {
	for i, s := range list {
		if s == strings.TrimSpace(v) {
			return i
		}
	}
	return -1
}
