package match

import (
	"fmt"
	"regexp"
	"strings"
)

// Filter holds parsed --include / -X patterns (dataset + source-snapshot).
type Filter struct {
	// Active is true if any include or exclude pattern was provided.
	Active bool

	excludeExact map[string]bool
	excludeDS    []*regexp.Regexp
	excludeSnap  []*regexp.Regexp

	includeExact map[string]bool
	includeDS    []*regexp.Regexp
	includeSnap  []*regexp.Regexp
	hasInclude   bool
	hasIncludeDS bool

	Warnings []string
}

// ParseFilter builds a Filter from raw include/exclude tokens.
// Tokens may contain comma-separated patterns (no whitespace trim).
func ParseFilter(include, exclude []string) *Filter {
	f := &Filter{
		excludeExact: make(map[string]bool),
		includeExact: make(map[string]bool),
	}
	inc := flattenPatterns(include)
	exc := flattenPatterns(exclude)
	if len(inc) > 0 {
		f.hasInclude = true
		f.Active = true
	}
	if len(exc) > 0 {
		f.Active = true
	}
	for _, p := range exc {
		f.addPattern(p, true)
	}
	for _, p := range inc {
		f.addPattern(p, false)
	}
	return f
}

func flattenPatterns(chunks []string) []string {
	var out []string
	for _, c := range chunks {
		for _, p := range strings.Split(c, ",") {
			// Awk does not trim; keep empty only if truly empty token from ,,
			if p == "" {
				continue
			}
			out = append(out, p)
		}
	}
	return out
}

func (f *Filter) addPattern(pat string, exclude bool) {
	switch {
	case strings.HasPrefix(pat, "/") || (strings.HasPrefix(pat, "*") && strings.Contains(pat, "/")):
		re, err := globToRegexp(pat, "(/.*)?")
		if err != nil {
			f.Warnings = append(f.Warnings, fmt.Sprintf("invalid filter pattern '%s': %v", pat, err))
			return
		}
		if exclude {
			f.excludeDS = append(f.excludeDS, re)
		} else {
			f.includeDS = append(f.includeDS, re)
			f.hasIncludeDS = true
		}
	case strings.HasPrefix(pat, "@"):
		re, err := globToRegexp(pat, "")
		if err != nil {
			f.Warnings = append(f.Warnings, fmt.Sprintf("invalid filter pattern '%s': %v", pat, err))
			return
		}
		if exclude {
			f.excludeSnap = append(f.excludeSnap, re)
		} else {
			f.includeSnap = append(f.includeSnap, re)
		}
	case strings.ContainsAny(pat, "*?"):
		// Oracle: must start with '@' or include '/'
		f.Warnings = append(f.Warnings, fmt.Sprintf("invalid filter pattern '%s' must start with '@' or include '/'", pat))
	default:
		if exclude {
			f.excludeExact[pat] = true
		} else {
			f.includeExact[pat] = true
			f.hasIncludeDS = true
		}
	}
}

// globToRegexp converts shell-ish glob to anchored regex; suffix is appended before $.
func globToRegexp(glob, suffix string) (*regexp.Regexp, error) {
	var b strings.Builder
	b.WriteByte('^')
	for i := 0; i < len(glob); i++ {
		c := glob[i]
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		case '\\', '^', '$', '.', '|', '(', ')', '[', ']', '{', '}', '+':
			b.WriteByte('\\')
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString(suffix)
	b.WriteByte('$')
	return regexp.Compile(b.String())
}

func regexAny(s string, pats []*regexp.Regexp) bool {
	for _, re := range pats {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func exactOrChild(m map[string]bool, name string) bool {
	if m[name] {
		return true
	}
	for exact := range m {
		if strings.HasPrefix(name, exact+"/") {
			return true
		}
	}
	return false
}

// dsExcluded reports whether a dataset row should be dropped.
func (f *Filter) dsExcluded(fullName, dsSuffix string) bool {
	if f == nil {
		return false
	}
	if exactOrChild(f.excludeExact, fullName) {
		return true
	}
	if dsSuffix != "" && regexAny(dsSuffix, f.excludeDS) {
		return true
	}
	// Root suffix "" — only exact/full-name excludes apply.
	return false
}

// dsIncluded reports whether a dataset passes INCLUDE (true if no DS include).
func (f *Filter) dsIncluded(fullName, dsSuffix string) bool {
	if f == nil || !f.hasInclude {
		return true
	}
	if !f.hasIncludeDS {
		return true // snap-only include: all datasets
	}
	if exactOrChild(f.includeExact, fullName) {
		return true
	}
	if f.includeExact[dsSuffix] || exactOrChild(f.includeExact, dsSuffix) {
		return true
	}
	if dsSuffix != "" && regexAny(dsSuffix, f.includeDS) {
		return true
	}
	// Root: relative patterns rarely match ""; allow exact root name only.
	if dsSuffix == "" && f.includeExact[fullName] {
		return true
	}
	return false
}

// keepDataset is the process_row gate for dataset objects (src and tgt).
func (f *Filter) keepDataset(fullName, dsSuffix string) bool {
	if f == nil || !f.Active {
		return true
	}
	if f.dsExcluded(fullName, dsSuffix) {
		return false
	}
	return f.dsIncluded(fullName, dsSuffix)
}

// keepSourceSnap is the process_row gate for source snapshots only.
func (f *Filter) keepSourceSnap(savepoint, fullDS, dsSuffix string) bool {
	if f == nil || !f.Active {
		return true
	}
	if regexAny(savepoint, f.excludeSnap) {
		return false
	}
	return f.snapOrDSIncluded(savepoint, fullDS, dsSuffix)
}

// KeepDatasetForPrune exports keepDataset for the prune package.
func (f *Filter) KeepDatasetForPrune(fullName, dsSuffix string) bool {
	return f.keepDataset(fullName, dsSuffix)
}

// KeepSourceSnap exports keepSourceSnap for the prune package.
func (f *Filter) KeepSourceSnap(savepoint, fullDS, dsSuffix string) bool {
	return f.keepSourceSnap(savepoint, fullDS, dsSuffix)
}

func (f *Filter) snapOrDSIncluded(savepoint, fullDS, dsSuffix string) bool {
	if !f.hasInclude {
		return true
	}
	if regexAny(savepoint, f.includeSnap) {
		return true
	}
	if f.hasIncludeDS && f.dsIncluded(fullDS, dsSuffix) {
		return true
	}
	if !f.hasIncludeDS {
		return false
	}
	return false
}
