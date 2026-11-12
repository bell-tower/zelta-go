package config

import (
	"context"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// internalKeys are runtime values that ordinary commands never read from the
// environment; they are only honored for internal "ipc-run" invocations.
var internalKeys = map[string]bool{
	"VERB": true, "OPERANDS": true, "USAGE": true, "MATCH_PIPE": true, "VERSION": true,
	"LEGACY_ENV": true, "SRC_ID": true, "SRC_REMOTE": true, "SRC_USER": true, "SRC_HOST": true,
	"SRC_DS": true, "SRC_SNAP": true, "TGT_ID": true, "TGT_REMOTE": true, "TGT_USER": true,
	"TGT_HOST": true, "TGT_DS": true, "TGT_SNAP": true, "POLICY_LOG_MODE": true,
	"POLICY_LOG_LEVEL": true, "POLICY_LOG_COMMAND": true,
}

// Loader resolves options. Its zero value is not useful; use NewLoader.
type Loader struct {
	// Env is the process environment.
	Env map[string]string
	// Now is the time used for default snapshot names when Clock is nil.
	Now time.Time
	// Clock, when set, supplies the time for each Load, so options loaded
	// later (such as each policy job) get their own default snapshot name.
	Clock func() time.Time
	// Hostname and User describe the local machine.
	Hostname, User string
	// DirExists reports whether a directory exists (overridable in tests).
	DirExists func(string) bool
	// Internal keeps internal runtime keys from the environment (ipc-run).
	Internal bool
	// Overrides are applied above every other source before defaults are
	// derived from them (so REMOTE_COMMAND still sets REMOTE_DEFAULT).
	Overrides map[string]string
	// SkipEnvFile ignores zelta.env.
	SkipEnvFile bool
}

// NewLoader returns a loader for the current process.
func NewLoader() *Loader {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	l := &Loader{Env: env, Now: time.Now(), Clock: time.Now, DirExists: dirExists}
	l.Hostname = env["HOSTNAME"]
	if l.Hostname == "" {
		l.Hostname, _ = os.Hostname()
	}
	if l.Hostname == "" {
		l.Hostname = "localhost"
	}
	l.User = env["USER"]
	if l.User == "" {
		if u, err := user.Current(); err == nil {
			l.User = u.Username
		}
	}
	return l
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// Paths are the locations of Zelta's configuration and documentation.
type Paths struct {
	Etc, Env, Config, Doc string
}

// Paths resolves configuration locations: ZELTA_ETC, else ~/.config/zelta
// when present, else /usr/local/etc/zelta (or /etc/zelta when only that
// exists); ZELTA_ENV and ZELTA_CONFIG default to files under it.
func (l *Loader) Paths() Paths {
	var p Paths
	home := l.Env["HOME"]
	p.Etc = l.Env[EnvPrefix+"ETC"]
	if p.Etc == "" {
		switch {
		case home != "" && l.DirExists(filepath.Join(home, ".config/zelta")):
			p.Etc = filepath.Join(home, ".config/zelta")
		case !l.DirExists("/usr/local/etc/zelta") && l.DirExists("/etc/zelta"):
			p.Etc = "/etc/zelta"
		default:
			p.Etc = "/usr/local/etc/zelta"
		}
	}
	p.Env = l.Env[EnvPrefix+"ENV"]
	if p.Env == "" {
		p.Env = filepath.Join(p.Etc, "zelta.env")
	}
	p.Config = l.Env[EnvPrefix+"CONFIG"]
	if p.Config == "" {
		p.Config = filepath.Join(p.Etc, "zelta.conf")
	}
	p.Doc = l.Env[EnvPrefix+"DOC"]
	if p.Doc == "" && home != "" && l.DirExists(filepath.Join(home, ".local/share/zelta/doc")) {
		p.Doc = filepath.Join(home, ".local/share/zelta/doc")
	}
	return p
}

// EnvOptions returns the ZELTA_ variables from the environment.
func (l *Loader) EnvOptions() map[string]string {
	out := map[string]string{}
	for k, v := range l.Env {
		if !strings.HasPrefix(k, EnvPrefix) {
			continue
		}
		key := strings.TrimPrefix(k, EnvPrefix)
		if key == "" || (!l.Internal && internalKeys[key]) {
			continue
		}
		out[key] = v
	}
	return out
}

// Load resolves the options for verb from built-in defaults, zelta.env,
// optional policy values, and the environment. Command-line operations are
// applied afterwards with ApplyOps.
func (l *Loader) Load(ctx context.Context, verb string, policy map[string]string) (*Options, []string, error) {
	var warnings []string
	o := NewOptions()
	for k, v := range l.EnvOptions() {
		o.Set(k, v, FromEnv)
	}
	for k, v := range policy {
		o.Override(k, v, FromPolicy)
	}

	paths := l.Paths()
	fileEnv := make([]string, 0, len(l.Env)+len(policy))
	for k, v := range l.Env {
		fileEnv = append(fileEnv, k+"="+v)
	}
	for k, v := range policy {
		if _, ok := l.Env[EnvPrefix+k]; !ok {
			fileEnv = append(fileEnv, EnvPrefix+k+"="+v)
		}
	}
	sort.Strings(fileEnv)
	if !l.SkipEnvFile {
		vals, w, err := LoadEnvFile(ctx, paths.Env, fileEnv)
		warnings = append(warnings, w...)
		if err != nil {
			warnings = append(warnings, err.Error())
		}
		for k, v := range vals {
			o.Override(k, v, FromEnvFile)
		}
	}
	for k, v := range l.Overrides {
		o.Set(k, v, FromCLI)
	}

	warnings = append(warnings, applyLegacy(o)...)
	applyVerbDefaults(o, verb)
	now := l.Now
	if l.Clock != nil {
		now = l.Clock()
	}
	applyDefaults(o, now)

	o.Set("ETC", paths.Etc, FromDefault)
	o.Set("ENV", paths.Env, FromDefault)
	if paths.Doc != "" {
		o.Set("DOC", paths.Doc, FromDefault)
	}
	if !o.Has("CONFIG") {
		o.Set("CONFIG", paths.Config, FromDefault)
	}
	if !o.Has("HOSTNAME") {
		o.Set("HOSTNAME", l.Hostname, FromDefault)
	}
	if !l.Internal || !o.Has("VERB") {
		o.Set("VERB", verb, FromDefault)
	}
	return o, warnings, nil
}

// applyLegacy maps deprecated keys to their replacements when the
// replacement was not given at the same or a higher precedence.
func applyLegacy(o *Options) []string {
	var warnings []string
	for _, lg := range Legacy {
		v, ok := o.Lookup(lg.Old)
		if !ok || v == "" {
			continue
		}
		src := o.Source(lg.Old)
		if o.Has(lg.New) && o.Source(lg.New) >= src {
			continue
		}
		if lg.Warning != "" {
			warnings = append(warnings, lg.Warning)
		}
		o.Set(lg.New, v, src)
	}
	return warnings
}

// Op is one command-line option occurrence, in order.
type Op struct {
	Spec *Spec
	// Flag is the spelling used, e.g. "--raw" or "-L".
	Flag string
	// Arg is the flag's argument, if it takes one.
	Arg string
}

// ErrInvalidOption is wrapped by errors for rejected options.
var ErrInvalidOption = errors.New("invalid option")

// OptionError reports a rejected command-line option.
type OptionError struct{ Msg string }

func (e *OptionError) Error() string { return e.Msg }
func (e *OptionError) Unwrap() error { return ErrInvalidOption }

// ApplyOps applies command-line operations in order on top of base and
// returns the resulting values of every key the operations touched, along
// with deprecation warnings. Increment and decrement start from base; lists
// start empty, so command-line lists replace configured ones.
func ApplyOps(base *Options, ops []Op) (map[string]string, []string, error) {
	vals := map[string]string{}
	var warnings []string
	for _, op := range ops {
		s := op.Spec
		if s.Type == Invalid {
			return nil, warnings, &OptionError{s.Warning}
		}
		if s.Warning != "" {
			warnings = append(warnings, s.Warning)
		}
		cur, touched := vals[s.Key]
		switch s.Type {
		case True:
			vals[s.Key] = "1"
		case False:
			vals[s.Key] = "0"
		case Set:
			if s.Value != "" {
				vals[s.Key] = s.Value
			} else {
				vals[s.Key] = op.Arg
			}
		case ArgList:
			vals[s.Key] = joinNonEmpty(cur, op.Flag, " ")
		case List:
			vals[s.Key] = joinNonEmpty(cur, op.Arg, ",")
		case Incr, Decr:
			if !touched {
				cur = base.Get(s.Key)
			}
			n, _ := strconv.Atoi(strings.TrimSpace(cur))
			if s.Type == Incr {
				n++
			} else {
				n--
			}
			vals[s.Key] = strconv.Itoa(n)
		}
	}
	return vals, warnings, nil
}

// Apply sets command-line values on o.
func (o *Options) Apply(cli map[string]string) {
	for k, v := range cli {
		o.Set(k, v, FromCLI)
	}
}

func joinNonEmpty(a, b, sep string) string {
	if a == "" || b == "" {
		return a + b
	}
	return a + sep + b
}
