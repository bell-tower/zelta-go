package policy

import "strings"

// Filter keeps jobs matching any operand (OR). Empty operands keeps all.
// Match axes: site, host, source_remote, source dataset, target, source EP, source leaf.
func Filter(jobs []Job, operands []string) []Job {
	if len(operands) == 0 {
		return jobs
	}
	want := map[string]bool{}
	for _, o := range operands {
		if o != "" {
			want[o] = true
		}
	}
	var out []Job
	for _, j := range jobs {
		if jobMatches(j, want) {
			out = append(out, j)
		}
	}
	return out
}

func jobMatches(j Job, want map[string]bool) bool {
	leaf := j.Source
	if i := strings.LastIndex(leaf, "/"); i >= 0 {
		leaf = leaf[i+1:]
	}
	keys := []string{
		j.Site,
		j.Host,
		j.SourceRemote,
		j.Source,
		j.Target,
		j.SourceEP(),
		leaf,
	}
	for _, k := range keys {
		if k != "" && want[k] {
			return true
		}
	}
	return false
}
