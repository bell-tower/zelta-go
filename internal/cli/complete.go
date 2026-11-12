package cli

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/bell-tower/zelta-go/internal/command"
	"github.com/bell-tower/zelta-go/internal/config"
)

// completionTimeout bounds zfs and ssh calls made while completing.
var completionTimeout = 4 * time.Second

// registerCompletions attaches operand and flag-value completion to a verb.
func (a *App) registerCompletions(cmd *cobra.Command, v verb) {
	cmd.ValidArgsFunction = func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if v.maxOps > 0 && len(args) >= v.maxOps {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		switch v.operand {
		case opEndpoint:
			return a.completeEndpoint(cmd.Context(), toComplete)
		case opPolicy:
			return a.completePolicy(toComplete), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	fs := spellings(v.optionVerb())
	for _, s := range fs.visible {
		if !s.TakesArg() {
			continue
		}
		fn := func(cmd *cobra.Command, _ []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			switch {
			case len(s.Values) > 0:
				return s.Values, cobra.ShellCompDirectiveNoFileComp
			case s.Complete == "file":
				return nil, cobra.ShellCompDirectiveDefault
			case s.Complete == "endpoint":
				return a.completeEndpoint(cmd.Context(), toComplete)
			case s.Complete == "columns":
				return a.completeColumns(toComplete), cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
			}
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		for _, f := range fs.owned(s) {
			name := strings.TrimPrefix(f, "--")
			if !strings.HasPrefix(f, "--") {
				name = syntheticName(f)
				if pf := cmd.Flags().ShorthandLookup(strings.TrimPrefix(f, "-")); pf != nil {
					name = pf.Name
				}
			}
			_ = cmd.RegisterFlagCompletionFunc(name, fn)
		}
	}
}

// completeColumns completes the last element of a comma-separated list of
// "zelta match" columns.
func (a *App) completeColumns(toComplete string) []string {
	head := ""
	if i := strings.LastIndex(toComplete, ","); i >= 0 {
		head = toComplete[:i+1]
	}
	var out []string
	names := []string{"all"}
	if a.Columns != nil {
		names = append(names, a.Columns()...)
	}
	for _, n := range dedupe(names) {
		out = append(out, head+n)
	}
	return filterPrefix(out, toComplete)
}

func (a *App) completePolicy(toComplete string) []string {
	loader := a.NewLoader()
	path := loader.Paths().Config
	if p := loader.Env[config.EnvPrefix+"CONFIG"]; p != "" {
		path = p
	}
	if a.PolicyObjects == nil {
		return nil
	}
	return filterPrefix(a.PolicyObjects(path), toComplete)
}

// completeEndpoint completes [user@host:]pool/dataset[@snapshot]. Without a
// host or a slash, it offers local pools and ssh hosts (with a trailing
// colon); otherwise it lists child datasets or snapshots on the host.
func (a *App) completeEndpoint(ctx context.Context, toComplete string) ([]string, cobra.ShellCompDirective) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, completionTimeout)
	defer cancel()
	host, path := splitHost(toComplete)
	prefix := ""
	if host != "" {
		prefix = host + ":"
	}
	directive := cobra.ShellCompDirectiveNoFileComp | cobra.ShellCompDirectiveNoSpace
	if host == "" && !strings.ContainsAny(path, "/@") {
		cands := a.zfsNames(ctx, "", "list", "-H", "-o", "name", "-d", "0")
		for _, h := range sshHosts() {
			cands = append(cands, h+":")
		}
		return filterPrefix(dedupe(cands), toComplete), directive
	}
	var names []string
	if ds, _, ok := strings.Cut(path, "@"); ok {
		names = a.zfsNames(ctx, host, "list", "-H", "-o", "name", "-t", "snapshot", "-d", "1", ds)
		directive = cobra.ShellCompDirectiveNoFileComp
	} else {
		parent := path
		if i := strings.LastIndex(path, "/"); i >= 0 {
			parent = path[:i]
		}
		if parent == "" {
			names = a.zfsNames(ctx, host, "list", "-H", "-o", "name", "-d", "0")
		} else {
			names = a.zfsNames(ctx, host, "list", "-H", "-o", "name", "-t", "filesystem,volume", "-d", "1", parent)
		}
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, prefix+n)
	}
	return filterPrefix(out, toComplete), directive
}

// zfsNames runs a read-only zfs command locally or on host and returns the
// first column of its output.
func (a *App) zfsNames(ctx context.Context, host string, args ...string) []string {
	c := command.New(append([]command.Arg{command.Word("zfs")}, command.Words(args...)...)...)
	if host != "" {
		remote := "ssh -n"
		if loader := a.NewLoader(); loader != nil {
			if r := loader.EnvOptions()["REMOTE_DEFAULT"]; r != "" {
				remote = r
			}
		}
		r := command.NewRemote(remote, host)
		if filepath.Base(r.Prefix[0]) == "ssh" {
			r.Prefix = append(r.Prefix, "-o", "BatchMode=yes", "-o", "ConnectTimeout=3")
		}
		c = c.On(r)
	}
	res := a.Exec.Run(ctx, c)
	var out []string
	for _, l := range res.Stdout() {
		if name, _, _ := strings.Cut(l, "\t"); name != "" {
			out = append(out, name)
		}
	}
	return out
}

// splitHost separates a "[user@]host:" or "[user@][ipv6]:" prefix.
func splitHost(s string) (host, path string) {
	if strings.HasPrefix(s, "[") || strings.Contains(s, "@[") {
		if i := strings.Index(s, "]:"); i >= 0 {
			return s[:i+1], s[i+2:]
		}
		return "", s
	}
	if i := strings.Index(s, ":"); i >= 0 && !strings.Contains(s[:i], "/") {
		h := s[:i]
		if h == "localhost" {
			return "", s[i+1:]
		}
		return h, s[i+1:]
	}
	return "", s
}

// sshHosts lists concrete host names from ~/.ssh/config and known_hosts.
func sshHosts() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	var hosts []string
	if data, err := os.ReadFile(filepath.Join(home, ".ssh", "config")); err == nil {
		for line := range strings.Lines(string(data)) {
			fields := strings.Fields(line)
			if len(fields) < 2 || !strings.EqualFold(fields[0], "host") {
				continue
			}
			for _, h := range fields[1:] {
				if !strings.ContainsAny(h, "*?!") {
					hosts = append(hosts, h)
				}
			}
		}
	}
	if data, err := os.ReadFile(filepath.Join(home, ".ssh", "known_hosts")); err == nil {
		for line := range strings.Lines(string(data)) {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "|") || strings.HasPrefix(line, "@") {
				continue
			}
			field, _, _ := strings.Cut(line, " ")
			for h := range strings.SplitSeq(field, ",") {
				if strings.HasPrefix(h, "[") {
					continue
				}
				hosts = append(hosts, h)
			}
		}
	}
	return dedupe(hosts)
}

func filterPrefix(cands []string, prefix string) []string {
	var out []string
	for _, c := range cands {
		if strings.HasPrefix(c, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func dedupe(in []string) []string {
	out := slices.Clone(in)
	sort.Strings(out)
	return slices.Compact(out)
}
