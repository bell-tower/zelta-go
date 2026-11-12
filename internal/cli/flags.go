package cli

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/bell-tower/zelta-go/internal/config"
)

// noArg is the value pflag passes to flags that take no argument.
const noArg = "\x00"

// parser collects option occurrences in command-line order.
type parser struct {
	ops []config.Op
	err string // Zelta-style message for the last rejected value
}

// opValue is a pflag.Value that records an Op for one flag spelling.
type opValue struct {
	p    *parser
	spec *config.Spec
	flag string
}

func (v *opValue) String() string { return "" }

func (v *opValue) Type() string {
	if v.takesArg() {
		return "string"
	}
	return "bool"
}

func (v *opValue) takesArg() bool { return v.spec.TakesArg() || v.spec.Optional != "" }

func (v *opValue) Set(s string) error {
	if !v.takesArg() {
		if s != noArg {
			v.p.err = fmt.Sprintf("invalid option assignment '%s=%s'", v.flag, s)
			return fmt.Errorf("%s", v.p.err)
		}
		s = ""
	}
	v.p.ops = append(v.p.ops, config.Op{Spec: v.spec, Flag: v.flag, Arg: s})
	return nil
}

// flagSet describes the flags registered for a verb.
type flagSet struct {
	// specs maps each accepted spelling to its spec.
	specs map[string]*config.Spec
	// visible lists the specs shown in help, in registry order.
	visible []*config.Spec
	// helpShort is "h" when -h means help for this verb.
	helpShort string
}

// spellings resolves which spec owns each flag spelling for a verb; later
// registry entries override earlier ones (e.g. "-h" in backup verbs).
func spellings(optVerb string) flagSet {
	fs := flagSet{specs: map[string]*config.Spec{}}
	for _, s := range config.SpecsFor(optVerb) {
		for _, f := range s.Flags {
			fs.specs[f] = s
		}
	}
	seen := map[*config.Spec]bool{}
	for _, s := range config.SpecsFor(optVerb) {
		if seen[s] {
			continue
		}
		seen[s] = true
		owned := false
		for _, f := range s.Flags {
			if fs.specs[f] == s {
				owned = true
			}
		}
		if owned && !s.Hidden && s.Key != "USAGE" {
			fs.visible = append(fs.visible, s)
		}
	}
	if s := fs.specs["-h"]; s != nil && s.Key == "USAGE" && s.Warning == "" {
		fs.helpShort = "h"
	}
	return fs
}

// owned returns the spellings of s that it owns in this flag set.
func (fs flagSet) owned(s *config.Spec) []string {
	var out []string
	for _, f := range s.Flags {
		if fs.specs[f] == s {
			out = append(out, f)
		}
	}
	return out
}

// registerFlags adds the option registry's flags for optVerb to cmd.
func registerFlags(cmd *cobra.Command, optVerb string, p *parser) flagSet {
	fs := spellings(optVerb)
	flags := cmd.Flags()
	flags.SortFlags = false
	flags.BoolP("help", fs.helpShort, false, "show help")
	seen := map[*config.Spec]bool{}
	for _, s := range config.SpecsFor(optVerb) {
		if seen[s] || s.Key == "USAGE" && s.Warning == "" {
			continue
		}
		seen[s] = true
		var longs, shorts []string
		for _, f := range fs.owned(s) {
			if strings.HasPrefix(f, "--") {
				longs = append(longs, f)
			} else {
				shorts = append(shorts, f)
			}
		}
		if s.Type == config.ArgList {
			// Each spelling is passed through literally, so each needs its
			// own flag to know which one was typed.
			for _, l := range longs {
				addFlag(flags, p, s, strings.TrimPrefix(l, "--"), "", l, s.Hidden)
			}
			for _, sh := range shorts {
				addFlag(flags, p, s, syntheticName(sh), strings.TrimPrefix(sh, "-"), sh, true)
			}
			continue
		}
		for i, l := range longs {
			short := ""
			if i == 0 && len(shorts) > 0 {
				short, shorts = strings.TrimPrefix(shorts[0], "-"), shorts[1:]
			}
			addFlag(flags, p, s, strings.TrimPrefix(l, "--"), short, l, s.Hidden || i > 0)
		}
		for _, sh := range shorts {
			addFlag(flags, p, s, syntheticName(sh), strings.TrimPrefix(sh, "-"), sh, true)
		}
	}
	return fs
}

func syntheticName(short string) string { return "zelta-short-" + strings.TrimPrefix(short, "-") }

func addFlag(flags *pflag.FlagSet, p *parser, s *config.Spec, name, short, spelling string, hidden bool) {
	v := &opValue{p: p, spec: s, flag: spelling}
	f := flags.VarPF(v, name, short, s.Desc)
	switch {
	case s.Optional != "":
		f.NoOptDefVal = s.Optional
	case !v.takesArg():
		f.NoOptDefVal = noArg
	}
	f.Hidden = hidden
}

// optionalValueFlags accept "--flag N" as well as "--flag=N".
var numericArg = regexp.MustCompile(`^-?[0-9]+$`)

// preprocess rewrites arguments pflag cannot parse directly: "-?" becomes
// "--help", and optional-value flags followed by a number are joined.
func preprocess(args []string, optVerb string) []string {
	fs := spellings(optVerb)
	out := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(out, args[i:]...)
		}
		if a == "-?" {
			out = append(out, "--help")
			continue
		}
		if s := fs.specs[a]; s != nil && s.Optional != "" && i+1 < len(args) && numericArg.MatchString(args[i+1]) {
			out = append(out, a+"="+args[i+1])
			i++
			continue
		}
		out = append(out, a)
	}
	return out
}

var (
	unknownLong  = regexp.MustCompile(`^unknown flag: (--?\S+)`)
	unknownShort = regexp.MustCompile(`^unknown shorthand flag: '(.)' in (-\S+)`)
	needsArgLong = regexp.MustCompile(`^flag needs an argument: (--\S+)`)
	needsArgShrt = regexp.MustCompile(`^flag needs an argument: '(.)' in -\S+`)
	badSyntax    = regexp.MustCompile(`^bad flag syntax: (\S+)`)
)

// flagError translates pflag errors into Zelta's historical messages.
func flagError(p *parser, err error) string {
	msg := err.Error()
	if p.err != "" {
		return p.err
	}
	if m := unknownLong.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("invalid option '%s'", strings.SplitN(m[1], "=", 2)[0])
	}
	if m := unknownShort.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("invalid option '-%s'", m[1])
	}
	if m := needsArgLong.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("option '%s' requires an argument", m[1])
	}
	if m := needsArgShrt.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("option '-%s' requires an argument", m[1])
	}
	if m := badSyntax.FindStringSubmatch(msg); m != nil {
		return fmt.Sprintf("invalid option: '%s'", m[1])
	}
	return msg
}
