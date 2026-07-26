package policy

import (
	"fmt"
	"os"
	"os/exec"
)

// RunResult holds the result of one job execution.
type RunResult struct {
	Job Job
	Err error
}

// Run executes backup jobs sequentially. Each job is run as:
//
//	zelta backup '<source>' '<target>'
//
// with non-policy-scope ZELTA_* options forwarded as environment variables.
// LOG_PREFIX is injected per job.
func Run(jobs []Job) []RunResult {
	ps := policyScopeSet()
	results := make([]RunResult, 0, len(jobs))
	for _, j := range jobs {
		err := runOne(j, ps)
		results = append(results, RunResult{Job: j, Err: err})
	}
	return results
}

func runOne(j Job, ps map[string]bool) error {
	args := []string{"backup", j.SourceEP(), j.Target}
	cmd := exec.Command("zelta", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), execEnv(j, ps)...)
	return cmd.Run()
}

func execEnv(j Job, ps map[string]bool) []string {
	opts := copyMap(j.Options)
	opts["LOG_PREFIX"] = fmt.Sprintf("[%s: %s] %s: ", j.Site, j.Target, j.SourceEP())
	var env []string
	for _, k := range sortedKeys(opts) {
		if ps[k] || opts[k] == "" {
			continue
		}
		env = append(env, "ZELTA_"+k+"="+opts[k])
	}
	return env
}
