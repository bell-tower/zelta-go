package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/bell-tower/zelta-go/internal/config"
)

const endpointForm = "Each endpoint is in the form: [user@host:]pool[/dataset/][@snap]"

const docFooter = `For complete documentation:  zelta help
                             zelta help [<topic>]
                             zelta help options
                             https://zelta.space`

// rootUsage is the command overview printed by "zelta" without arguments,
// keeping the historical layout and adding newer commands.
func rootUsage(w io.Writer) {
	fmt.Fprintln(w, "usage: zelta command [OPTIONS]")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Where 'command' is one of the following:")
	fmt.Fprintln(w)
	for _, name := range []string{"match", "backup", "policy", "clone", "revert", "rotate", "lock", "unlock", "failover", "propsync"} {
		v, _ := findVerb(name)
		fmt.Fprintf(w, "  %-27s%s\n", v.name, v.short)
	}
	fmt.Fprintln(w)
	for _, name := range []string{"snapshot", "rebase", "prune"} {
		v, _ := findVerb(name)
		fmt.Fprintf(w, "  %-27s%s\n", v.name, v.short)
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %-27s%s\n", "version", "Show version information")
	fmt.Fprintf(w, "  %-27s%s\n", "completion", "Generate shell completion scripts")
	fmt.Fprintln(w)
	fmt.Fprintln(w, endpointForm)
	fmt.Fprintln(w)
	fmt.Fprintln(w, docFooter)
}

// rootHelp is the richer overview printed by "zelta --help".
func rootHelp(w io.Writer) {
	fmt.Fprintln(w, "zelta - safe, recursive ZFS replication, recovery, and retention")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "usage: zelta COMMAND [OPTIONS] [ENDPOINT ...]")
	for _, g := range groupOrder {
		fmt.Fprintf(w, "\n%s:\n", g)
		for _, v := range verbs {
			if v.group == g && !v.hidden {
				fmt.Fprintf(w, "  %-12s%s\n", v.name, v.short)
			}
		}
	}
	fmt.Fprintln(w, "\nOther commands:")
	fmt.Fprintf(w, "  %-12s%s\n", "help", "Show the manual for zelta or a TOPIC")
	fmt.Fprintf(w, "  %-12s%s\n", "replay", "Repeat a previous backup")
	fmt.Fprintf(w, "  %-12s%s\n", "version", "Show version information")
	fmt.Fprintf(w, "  %-12s%s\n", "completion", "Generate a bash, zsh, fish, or powershell completion script")
	fmt.Fprintln(w)
	fmt.Fprintln(w, endpointForm)
	fmt.Fprintln(w, "Run 'zelta COMMAND --help' for a command's options.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, docFooter)
}

func synopsis(v verb, prog string) string {
	return fmt.Sprintf("usage: %s [OPTIONS] %s", prog, v.usage)
}

// verbUsage is the short usage printed after usage errors.
func verbUsage(w io.Writer, v verb, prog string) {
	fmt.Fprintln(w, synopsis(v, prog))
	fmt.Fprintln(w)
	fmt.Fprintf(w, "Run '%s --help' for options, or 'zelta help %s' for the manual.\n", prog, helpTopic(v))
}

func helpTopic(v verb) string {
	if v.name == "replicate" || v.name == "sync" {
		return "backup"
	}
	return v.name
}

// verbHelp prints a command's synopsis, description, and options grouped as
// in the registry.
func verbHelp(w io.Writer, v verb, prog string) {
	fmt.Fprintln(w, synopsis(v, prog))
	fmt.Fprintln(w)
	fmt.Fprintln(w, wrap(v.long, 78))
	if v.operand == opEndpoint {
		fmt.Fprintln(w)
		fmt.Fprintln(w, endpointForm)
	}
	fs := spellings(v.optionVerb())
	byGroup := map[string][]*config.Spec{}
	for _, s := range fs.visible {
		g := s.Group
		if g == "" {
			g = config.GroupCommand
		}
		byGroup[g] = append(byGroup[g], s)
	}
	for _, g := range config.GroupOrder {
		specs := byGroup[g]
		if len(specs) == 0 {
			continue
		}
		fmt.Fprintf(w, "\n%s:\n", g)
		for _, s := range specs {
			fmt.Fprint(w, formatSpec(fs, s))
		}
	}
	if v.name == "zprune" {
		fmt.Fprintf(w, "\nzprune Options:\n  %-30s%s\n", "-V, --version", "show version information")
	}
	fmt.Fprintf(w, "\n  %-30s%s\n", "-h, --help", "show this help")
	fmt.Fprintln(w)
	fmt.Fprintf(w, "For complete documentation:  zelta help %s\n", helpTopic(v))
	fmt.Fprintln(w, "                             https://zelta.space")
}

func formatSpec(fs flagSet, s *config.Spec) string {
	owned := fs.owned(s)
	var shorts, longs []string
	for _, f := range owned {
		if strings.HasPrefix(f, "--") {
			longs = append(longs, f)
		} else {
			shorts = append(shorts, f)
		}
	}
	names := strings.Join(append(shorts, longs...), ", ")
	if s.Arg != "" {
		if s.Optional != "" {
			names += "[=" + s.Arg + "]"
		} else {
			names += " " + s.Arg
		}
	}
	desc := s.Desc
	if s.Type == config.ArgList {
		desc = "pass to zfs " + map[bool]string{true: "recv", false: "send"}[s.Key == "RECV_OVERRIDE"] + "; replaces automatic options"
	}
	if len(names) > 28 {
		return fmt.Sprintf("  %s\n  %-30s%s\n", wrapIndent(names, 76, "    "), "", desc)
	}
	return fmt.Sprintf("  %-30s%s\n", names, desc)
}

func wrap(s string, width int) string { return wrapIndent(s, width, "") }

func wrapIndent(s string, width int, indent string) string {
	var lines []string
	line := ""
	for word := range strings.FieldsSeq(s) {
		if line != "" && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line = indent + word
			continue
		}
		if line == "" {
			line = word
		} else {
			line += " " + word
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
