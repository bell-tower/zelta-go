package policy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/bell-tower/zelta-go/internal/app"
	"github.com/bell-tower/zelta-go/internal/config"
	"github.com/bell-tower/zelta-go/internal/endpoint"
)

// StoreVersion is the schema version of a policy store.
const StoreVersion = 1

// StoreName is the default policy store file name in the Zelta etc
// directory.
const StoreName = "policy.json"

// Store is the canonical policy: a set of backup jobs. Its encoding is an
// implementation detail; the schema is the contract.
type Store struct {
	Version int        `json:"version"`
	Jobs    []StoreJob `json:"jobs"`
}

// StoreJob is one backup from Source to Target. A source and target pair
// identifies the job.
type StoreJob struct {
	Source string `json:"source"`
	Target string `json:"target"`
	// Group selects jobs on the command line, as sites did in zelta.conf.
	Group string `json:"group,omitempty"`
	// Options are option keys (without ZELTA_) applied to the backup.
	Options map[string]string `json:"options,omitempty"`
}

// Label describes the job as "source -> target".
func (j StoreJob) Label() string { return j.Source + " -> " + j.Target }

// IsStore reports whether path names a policy store rather than a
// zelta.conf file.
func IsStore(path string) bool { return strings.EqualFold(filepath.Ext(path), ".json") }

// JobOption reports whether key may be saved in a job: a backup option that
// is not about output, or a policy option that applies per job.
func JobOption(key string) bool {
	if key == "RETRY" || key == "BACKUP_COMMAND" {
		return true // per-job policy options
	}
	if !config.PolicyKey(key) || config.PolicyScoped(key) {
		return false
	}
	for _, s := range config.Specs {
		if s.Key == key && s.Group == config.GroupOutput {
			return false
		}
	}
	return key != "LOG_PREFIX" && key != "CONFIG"
}

// Validate checks the schema: version, endpoints, option keys, and unique
// source and target pairs.
func (s *Store) Validate() error {
	if s.Version != StoreVersion {
		return fmt.Errorf("unsupported policy version %d", s.Version)
	}
	seen := map[string]bool{}
	for _, j := range s.Jobs {
		if j.Source == "" || j.Target == "" {
			return fmt.Errorf("job needs a source and a target: %s", j.Label())
		}
		for _, ep := range []string{j.Source, j.Target} {
			if e := endpoint.Parse(ep, "", ""); e.DS == "" || e.Snap != "" {
				return fmt.Errorf("invalid endpoint '%s'", ep)
			}
		}
		if seen[j.Label()] {
			return fmt.Errorf("duplicate job: %s", j.Label())
		}
		seen[j.Label()] = true
		for k := range j.Options {
			if !JobOption(k) {
				return fmt.Errorf("%s: option '%s' cannot be saved in a job", j.Label(), k)
			}
		}
	}
	return nil
}

// LoadStore reads and validates a policy store. A missing file is an empty
// store.
func LoadStore(path string) (*Store, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Store{Version: StoreVersion}, nil
	}
	if err != nil {
		return nil, err
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	if err := s.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %v", path, err)
	}
	return &s, nil
}

// Save validates and atomically writes the store.
func (s *Store) Save(path string) error {
	if err := s.Validate(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".policy-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(append(data, '\n')); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

// Put adds or replaces the job with the same source and target and returns
// the replaced job, if any.
func (s *Store) Put(j StoreJob) (old *StoreJob) {
	for i := range s.Jobs {
		if s.Jobs[i].Label() == j.Label() {
			prev := s.Jobs[i]
			s.Jobs[i] = j
			return &prev
		}
	}
	s.Jobs = append(s.Jobs, j)
	return nil
}

// Remove deletes the job with this label.
func (s *Store) Remove(label string) {
	s.Jobs = slices.DeleteFunc(s.Jobs, func(j StoreJob) bool { return j.Label() == label })
}

// Find returns the jobs whose label contains pattern.
func (s *Store) Find(pattern string) []StoreJob {
	var out []StoreJob
	for _, j := range s.Jobs {
		if strings.Contains(j.Label(), pattern) {
			out = append(out, j)
		}
	}
	return out
}

// addStore adds the store's jobs to the parser, as zelta.conf datasets.
func (p *parser) addStore(s *Store) {
	for _, sj := range s.Jobs {
		ep := endpoint.Parse(sj.Source, "", "")
		j := &Job{Site: sj.Group, SourceRemote: ep.Remote, Source: ep.DS}
		if ep.Remote != "" {
			j.Host = ep.Host
		}
		opts := copyMap(p.global)
		for k, v := range sj.Options {
			if p.opts != nil && p.opts.Source(k) >= config.FromEnv {
				continue // the environment and command line override jobs
			}
			opts[k] = v
		}
		p.addJob(j, sj.Target, opts)
	}
}

// Import reads the jobs of a zelta.conf file into store form. Inherited
// options are flattened into each job.
func Import(path string) ([]StoreJob, error) {
	lines, err := loadLines(path, 0, map[string]bool{})
	if err != nil {
		return nil, err
	}
	p := newParser(nil, nil, nil)
	if err := p.parse(lines); err != nil {
		return nil, err
	}
	var out []StoreJob
	for _, j := range p.all {
		sj := StoreJob{Source: j.SourceEndpoint(), Target: j.Target, Group: j.Site}
		for k, v := range j.Opts {
			if v != "" && JobOption(k) {
				if sj.Options == nil {
					sj.Options = map[string]string{}
				}
				sj.Options[k] = v
			}
		}
		out = append(out, sj)
	}
	return out, nil
}

// load fills the parser from the policy at path, which is either a
// store or a zelta.conf file.
func (p *parser) load(path string) error {
	if IsStore(path) {
		s, err := LoadStore(path)
		if err != nil {
			return app.Usage("%v", err)
		}
		p.addStore(s)
		return nil
	}
	lines, err := loadLines(path, 0, map[string]bool{})
	if err != nil {
		return err
	}
	return p.parse(lines)
}
