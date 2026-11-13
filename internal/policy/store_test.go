package policy

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An imported zelta.conf runs the same jobs as the original.
func TestStoreImportRoundTrip(t *testing.T) {
	jobs, err := Import(centralized)
	if err != nil {
		t.Fatal(err)
	}
	s := &Store{Version: StoreVersion, Jobs: jobs}
	path := filepath.Join(t.TempDir(), "policy.json")
	if err := s.Save(path); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		golden string
		opts   map[string]string
		args   []string
	}{
		{"centralized.table.golden", nil, nil},
		{"centralized.H.golden", map[string]string{"NO_HEADER": "1"}, []string{"NYC1", "host1.client0"}},
		{"centralized.verbose.golden", map[string]string{"LOG_LEVEL": "3"}, []string{"fw2.dal1"}},
	} {
		opts := map[string]string{"CONFIG": path, "DRYRUN": "1"}
		maps.Copy(opts, c.opts)
		tr := newRT(opts)
		if err := Run(ctx, tr.Runtime, c.args, Deps{}); err != nil {
			t.Fatal(err)
		}
		if got, want := tr.Out.String(), golden(t, c.golden); got != want {
			t.Errorf("%s mismatch:\n%s\nwant:\n%s", c.golden, got, want)
		}
	}
}

func TestStoreValidate(t *testing.T) {
	for _, c := range []struct {
		body, err string
	}{
		{`{"version": 2, "jobs": []}`, "unsupported policy version 2"},
		{`{"version": 1, "jobs": [{"source": "a/b"}]}`, "needs a source and a target"},
		{`{"version": 1, "jobs": [{"source": "a/b@s", "target": "c/d"}]}`, "invalid endpoint 'a/b@s'"},
		{`{"version": 1, "jobs": [{"source": "a/b", "target": "c/d"}, {"source": "a/b", "target": "c/d"}]}`, "duplicate job"},
		{`{"version": 1, "jobs": [{"source": "a/b", "target": "c/d", "options": {"LOG_LEVEL": "4"}}]}`, "option 'LOG_LEVEL' cannot be saved"},
		{`{"version": 1, "jobs": [{"source": "a/b", "target": "c/d", "options": {"BOGUS": "1"}}]}`, "option 'BOGUS' cannot be saved"},
	} {
		path := filepath.Join(t.TempDir(), "policy.json")
		os.WriteFile(path, []byte(c.body), 0o644)
		if _, err := LoadStore(path); err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: got %v, want %q", c.body, err, c.err)
		}
	}
	s, err := LoadStore(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil || len(s.Jobs) != 0 {
		t.Errorf("missing store: %v %+v", err, s)
	}
}
