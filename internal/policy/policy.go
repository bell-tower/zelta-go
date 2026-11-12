// Package policy implements "zelta policy", which runs the backup jobs
// described by a YAML-like configuration file (zelta.conf):
//
//	GLOBAL_OPTION: value
//	SITE:
//	  SITE_OPTION: value
//	  host.example:
//	    options:
//	      HOST_OPTION: value
//	    datasets:
//	      - pool/dataset
//	      - pool/other: backup.example:tank/Backups/other
//
// Options cascade from the global scope to sites and hosts. Values from the
// user environment and command line override every scope, and policy values
// override zelta.env.
package policy

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/execx"
	"github.com/bell-tower/zelta-go/internal/shellwords"
)

// defaultBackupCommand runs jobs in-process.
const defaultBackupCommand = "zelta backup"

// Deps are the collaborators "zelta policy" needs to run backup jobs.
type Deps struct {
	// Loader resolves per-job options (defaults, zelta.env, environment).
	Loader *config.Loader
	// CLI holds command-line option values, applied to every job.
	CLI map[string]string
	// Backup runs one backup job in-process.
	Backup func(ctx context.Context, rt *app.Runtime, args []string) error
}

// hiddenDisplayKeys are policy-internal options removed from displayed
// commands; they are meaningless outside the policy run.
var hiddenDisplayKeys = []string{"LOG_PREFIX", "LOG_MODE", "LOG_LEVEL", "LOG_COMMAND"}

// Run implements "zelta policy [OPTIONS] [site|host|dataset]...".
func Run(ctx context.Context, rt *app.Runtime, args []string, deps Deps) error {
	p := newParser(rt.Opts, rt.Log, args)
	cfg := rt.Opts.Get("CONFIG")
	lines, err := loadLines(cfg, 0, map[string]bool{})
	if err != nil {
		return err
	}
	if err := p.parse(lines); err != nil {
		return err
	}
	if len(p.jobs) == 0 {
		if len(args) > 0 {
			return app.Fail("policy object(s) not found: %s", strings.Join(args, ", "))
		}
		return app.Usage("no datasets defined in %s", cfg)
	}
	if rt.DryRun() {
		if rt.Opts.Int("LOG_LEVEL") >= 3 {
			for _, j := range p.jobs {
				rt.Log.Noticef("+ %s", displayCommand(j))
			}
		} else {
			dryRunTable(rt, p.jobs)
		}
		return nil
	}
	r := &runner{rt: rt, deps: deps, warned: map[string]bool{}}
	if r.run(ctx, p.jobs, atoi(p.global["JOBS"])) > 0 {
		return &app.ExitError{Code: 1}
	}
	return nil
}

// dryRunTable prints the source and target of each job.
func dryRunTable(rt *app.Runtime, jobs []*Job) {
	width := 6
	for _, j := range jobs {
		width = max(width, len(j.SourceEndpoint()))
	}
	scripting := rt.Opts.Bool("NO_HEADER")
	if !scripting {
		rt.Log.Noticef("%-*s  %s", width, "SOURCE", "TARGET")
	}
	for _, j := range jobs {
		if scripting {
			rt.Log.Noticef("%s %s", j.SourceEndpoint(), j.Target)
		} else {
			rt.Log.Noticef("%-*s  %s", width, j.SourceEndpoint(), j.Target)
		}
	}
}

// forwarded returns the job options passed to the backup, excluding
// policy-scoped options, sorted by key.
func forwarded(opts map[string]string) []string {
	var keys []string
	for k, v := range opts {
		if v != "" && !config.PolicyScoped(k) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

func backupCommand(j *Job) string {
	if c := j.Opts["BACKUP_COMMAND"]; c != "" {
		return c
	}
	return defaultBackupCommand
}

// shellCommand renders a job as a shell command with its options as
// ZELTA_ environment assignments. hide lists keys to leave out.
func shellCommand(j *Job, extra map[string]string, hide []string) string {
	vals := copyMap(extra)
	for _, k := range forwarded(j.Opts) {
		if _, ok := vals[k]; !ok {
			vals[k] = j.Opts[k]
		}
	}
	keys := make([]string, 0, len(vals))
	for k := range vals {
		if !slices.Contains(hide, k) && vals[k] != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, config.EnvPrefix+k+"="+shellwords.DoubleQuote(vals[k]))
	}
	parts = append(parts, backupCommand(j), shellwords.SingleQuote(j.SourceEndpoint()), shellwords.SingleQuote(j.Target))
	return strings.Join(parts, " ")
}

// displayCommand is the copy-pasteable backup command shown by --dryrun -v.
func displayCommand(j *Job) string { return shellCommand(j, nil, hiddenDisplayKeys) }

// runner executes policy jobs.
type runner struct {
	rt     *app.Runtime
	deps   Deps
	mu     sync.Mutex
	warned map[string]bool
}

// run executes jobs and their retries, returning the number of jobs that
// still failed. With more than one site and jobs > 1, up to jobs sites run
// concurrently; each site's jobs run in order.
func (r *runner) run(ctx context.Context, jobs []*Job, maxSites int) int {
	groups := [][]*Job{jobs}
	if maxSites > 1 {
		groups = bySite(jobs)
	}
	if len(groups) < 2 {
		return r.runGroup(ctx, jobs)
	}
	r.rt.Log.Debugf("launching %d 'zelta policy' jobs", maxSites)
	var (
		wg     sync.WaitGroup
		failed int
		mu     sync.Mutex
		sem    = make(chan struct{}, maxSites)
	)
	for _, g := range groups {
		wg.Add(1)
		sem <- struct{}{}
		go func(g []*Job) {
			defer wg.Done()
			defer func() { <-sem }()
			n := r.runGroup(ctx, g)
			mu.Lock()
			failed += n
			mu.Unlock()
		}(g)
	}
	wg.Wait()
	return failed
}

func bySite(jobs []*Job) [][]*Job {
	var groups [][]*Job
	index := map[string]int{}
	for _, j := range jobs {
		i, ok := index[j.Site]
		if !ok {
			i = len(groups)
			index[j.Site] = i
			groups = append(groups, nil)
		}
		groups[i] = append(groups[i], j)
	}
	return groups
}

// runGroup runs jobs in order, then retries failures up to each job's RETRY.
func (r *runner) runGroup(ctx context.Context, jobs []*Job) int {
	var failed []*Job
	for _, j := range jobs {
		if !r.runJob(ctx, j) {
			failed = append(failed, j)
		}
	}
	for round := 1; len(failed) > 0 && ctx.Err() == nil; round++ {
		var still, retried []*Job
		for _, j := range failed {
			if atoi(j.Opts["RETRY"]) < round {
				still = append(still, j)
				continue
			}
			retried = append(retried, j)
			r.rt.Log.Noticef("retrying: %s", j.Name())
			if !r.runJob(ctx, j) {
				still = append(still, j)
			}
		}
		failed = still
		if len(retried) == 0 {
			break
		}
	}
	return len(failed)
}

// runJob runs one backup and reports success.
func (r *runner) runJob(ctx context.Context, j *Job) bool {
	if backupCommand(j) != defaultBackupCommand {
		return r.runExternal(ctx, j)
	}
	opts, err := r.jobOptions(ctx, j)
	if err != nil {
		r.rt.Log.Errorf("%s: %v", j.Name(), err)
		return false
	}
	jrt, _ := app.Build(r.rt, opts)
	if r.deps.Backup == nil {
		return app.Finish(jrt, app.Fail("backup is unavailable")) == 0
	}
	return app.Finish(jrt, r.deps.Backup(ctx, jrt, []string{j.SourceEndpoint(), j.Target})) == 0
}

// cliValues returns the command-line values forwarded to backups.
func (r *runner) cliValues() map[string]string {
	out := map[string]string{}
	for k, v := range r.deps.CLI {
		// VERB is not forwarded: Zelta 1.2 always ran jobs as "zelta backup",
		// and forwarding --rotate would rename every target on each run.
		if !config.PolicyScoped(k) && k != "USAGE" && k != "DRYRUN" && k != "VERB" {
			out[k] = v
		}
	}
	return out
}

// jobOptions layers a job's options: defaults < zelta.env < policy <
// environment < command line. The job's LOG_PREFIX always applies.
func (r *runner) jobOptions(ctx context.Context, j *Job) (*config.Options, error) {
	vals := map[string]string{}
	for _, k := range forwarded(j.Opts) {
		vals[k] = j.Opts[k]
	}
	var opts *config.Options
	if r.deps.Loader != nil {
		o, warnings, err := r.deps.Loader.Load(ctx, "backup", vals)
		if err != nil {
			return nil, err
		}
		r.warnOnce(warnings)
		opts = o
	} else {
		opts = r.rt.Opts.Clone()
		for k, v := range vals {
			opts.Override(k, v, config.FromPolicy)
		}
	}
	cli := r.cliValues()
	opts.Apply(cli)
	opts.Set("LOG_PREFIX", j.Prefix(), config.FromCLI)
	opts.Set("VERB", "backup", config.FromCLI)
	return opts, nil
}

func (r *runner) warnOnce(warnings []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, w := range warnings {
		if !r.warned[w] {
			r.warned[w] = true
			r.rt.Log.Warnf("%s", w)
		}
	}
}

// runExternal runs a job through a custom BACKUP_COMMAND, passing options
// as environment assignments and streaming its output.
func (r *runner) runExternal(ctx context.Context, j *Job) bool {
	script := shellCommand(j, r.cliValues(), nil)
	r.rt.Log.Debugf("`%s`", script)
	res := r.rt.Exec.Pipe(ctx, command.Pipeline{command.Script(script)}, func(l execx.Line) {
		w := r.rt.Stdout
		if l.Stream == execx.Stderr {
			w = r.rt.Stderr
		}
		fmt.Fprintln(w, l.Text)
	})
	return res.OK()
}

// Objects returns the site, host, dataset, and target names in the policy
// file at path, for shell completion.
func Objects(path string) []string {
	lines, err := loadLines(path, 0, map[string]bool{})
	if err != nil {
		return nil
	}
	p := newParser(nil, nil, nil)
	p.strict = false
	_ = p.parse(lines)
	seen := map[string]bool{}
	add := func(s string) {
		if s != "" {
			seen[s] = true
		}
	}
	for _, s := range p.sites {
		add(s)
	}
	for _, j := range p.all {
		add(j.Host)
		add(j.SourceRemote)
		add(j.Source)
		add(j.Target)
		add(j.SourceEndpoint())
		add(j.Source[strings.LastIndexByte(j.Source, '/')+1:])
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
