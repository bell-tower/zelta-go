package policy

import (
	"fmt"
	"strconv"
	"strings"
)

// Job is one resolved backup pair from a policy config.
type Job struct {
	Site         string
	Host         string // hostname without user@
	SourceRemote string // host key as written (may be user@host)
	Source       string // dataset path
	Target       string // fully resolved target endpoint
	Options      map[string]string
}

// SourceEP returns host:dataset for the job source.
func (j Job) SourceEP() string {
	return j.SourceRemote + ":" + j.Source
}

// Load reads and parses a policy config file into ordered jobs.
// override seeds global options and blocks conf from replacing those keys
// (process env / CLI). Values should already be canonical key names.
func Load(path string, override map[string]string) ([]Job, []string, error) {
	lines, err := expandFile(path)
	if err != nil {
		return nil, nil, err
	}
	return parseLines(lines, override)
}

func parseLines(lines []line, override map[string]string) ([]Job, []string, error) {
	global := map[string]string{}
	blocked := map[string]bool{}
	for k, v := range override {
		ck, err := canonicalize(k)
		if err != nil {
			// Non-option env keys are ignored as overrides.
			continue
		}
		blocked[ck] = true
		if v != "" {
			global[ck] = normalizeValue(ck, v)
		}
	}
	var siteOpt map[string]string
	var hostOpt map[string]string
	context := "global"
	var site, sourceRemote, host string
	var jobs []Job
	var warns []string

	set := func(dst map[string]string, name, val string) error {
		k, err := canonicalize(name)
		if err != nil {
			return err
		}
		if blocked[k] {
			return nil
		}
		dst[k] = normalizeValue(k, val)
		return nil
	}

	for _, ln := range lines {
		code := strings.TrimRight(stripComment(ln.text), " \t")
		if code == "" {
			continue
		}
		parseErr := func() error {
			return fmt.Errorf("configuration parse error at %s:%d: %s", ln.file, ln.lineNum, strings.TrimLeft(code, " \t"))
		}

		// Global options: KEY: value at column 0
		if key, val, ok := matchKV(code, 0); ok {
			if err := set(global, key, val); err != nil {
				return nil, warns, err
			}
			continue
		}
		// Sites: Name: at column 0
		if name, ok := matchKeyOnly(code, 0); ok {
			context = "site"
			site = name
			siteOpt = copyMap(global)
			continue
		}
		// Site-level options: two-space KEY: value
		if key, val, ok := matchKV(code, 2); ok {
			if siteOpt == nil {
				return nil, warns, parseErr()
			}
			if err := set(siteOpt, key, val); err != nil {
				return nil, warns, err
			}
			continue
		}
		// Hosts: two-space host:
		if name, ok := matchKeyOnly(code, 2); ok {
			if context == "global" {
				return nil, warns, parseErr()
			}
			context = "host"
			sourceRemote = name
			host = sourceHost(name)
			hostOpt = copyMap(siteOpt)
			continue
		}

		// options: / datasets: markers (any indent; AWK matches $2)
		if isSection(code, "options") {
			context = "options"
			continue
		}
		if isSection(code, "datasets") {
			context = "datasets"
			continue
		}

		// Host options under options:: six-space KEY: value
		if key, val, ok := matchKV(code, 6); ok {
			if context != "options" {
				return nil, warns, parseErr()
			}
			if err := set(hostOpt, key, val); err != nil {
				return nil, warns, err
			}
			continue
		}

		// Dataset list item
		if src, tgt, ok := matchListItem(code); ok {
			if context != "datasets" && context != "host" {
				return nil, warns, parseErr()
			}
			opt := copyMap(hostOpt)
			target := resolveTarget(tgt, opt, host, src)
			if target == "" {
				warns = append(warns, "no target defined for "+src)
				continue
			}
			jobs = append(jobs, Job{
				Site:         site,
				Host:         host,
				SourceRemote: sourceRemote,
				Source:       src,
				Target:       target,
				Options:      opt,
			})
			continue
		}

		return nil, warns, parseErr()
	}
	return jobs, warns, nil
}

func sourceHost(sourceRemote string) string {
	if i := strings.LastIndex(sourceRemote, "@"); i >= 0 {
		return sourceRemote[i+1:]
	}
	return sourceRemote
}

func resolveTarget(explicit string, opt map[string]string, host, source string) string {
	if explicit != "" {
		return explicit
	}
	tgt := opt["BACKUP_ROOT"]
	if tgt == "" {
		return ""
	}
	if truthyPrefix(opt["ADD_HOST_PREFIX"]) && host != "" {
		tgt = tgt + "/" + host
	}
	prefix := 0
	if s := opt["ADD_DATASET_PREFIX"]; s != "" {
		if n, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
			prefix = n
		}
	}
	segs := strings.Split(source, "/")
	n := len(segs)
	start := n - prefix
	for i := start; i <= n; i++ {
		// AWK is 1-indexed; segments[_i] for _i from n-prefix..n
		// Go 0-indexed: i from start..n where segs index is i-1 when i>=1
		if i < 1 || i > n {
			continue
		}
		seg := segs[i-1]
		if seg != "" {
			tgt = tgt + "/" + seg
		}
	}
	return tgt
}

func truthyPrefix(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n != 0
	}
	switch strings.ToLower(v) {
	case "0", "no", "false", "off":
		return false
	default:
		return true
	}
}

func copyMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// matchKV matches indent spaces then KEY: value (value non-empty after colon+space).
func matchKV(code string, indent int) (key, val string, ok bool) {
	if !hasIndent(code, indent) {
		return "", "", false
	}
	rest := code[indent:]
	if rest == "" || rest[0] == ' ' {
		return "", "", false
	}
	// KEY: value — require colon + space (or tabs) and non-empty value
	i := strings.Index(rest, ":")
	if i <= 0 {
		return "", "", false
	}
	key = rest[:i]
	if strings.ContainsAny(key, " \t") {
		return "", "", false
	}
	after := rest[i+1:]
	if after == "" || (after[0] != ' ' && after[0] != '\t') {
		return "", "", false
	}
	val = strings.TrimSpace(after)
	if val == "" {
		return "", "", false
	}
	return key, val, true
}

func matchKeyOnly(code string, indent int) (name string, ok bool) {
	if !hasIndent(code, indent) {
		return "", false
	}
	rest := code[indent:]
	if rest == "" || rest[0] == ' ' {
		return "", false
	}
	if !strings.HasSuffix(rest, ":") {
		return "", false
	}
	name = rest[:len(rest)-1]
	if name == "" || strings.ContainsAny(name, " \t") {
		return "", false
	}
	// Must not be KEY: value
	if strings.Contains(name, ":") {
		return "", false
	}
	return name, true
}

func hasIndent(code string, indent int) bool {
	if len(code) < indent {
		return false
	}
	for i := 0; i < indent; i++ {
		if code[i] != ' ' {
			return false
		}
	}
	return true
}

func isSection(code, name string) bool {
	t := strings.TrimLeft(code, " ")
	return t == name+":"
}

// matchListItem parses "  - source" or "  - source: target" (any leading spaces before -).
// Colon split only on ": " / ":\t" / trailing ":" so host:path targets stay intact.
func matchListItem(code string) (source, target string, ok bool) {
	t := strings.TrimLeft(code, " ")
	if !strings.HasPrefix(t, "- ") && !strings.HasPrefix(t, "-\t") {
		if t == "-" {
			return "", "", false
		}
		if !strings.HasPrefix(t, "-") {
			return "", "", false
		}
		// "-source" without space: AWK FS "- " requires space; reject
		return "", "", false
	}
	// strip leading spaces and "- "
	rest := strings.TrimSpace(t[1:])
	if rest == "" {
		return "", "", false
	}
	// Split on first ": " or ":\t" or trailing ":"
	src, tgt, cut := splitDatasetTarget(rest)
	if !cut {
		return rest, "", true
	}
	return src, tgt, true
}

func splitDatasetTarget(s string) (source, target string, ok bool) {
	for i := 0; i < len(s); i++ {
		if s[i] != ':' {
			continue
		}
		// trailing :
		if i == len(s)-1 {
			return s[:i], "", true
		}
		if s[i+1] == ' ' || s[i+1] == '\t' {
			return s[:i], strings.TrimSpace(s[i+1:]), true
		}
	}
	return s, "", false
}
