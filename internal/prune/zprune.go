package prune

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"

	"zelta.space/internal/app"
	"zelta.space/internal/command"
	"zelta.space/internal/endpoint"
	"zelta.space/internal/execx"
	"zelta.space/internal/shellwords"
	"zelta.space/internal/units"
)

// zprune carries the state of one zprune run.
type zprune struct {
	rt     *app.Runtime
	ctx    context.Context
	src    endpoint.Endpoint
	srcID  string
	level  int
	quiet  bool
	dryRun bool
	force  bool
}

// Zprune implements zprune: it selects candidates exactly like "zelta
// prune", previews them with "zfs destroy -nvp", asks for confirmation, and
// destroys them. Its own messages are written directly to the standard
// streams, as the original shell wrapper printed them.
func Zprune(ctx context.Context, rt *app.Runtime, args []string) error {
	o := rt.Opts
	z := &zprune{rt: rt, ctx: ctx, level: 2, dryRun: o.Bool("DRYRUN"), force: o.Bool("PRUNE_FORCE")}
	if o.Get("LOG_LEVEL") != "" {
		z.level = o.Int("LOG_LEVEL")
	}
	z.quiet = z.level < 2
	if len(args) == 0 || args[0] == "" {
		z.errorf("endpoint required")
		return &app.UsageError{}
	}
	z.srcID = args[0]
	z.src = rt.Endpoint(args[0])

	// Candidate selection never runs in dry-run mode and logs at the
	// default level, whatever the zprune verbosity.
	sel := *rt
	sel.Opts = o.Clone()
	sel.Opts.Unset("DRYRUN")
	sel.Log = rt.Log.WithLevel(2)
	plan, err := Build(ctx, &sel, args, false)
	if err != nil {
		return err
	}
	if o.Bool("PRUNE_VISUAL") {
		io.WriteString(rt.Stdout, plan.Visual())
	}
	if plan.Empty {
		sel.Log.Errorf("datasets inaccessible or do not exist")
	}
	candidates := plan.Candidates()
	if len(candidates) == 0 {
		if !z.quiet {
			fmt.Fprintln(rt.Stdout, "No snapshots to prune.")
		}
		return nil
	}
	if invalid := invalidCandidates(candidates); len(invalid) > 0 {
		z.errorf("refusing unexpected prune output:")
		fmt.Fprintf(rt.Stderr, "\n%s\n", strings.Join(invalid, "\n"))
		z.errorf("destroy aborted")
		return &app.ExitError{Code: 1}
	}
	groups := group(candidates)

	if z.wantPreview() {
		if err := z.preview(groups); err != nil {
			return err
		}
	}
	if z.dryRun {
		return nil
	}
	hasGuard := len(args) > 1 && args[1] != "" || o.Has("MATCH_ENDPOINT")
	if g := o.Get("PRUNE_GUARD"); !hasGuard && !guardDisabled(g) {
		z.errorf("match endpoint required to destroy snapshots with prune guard enabled;\n  use --match-endpoint=GUARD or --no-prune-guard")
		return &app.ExitError{Code: 1}
	}
	if !z.force && !z.confirm() {
		fmt.Fprintln(rt.Stdout, "Aborted.")
		return &app.ExitError{Code: 1}
	}
	if !z.destroyAll(groups, "", rt.Stdout) {
		return &app.ExitError{Code: 1}
	}
	return nil
}

func (z *zprune) errorf(format string, a ...any) {
	fmt.Fprintf(z.rt.Stderr, "zprune: "+format+"\n", a...)
}

// wantPreview reports whether to run the "zfs destroy -nvp" preview: never
// below LOG_LEVEL 1 (-qq), and otherwise unless a quiet forced run makes it
// pointless.
func (z *zprune) wantPreview() bool {
	return z.level >= 1 && (z.dryRun || !z.quiet || !z.force)
}

// invalidCandidates returns candidates that are not snapshot names or that
// contain a single quote, which the destroy commands could not carry safely.
func invalidCandidates(cands []string) []string {
	var bad []string
	for _, c := range cands {
		if c != "" && (strings.Contains(c, "'") || !strings.Contains(c, "@")) {
			bad = append(bad, c)
		}
	}
	return bad
}

// group joins consecutive candidates of the same dataset into one zfs
// destroy argument: "pool/ds@a%b,c".
func group(cands []string) []string {
	var out []string
	var ds, arg string
	for _, c := range cands {
		if !strings.Contains(c, "@") {
			continue
		}
		cds, spec := c[:strings.LastIndex(c, "@")], c[strings.Index(c, "@")+1:]
		switch {
		case ds == "":
			ds, arg = cds, c
		case cds == ds:
			arg += "," + spec
		default:
			out = append(out, arg)
			ds, arg = cds, c
		}
	}
	if arg != "" {
		out = append(out, arg)
	}
	return out
}

// destroyCmd returns "zfs destroy [flags] target" on the source host.
func (z *zprune) destroyCmd(flags, target string) command.Cmd {
	if z.src.IsRemote() {
		script := strings.Join(nonBlank("zfs destroy", flags, shellwords.SingleQuote(target)), " ")
		return command.Cmd{Remote: command.NewRemote(z.remoteCommand(), z.src.Remote), Script: script}
	}
	args := command.Words("zfs", "destroy")
	if flags != "" {
		args = append(args, command.Word(flags))
	}
	return command.New(append(args, command.Name(target))...)
}

// displayDestroy renders a destroy command as zprune has always shown it.
func (z *zprune) displayDestroy(flags, target string) string {
	if z.src.IsRemote() {
		inner := strings.Join(nonBlank("zfs destroy", flags, "'"+target+"'"), " ")
		return fmt.Sprintf(`%s %s "%s"`, z.remoteCommand(), z.src.Remote, inner)
	}
	return strings.Join(nonBlank("zfs destroy", flags, target), " ")
}

func (z *zprune) remoteCommand() string {
	if c := z.rt.Opts.Get("REMOTE_COMMAND"); c != "" {
		return c
	}
	return "ssh"
}

func nonBlank(parts ...string) []string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// destroyAll runs the destroy commands for every group, writing their
// standard output to out; it reports whether all of them succeeded. With
// flags (a preview), each group's argument is written to out first.
func (z *zprune) destroyAll(groups []string, flags string, out io.Writer) bool {
	ok := true
	for _, g := range groups {
		if flags != "" {
			fmt.Fprintln(out, g)
		} else if z.level >= 4 {
			fmt.Fprintf(z.rt.Stderr, "debug: `%s`\n", z.displayDestroy("", g))
		}
		res := z.rt.Exec.Run(z.ctx, z.destroyCmd(flags, g))
		for _, l := range res.Lines {
			if l.Stream == execx.Stdout {
				fmt.Fprintln(out, l.Text)
			} else {
				fmt.Fprintln(z.rt.Stderr, l.Text)
			}
		}
		if !res.OK() {
			z.errorf("destroy command failed: %s", g)
			ok = false
		}
	}
	return ok
}

// preview prints the destroy commands, runs the dry-run destroy, and
// summarizes the snapshots and space that would be freed.
func (z *zprune) preview(groups []string) error {
	out := z.rt.Stdout
	if z.level >= 2 {
		fmt.Fprintf(out, "Preparing to prune \"%s\" using the following commands:\n", z.srcID)
		for _, g := range groups {
			fmt.Fprintln(out, z.displayDestroy("", g))
		}
		fmt.Fprintln(out)
		fmt.Fprintln(out, "Calculating snapshots and space to be freed...")
	}
	var buf strings.Builder
	if !z.destroyAll(groups, "-nvp", &buf) {
		if !z.quiet {
			io.WriteString(out, buf.String())
		}
		z.errorf("destroy aborted")
		return &app.ExitError{Code: 1}
	}
	if z.level >= 2 {
		fmt.Fprintln(out)
	}
	p := parsePreview(buf.String())
	totalCount, totalBytes := z.totals()
	if size := z.rt.Opts.Get("PRUNE_SIZE"); size != "" && len(p.datasets) > 1 {
		fmt.Fprintf(out, "Estimating %s per dataset savings on %d datasets.\n\n", size, len(p.datasets))
	}
	if len(p.datasets) > 1 {
		for _, ds := range p.datasets {
			fmt.Fprintf(out, "%s: %d snapshots, %s\n", ds, p.count[ds], units.HumanBytesPrecise(p.bytes[ds]))
		}
	}
	fmt.Fprintf(out, "%d snapshots (%d%% of %d) will be destroyed\n", p.destroyed, percent(float64(p.destroyed), float64(totalCount)), totalCount)
	fmt.Fprintf(out, "%s total reclaimed (%d%% of %s)\n", units.HumanBytesPrecise(p.reclaim), percent(p.reclaim, totalBytes), units.HumanBytesPrecise(totalBytes))
	return nil
}

func percent(part, total float64) int {
	if total <= 0 {
		return 0
	}
	return int(part/total*100 + 0.5)
}

// previewSummary is what "zfs destroy -nvp" reported.
type previewSummary struct {
	destroyed int
	reclaim   float64
	datasets  []string
	count     map[string]int
	bytes     map[string]float64
}

// parsePreview reads "zfs destroy -nvp" output in either its parsable form
// ("destroy\tpool/ds@snap", "reclaim\t1234") or its human form ("would
// destroy pool/ds@snap", "would reclaim 1.2M").
func parsePreview(text string) previewSummary {
	p := previewSummary{count: map[string]int{}, bytes: map[string]float64{}}
	current := ""
	for line := range strings.SplitSeq(text, "\n") {
		fields := strings.Fields(line)
		var name string
		switch {
		case strings.HasPrefix(line, "destroy\t") || strings.HasPrefix(line, "destroy "):
			name = strings.TrimSpace(line[len("destroy"):])
		case strings.HasPrefix(line, "would destroy "):
			name = strings.TrimSpace(line[len("would destroy "):])
		case strings.HasPrefix(line, "reclaim\t") || strings.HasPrefix(line, "reclaim "):
			n := units.Atof(fields[1])
			p.reclaim += n
			if current != "" {
				p.bytes[current] += n
			}
			continue
		case strings.Contains(line, "would reclaim "):
			n := units.Atof(fields[len(fields)-1])
			p.reclaim += n
			if current != "" {
				p.bytes[current] += n
			}
			continue
		default:
			continue
		}
		p.destroyed++
		if i := strings.Index(name, "@"); i >= 0 {
			name = name[:i]
		}
		current = name
		if _, ok := p.count[name]; !ok {
			p.datasets = append(p.datasets, name)
		}
		p.count[name]++
	}
	return p
}

// totals counts the endpoint's snapshots and sums their written bytes.
func (z *zprune) totals() (count int, written float64) {
	count = len(z.list("-H -r -t snapshot -o name").Stdout())
	for _, l := range z.list("-H -r -p -t snapshot -o written").Stdout() {
		if f := strings.Fields(l); len(f) > 0 {
			written += units.Atof(f[0])
		}
	}
	return count, written
}

func (z *zprune) list(flags string) execx.Result {
	var c command.Cmd
	if z.src.IsRemote() {
		c = command.Cmd{Remote: command.NewRemote(z.remoteCommand(), z.src.Remote), Script: "zfs list " + flags + " " + shellwords.SingleQuote(z.src.DS)}
	} else {
		c = command.New(append(append(command.Words("zfs", "list"), command.Fragment(flags)...), command.Name(z.src.DS))...)
	}
	return z.rt.Exec.Run(z.ctx, c)
}

// confirm asks on the terminal before destroying.
func (z *zprune) confirm() bool {
	fmt.Fprint(z.rt.Stdout, "Destroy now? [y/N] ")
	tty, err := z.rt.OpenTTY()
	if err != nil {
		return false
	}
	defer tty.Close()
	answer, _ := bufio.NewReader(tty).ReadString('\n')
	switch strings.TrimRight(answer, "\r\n") {
	case "y", "Y", "yes", "YES":
		return true
	}
	return false
}

// guardDisabled reports whether PRUNE_GUARD explicitly disables the guard.
func guardDisabled(v string) bool {
	g, err := parseGuard(v)
	return v != "" && err == nil && g == guardNone
}

// Destroy destroys a plan's candidates the way a forced zprune does, but
// without previews or prompts, logging through the runtime. It returns the
// "zfs destroy" arguments that succeeded. Like zprune, it refuses to run
// without a match endpoint unless PRUNE_GUARD disables the guard.
func Destroy(ctx context.Context, rt *app.Runtime, plan *Plan) ([]string, error) {
	if plan.Guard.DS == "" && !guardDisabled(rt.Opts.Get("PRUNE_GUARD")) {
		return nil, app.Fail("match endpoint required to destroy snapshots with prune guard enabled")
	}
	candidates := plan.Candidates()
	if invalid := invalidCandidates(candidates); len(invalid) > 0 {
		return nil, app.Fail("refusing unexpected prune output: %s", strings.Join(invalid, ", "))
	}
	z := &zprune{rt: rt, ctx: ctx, src: plan.Source}
	var done []string
	failed := false
	for _, g := range group(candidates) {
		c := z.destroyCmd("", g)
		if rt.DryRun() {
			rt.Log.Noticef("+ %s", c.String())
			continue
		}
		res := rt.Run(ctx, c)
		for _, l := range res.Lines {
			if l.Stream == execx.Stderr {
				rt.Log.Warnf("%s", app.SanitizeLine(l.Text))
			} else {
				rt.Log.Infof("%s", app.SanitizeLine(l.Text))
			}
		}
		if !res.OK() {
			rt.Log.Errorf("destroy command failed: %s", g)
			failed = true
			continue
		}
		rt.Log.Noticef("destroyed '%s'", g)
		done = append(done, g)
	}
	if failed {
		return done, &app.ExitError{Code: 1}
	}
	return done, nil
}
