package match

import (
	"regexp"
	"strings"
)

// Filter selects datasets and source snapshots by Zelta's include and
// exclude patterns (see zelta-options(7), INCLUDE AND EXCLUDE PATTERNS):
//
//   - "pool/ds"   an absolute dataset name and its descendants
//   - "/suffix"   a dataset relative to the endpoint, and its descendants
//   - "*/swap"    a relative glob; it must start with "/" or "*" and contain "/"
//   - "@name"     a snapshot name or glob
type Filter struct {
	excludeDS, includeDS     map[string]bool
	excludeDSRe, includeDSRe []*regexp.Regexp
	excludeSnap, includeSnap []*regexp.Regexp
	include                  bool
	// Warnings lists ignored patterns.
	Warnings []string
}

// NewFilter compiles comma-separated exclude and include pattern lists.
func NewFilter(exclude, include string) *Filter {
	f := &Filter{excludeDS: map[string]bool{}, includeDS: map[string]bool{}, include: include != ""}
	f.load(exclude, f.excludeDS, &f.excludeDSRe, &f.excludeSnap)
	f.load(include, f.includeDS, &f.includeDSRe, &f.includeSnap)
	return f
}

func (f *Filter) load(list string, names map[string]bool, dsRe, snapRe *[]*regexp.Regexp) {
	if list == "" {
		return
	}
	for pat := range strings.SplitSeq(list, ",") {
		switch {
		case strings.HasPrefix(pat, "/") || (strings.HasPrefix(pat, "*") && strings.Contains(pat, "/")):
			*dsRe = append(*dsRe, GlobRegexp(pat, "(/.*)?"))
		case strings.HasPrefix(pat, "@"):
			*snapRe = append(*snapRe, GlobRegexp(pat, ""))
		case strings.ContainsAny(pat, "*?"):
			f.Warnings = append(f.Warnings, "invalid filter pattern '"+pat+"' must start with '@' or include '/'")
		default:
			names[pat] = true
		}
	}
}

// GlobRegexp converts a glob ("*" and "?") to an anchored regular
// expression, appending suffix (itself a regular expression) before the end
// anchor.
func GlobRegexp(glob, suffix string) *regexp.Regexp {
	var b strings.Builder
	b.WriteByte('^')
	for _, r := range glob {
		switch r {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteByte('.')
		default:
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(suffix)
	b.WriteByte('$')
	return regexp.MustCompile(b.String())
}

func anyMatch(s string, res []*regexp.Regexp) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func underName(s string, names map[string]bool) bool {
	if names[s] {
		return true
	}
	for n := range names {
		if n != "" && strings.HasPrefix(s, n+"/") {
			return true
		}
	}
	return false
}

func (f *Filter) hasDSIncludes() bool { return len(f.includeDS) > 0 || len(f.includeDSRe) > 0 }

func (f *Filter) dsIncluded(s string) bool {
	if !f.include || !f.hasDSIncludes() {
		return true
	}
	return underName(s, f.includeDS) || anyMatch(s, f.includeDSRe)
}

// Dataset reports whether a dataset row is selected. name is the full dataset
// name and suffix is relative to the endpoint ("" for the top dataset).
func (f *Filter) Dataset(name, suffix string) bool {
	if underName(name, f.excludeDS) || anyMatch(suffix, f.excludeDSRe) {
		return false
	}
	return f.dsIncluded(name) || f.dsIncluded(suffix)
}

// Snapshot reports whether a source snapshot is selected. savepoint is the
// "@name" part; dsName and suffix identify its dataset.
func (f *Filter) Snapshot(savepoint, dsName, suffix string) bool {
	if anyMatch(savepoint, f.excludeSnap) {
		return false
	}
	if !f.include {
		return true
	}
	if anyMatch(savepoint, f.includeSnap) {
		return true
	}
	return f.hasDSIncludes() && (f.dsIncluded(dsName) || f.dsIncluded(suffix))
}

// Active reports whether any pattern was given.
func (f *Filter) Active() bool {
	return f.include || len(f.excludeDS) > 0 || len(f.excludeDSRe) > 0 || len(f.excludeSnap) > 0
}
