package opt

import (
	"fmt"
	"strconv"
	"strings"
)

// Parsed is the result of resolving CLI args over the seeded environment.
type Parsed struct {
	Env      Env             // defaults + zelta.env/process env + CLI
	Operands []string        // bare operands (flags end at first one, or --)
	Warnings []string        // deprecation/legacy warnings to print
	Changed  map[string]bool // keys explicitly set by CLI flags
	Usage    bool            // USAGE flag seen (-h, --help, -?)
}

// Parse resolves verb CLI args per the oracle zelta-args.awk contract:
// exact flag match only; --opt=value / --opt value; short bundling (-nq);
// a value-taking short flag consumes the rest of its cluster (-d2) or the
// next argv; "--" or the first bare operand ends flag parsing.
func Parse(verb string, argv []string) (*Parsed, error) {
	rows, err := Table()
	if err != nil {
		return nil, err
	}
	byFlag := map[string]*Row{}
	for i := range rows {
		r := &rows[i]
		if !r.AppliesTo(verb) {
			continue
		}
		for _, f := range r.Flags {
			if f != "" { // legacy rows have no flags
				byFlag[f] = r
			}
		}
	}
	env, warns := seed()
	p := &Parsed{Env: env, Changed: map[string]bool{}, Warnings: warns}

	i := 0
loop:
	for i = 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--":
			i++
			break loop
		case len(a) < 2 || a[0] != '-':
			break loop // first bare operand ends flag parsing
		case strings.HasPrefix(a, "--"):
			if err := p.longFlag(byFlag, a, argv, &i); err != nil {
				return nil, err
			}
		default:
			if err := p.shortCluster(byFlag, a, argv, &i); err != nil {
				return nil, err
			}
		}
	}
	p.Operands = argv[i:]
	p.Usage = p.Env.Bool("USAGE", false)
	return p, nil
}

// longFlag handles --name[=value]; may consume argv[*i+1] for a value.
func (p *Parsed) longFlag(byFlag map[string]*Row, a string, argv []string, i *int) error {
	name, val, hasEq := strings.Cut(a, "=")
	r := byFlag[name]
	if r == nil {
		return fmt.Errorf("invalid option '%s'", name)
	}
	if r.Warn != "" {
		p.Warnings = append(p.Warnings, r.Warn)
	}
	switch r.Type {
	case "invalid":
		if hasEq {
			return fmt.Errorf("invalid option assignment '%s'", a)
		}
		return invalidErr(r, name)
	case "true", "false", "incr", "decr", "arglist":
		if hasEq {
			return fmt.Errorf("invalid option assignment '%s'", a)
		}
		p.apply(r, name)
	case "set":
		if r.Value != "" {
			if hasEq {
				return fmt.Errorf("invalid option assignment '%s'", a)
			}
			p.apply(r, "")
		} else {
			v, err := flagValue(val, hasEq, name, argv, i)
			if err != nil {
				return err
			}
			p.apply(r, v)
		}
	case "list":
		v, err := flagValue(val, hasEq, name, argv, i)
		if err != nil {
			return err
		}
		p.apply(r, v)
	default:
		return fmt.Errorf("opts.tsv: unknown type %q for %s", r.Type, name)
	}
	return nil
}

// shortCluster handles -abc; a value-taking flag consumes the rest of the
// cluster or argv[*i+1] and ends the cluster.
func (p *Parsed) shortCluster(byFlag map[string]*Row, a string, argv []string, i *int) error {
	cluster := a[1:]
	for j := 0; j < len(cluster); j++ {
		name := "-" + string(cluster[j])
		r := byFlag[name]
		if r == nil {
			return fmt.Errorf("invalid option '%s'", name)
		}
		if r.Warn != "" {
			p.Warnings = append(p.Warnings, r.Warn)
		}
		switch r.Type {
		case "invalid":
			return invalidErr(r, name)
		case "true", "false", "incr", "decr":
			p.apply(r, "")
		case "set":
			if r.Value != "" {
				p.apply(r, "")
				continue
			}
			v := cluster[j+1:]
			if v == "" {
				var err error
				v, err = flagValue("", false, name, argv, i)
				if err != nil {
					return err
				}
			}
			p.apply(r, v)
			return nil // cluster consumed
		case "list":
			v := cluster[j+1:]
			if v == "" {
				var err error
				v, err = flagValue("", false, name, argv, i)
				if err != nil {
					return err
				}
			}
			p.apply(r, v)
			return nil
		case "arglist":
			p.apply(r, name)
		default:
			return fmt.Errorf("opts.tsv: unknown type %q for %s", r.Type, name)
		}
	}
	return nil
}

func invalidErr(r *Row, name string) error {
	if r.Warn != "" {
		return fmt.Errorf("%s", r.Warn)
	}
	return fmt.Errorf("invalid option '%s'", name)
}

// flagValue takes the =value or the next argv element.
func flagValue(val string, hasEq bool, name string, argv []string, i *int) (string, error) {
	if hasEq {
		return val, nil
	}
	if *i+1 >= len(argv) {
		return "", fmt.Errorf("option '%s' requires a value", name)
	}
	*i++
	return argv[*i], nil
}

// apply records one operation per the row type.
func (p *Parsed) apply(r *Row, v string) {
	k := r.Key
	p.Changed[k] = true
	switch r.Type {
	case "true":
		p.Env[k] = "1"
	case "false":
		p.Env[k] = "0"
	case "set":
		if r.Value != "" {
			p.Env[k] = r.Value
		} else {
			p.Env[k] = v
		}
	case "list":
		if cur := p.Env[k]; cur != "" {
			p.Env[k] = cur + "," + v
		} else {
			p.Env[k] = v
		}
	case "arglist":
		if cur := p.Env[k]; cur != "" {
			p.Env[k] = cur + " " + v
		} else {
			p.Env[k] = v
		}
	case "incr", "decr":
		n, _ := strconv.Atoi(strings.TrimSpace(p.Env[k]))
		if r.Type == "incr" {
			n++
		} else {
			n--
		}
		p.Env[k] = strconv.Itoa(n)
	}
}
