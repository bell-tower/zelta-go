package policy

import (
	"github.com/bell-tower/zelta-go/data"
	"github.com/bell-tower/zelta-go/zconf"
)

// Job is the resolved backup-pair type from a policy document (re-export).
type Job = zconf.Job

// Load parses a policy config via the public zconf import.
func Load(path string, override map[string]string) ([]Job, []string, error) {
	return zconf.Load(path, override)
}

// policyScopeSet returns the set of policy-only keys (not forwarded to backup).
// Local copy of the zconf analysis so formatting/exec stay consumer-side.
func policyScopeSet() map[string]bool {
	rows, err := data.Table()
	if err != nil {
		return nil
	}
	out := map[string]bool{}
	for _, r := range rows {
		if r.Key == "" || !r.AppliesTo("policy") {
			continue
		}
		if r.Verbs == "policy" {
			out[r.Key] = true
		}
	}
	return out
}

func copyMap(src map[string]string) map[string]string {
	dst := make(map[string]string, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
