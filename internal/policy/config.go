package policy

import (
	"bufio"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/logging"
)

// maxImportDepth bounds nested "import:" fragments.
const maxImportDepth = 8

// line is one policy line after import expansion.
type line struct {
	text string // raw text, including indentation and comments
	file string // file the line came from, as resolved
	num  int    // line number within file
}

var importRe = regexp.MustCompile(`^[ ]*import:[[:space:]]+[^[:space:]]+`)

// stripComment removes everything after the first "#" and trailing spaces.
func stripComment(s string) string {
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimRight(s, " \t")
}

// fileDir returns the directory used to resolve imports in path.
func fileDir(path string) string {
	i := strings.LastIndexByte(path, '/')
	switch {
	case i < 0:
		return "."
	case i == 0:
		return "/"
	}
	return path[:i]
}

// resolveImport resolves an import path relative to the importing file.
func resolveImport(path, base string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return fileDir(base) + "/" + path
}

// loadLines reads a policy file, expanding "import:" lines in place. Imported
// lines are prefixed with the indentation of the import line, so fragments
// inherit the nesting level of the import.
func loadLines(path string, depth int, stack map[string]bool) ([]line, error) {
	if depth > maxImportDepth {
		return nil, app.Usage("import depth exceeded near %s", path)
	}
	key := filepath.Clean(path)
	if stack[key] {
		return nil, app.Usage("recursive import detected: %s", path)
	}
	stack[key] = true
	defer delete(stack, key)

	var lines []line
	n := 0
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			n++
			raw := sc.Text()
			clean := stripComment(raw)
			if !importRe.MatchString(clean) {
				lines = append(lines, line{raw, path, n})
				continue
			}
			indent := clean[:strings.Index(clean, "import:")]
			child := resolveImport(strings.TrimLeft(clean[len(indent)+len("import:"):], " \t"), path)
			sub, err := loadLines(child, depth+1, stack)
			if err != nil {
				return nil, err
			}
			for _, l := range sub {
				if l.text != "" {
					l.text = indent + l.text
				}
				lines = append(lines, l)
			}
		}
		if err := sc.Err(); err != nil {
			return nil, app.Usage("unreadable policy file: %s: %v", path, err)
		}
	}
	if n == 0 {
		return nil, app.Usage("empty or unreadable policy file: %s", path)
	}
	return lines, nil
}

// Job is one configured backup.
type Job struct {
	Site string
	// Host is the source hostname; SourceRemote is the host key as written,
	// possibly with a user ("user@host").
	Host, SourceRemote string
	Source, Target     string
	// Opts is the option scope in effect for the job, including
	// policy-scoped options and LOG_PREFIX.
	Opts map[string]string
}

// SourceEndpoint returns the source endpoint passed to zelta backup.
func (j *Job) SourceEndpoint() string { return j.SourceRemote + ":" + j.Source }

// Name identifies the job in messages.
func (j *Job) Name() string { return "[" + j.Site + ": " + j.Target + "] " + j.SourceEndpoint() }

// Prefix is the LOG_PREFIX for the job's messages.
func (j *Job) Prefix() string { return j.Name() + ": " }

// parser turns policy lines into jobs.
type parser struct {
	// opts are the options of the policy command; values set by the user
	// environment or command line override every policy scope. Nil disables
	// that check.
	opts *config.Options
	log  *logging.Logger
	// strict rejects unknown options and malformed lines.
	strict bool
	// operands select jobs; empty selects all.
	operands map[string]bool

	global map[string]string
	sites  []string
	jobs   []*Job
	// all records every job, selected or not, for completion.
	all []*Job
}

func newParser(opts *config.Options, log *logging.Logger, operands []string) *parser {
	p := &parser{opts: opts, log: log, strict: true, global: map[string]string{}, operands: map[string]bool{}}
	for _, o := range operands {
		p.operands[o] = true
	}
	if opts != nil {
		for _, k := range opts.Keys() {
			if config.PolicyScoped(k) {
				p.global[k] = opts.Get(k)
			}
		}
	}
	return p
}

// fsRe is the field separator of Zelta's policy grammar: leading spaces, a
// "- " list marker, or a colon ending a key.
var fsRe = regexp.MustCompile(`^ +|- |:[[:space:]]+|:$`)

// fields splits a line like Awk does with the policy field separator.
func fields(s string) []string {
	var out []string
	last := 0
	for _, m := range fsRe.FindAllStringIndex(s, -1) {
		out = append(out, s[last:m[0]])
		last = m[1]
	}
	return append(out, s[last:])
}

func field(f []string, i int) string {
	if i < len(f) {
		return f[i]
	}
	return ""
}

var (
	globalOptRe = regexp.MustCompile(`^[^ ]+: +[^ ]`)
	siteRe      = regexp.MustCompile(`^[^ ]+:$`)
	siteOptRe   = regexp.MustCompile(`^  [^ ]+: +[^ ]`)
	hostRe      = regexp.MustCompile(`^  [^ ]+:$`)
	hostOptRe   = regexp.MustCompile(`^      [^ ]+: +[^ ]`)
	datasetRe   = regexp.MustCompile(`^ +-[[:space:]]+[^ ]`)
)

// keyValue splits "KEY: value" into its trimmed key and value. The value is
// the rest of the line, so values may contain ": " or "- ".
func keyValue(s string) (string, string) {
	k, v, _ := strings.Cut(strings.TrimLeft(s, " "), ":")
	return k, strings.TrimSpace(v)
}

func copyMap(m map[string]string) map[string]string {
	c := make(map[string]string, len(m))
	maps.Copy(c, m)
	return c
}

// parse reads the expanded policy lines.
func (p *parser) parse(lines []line) error {
	const parseError = "configuration parse error"
	context := "global"
	var site, host, remote string
	siteOpts, opts := map[string]string{}, map[string]string{}
	fail := func(l line) error {
		if !p.strict {
			return nil
		}
		return app.Usage("%s at %s:%d: %s", parseError, l.file, l.num, strings.TrimLeft(l.text, " \t"))
	}
	for _, l := range lines {
		s := stripComment(l.text)
		if s == "" {
			continue
		}
		f := fields(s)
		var err error
		switch {
		case globalOptRe.MatchString(s):
			err = p.set(p.global, s)
		case siteRe.MatchString(s):
			context, site = "site", strings.TrimSuffix(s, ":")
			p.sites = append(p.sites, site)
			siteOpts = copyMap(p.global)
		case siteOptRe.MatchString(s):
			err = p.set(siteOpts, s)
		case hostRe.MatchString(s):
			if context == "global" {
				err = fail(l)
				break
			}
			context, remote = "host", field(f, 1)
			host = remote[strings.LastIndexByte(remote, '@')+1:]
			opts = copyMap(siteOpts)
		case field(f, 1) == "options":
			context = "options"
		case field(f, 1) == "datasets":
			context = "datasets"
		case hostOptRe.MatchString(s):
			if context != "options" {
				err = fail(l)
				break
			}
			err = p.set(opts, s)
		case datasetRe.MatchString(s):
			if context != "datasets" && context != "host" {
				err = fail(l)
				break
			}
			p.addJob(&Job{Site: site, Host: host, SourceRemote: remote, Source: field(f, 2)}, field(f, 3), opts)
		default:
			err = fail(l)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// set assigns a "KEY: value" option into scope, normalizing the key and
// boolean values. Keys set explicitly in the environment or on the command
// line are skipped: they override every policy scope.
func (p *parser) set(scope map[string]string, s string) error {
	rawKey, val := keyValue(s)
	key := strings.ToUpper(strings.ReplaceAll(strings.TrimLeft(rawKey, " "), "-", "_"))
	key = strings.TrimPrefix(key, config.EnvPrefix)
	if n, _, ok := config.LegacyKey(key); ok {
		p.debugf("reassigning the option '%s' to '%s'", key, n)
		key = n
	}
	if !config.PolicyKey(key) {
		if !p.strict {
			return nil
		}
		return app.Usage("unknown option: %s", key)
	}
	if p.opts != nil {
		if _, ok := p.opts.Lookup(key); ok && p.opts.Source(key) >= config.FromEnv {
			p.debugf("skipping %s; already in args/user env", key)
			return nil
		}
	}
	boolish, integer := keyKinds(key)
	switch {
	case integer:
		if !intRe.MatchString(val) {
			if p.log != nil {
				p.log.Warnf("option '%s' is an integer; '%s' invalid", key, val)
			}
			return nil
		}
	case boolish:
		switch strings.ToLower(val) {
		case "yes", "true", "1":
			val = "1"
		case "no", "false", "0":
			val = "0"
		}
	}
	scope[key] = val
	return nil
}

var intRe = regexp.MustCompile(`^-?[0-9]+$`)

// keyKinds reports whether any flag treats key as a boolean or as a counter.
func keyKinds(key string) (boolish, integer bool) {
	for _, s := range config.Specs {
		if s.Key != key {
			continue
		}
		switch s.Type {
		case config.True, config.False:
			boolish = true
		case config.Incr, config.Decr:
			integer = true
		}
	}
	return boolish, integer
}

func (p *parser) debugf(format string, a ...any) {
	if p.log != nil {
		p.log.Debugf(format, a...)
	}
}

// addJob resolves a job's target and records it if the operands select it.
func (p *parser) addJob(j *Job, target string, opts map[string]string) {
	j.Target = resolveTarget(target, opts, j)
	if j.Target == "" {
		if p.log != nil {
			p.log.Warnf("no target defined for %s", j.Source)
		}
		return
	}
	j.Opts = copyMap(opts)
	j.Opts["LOG_PREFIX"] = j.Prefix()
	p.all = append(p.all, j)
	if p.selected(j) {
		p.jobs = append(p.jobs, j)
	}
}

// resolveTarget computes a job's target: an explicit target as given, or
// BACKUP_ROOT followed by the optional source host and the source's last
// ADD_DATASET_PREFIX+1 labels (all labels for -1).
func resolveTarget(target string, opts map[string]string, j *Job) string {
	if target != "" {
		return target
	}
	root := opts["BACKUP_ROOT"]
	if root == "" {
		return ""
	}
	if config.Truthy(opts["ADD_HOST_PREFIX"]) && j.Host != "" {
		root += "/" + j.Host
	}
	labels := strings.Split(j.Source, "/")
	keep := 1
	if prefix := strings.TrimSpace(opts["ADD_DATASET_PREFIX"]); prefix != "" {
		if prefix == "-1" {
			keep = len(labels)
		} else if n := atoi(prefix); n > 0 {
			keep = n + 1
		}
	}
	if keep > len(labels) {
		keep = len(labels)
	}
	for _, l := range labels[len(labels)-keep:] {
		if l != "" {
			root += "/" + l
		}
	}
	return root
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			break
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// selected reports whether the operands select the job. Operands match the
// site, host, host key, source, target, source endpoint, or source leaf.
func (p *parser) selected(j *Job) bool {
	if len(p.operands) == 0 {
		return true
	}
	leaf := j.Source[strings.LastIndexByte(j.Source, '/')+1:]
	for _, c := range []string{j.Site, j.Host, j.SourceRemote, j.Source, j.Target, j.SourceEndpoint(), leaf} {
		if c != "" && p.operands[c] {
			return true
		}
	}
	return false
}
