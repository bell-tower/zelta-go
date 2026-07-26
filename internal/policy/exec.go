package policy

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

// RunResult holds the result of one job execution.
type RunResult struct {
	Job Job
	Err error
}

// Run executes backup jobs sequentially with retry.
// Retries failed jobs up to RETRY times (from each job's Options).
func Run(jobs []Job) []RunResult {
	ps := policyScopeSet()
	results := make([]RunResult, len(jobs))
	for i, j := range jobs {
		results[i] = RunResult{Job: j, Err: runOne(j, ps)}
	}
	results = retryFailed(jobs, results, ps)
	return results
}

// RunParallel executes backup jobs with up to n concurrent workers.
// Workers share the same retry count and retry logic.
func RunParallel(jobs []Job, n int) []RunResult {
	if n < 2 {
		return Run(jobs)
	}
	ps := policyScopeSet()
	results := runBatch(jobs, n, ps)
	results = retryFailedParallel(jobs, results, n, ps)
	return results
}

// runOne executes a single job via exec.Command.
func runOne(j Job, ps map[string]bool) error {
	args := []string{"backup", j.SourceEP(), j.Target}
	cmd := exec.Command("zelta", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), execEnv(j, ps)...)
	return cmd.Run()
}

// execEnv builds environment variables for a child backup process.
// Injects LOG_PREFIX per job; excludes policy-scope keys.
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

// retryFailed retries failed jobs in place, up to RETRY times.
func retryFailed(jobs []Job, results []RunResult, ps map[string]bool) []RunResult {
	retries := extractRetries(jobs)
	for attempt := 0; attempt < retries; attempt++ {
		var count int
		for i := range results {
			if results[i].Err != nil {
				results[i].Err = runOne(jobs[i], ps)
				count++
			}
		}
		if count == 0 {
			break
		}
	}
	return results
}

// retryFailedParallel retries failed jobs in parallel, up to RETRY times.
func retryFailedParallel(jobs []Job, results []RunResult, n int, ps map[string]bool) []RunResult {
	retries := extractRetries(jobs)
	for attempt := 0; attempt < retries; attempt++ {
		var failed []int
		for i := range results {
			if results[i].Err != nil {
				failed = append(failed, i)
			}
		}
		if len(failed) == 0 {
			break
		}
		// Re-run failed jobs in parallel
		mu := sync.Mutex{}
		var wg sync.WaitGroup
		ch := make(chan int, len(failed))
		for _, idx := range failed {
			ch <- idx
		}
		close(ch)
		for w := 0; w < n; w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for idx := range ch {
					err := runOne(jobs[idx], ps)
					mu.Lock()
					results[idx].Err = err
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
	}
	return results
}

// runBatch runs all jobs with up to n concurrent workers.
func runBatch(jobs []Job, n int, ps map[string]bool) []RunResult {
	results := make([]RunResult, len(jobs))
	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan int, len(jobs))
	for i := range jobs {
		ch <- i
	}
	close(ch)
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range ch {
				err := runOne(jobs[idx], ps)
				mu.Lock()
				results[idx] = RunResult{Job: jobs[idx], Err: err}
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	return results
}

// extractRetries returns the RETRY value from job options (first job wins).
func extractRetries(jobs []Job) int {
	if len(jobs) == 0 {
		return 0
	}
	if s := jobs[0].Options["RETRY"]; s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			return n
		}
	}
	return 0
}
