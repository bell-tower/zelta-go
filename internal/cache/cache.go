// Package cache stores hints that make Zelta faster: never the authority
// for a decision. Deleting the cache directory may only make Zelta slower.
//
// Entries are small JSON files written atomically. A nil *Store is a valid
// disabled cache: loads miss and saves do nothing.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Store is a cache directory.
type Store struct {
	// Dir is the cache directory, created on first save.
	Dir string
	// TTL bounds the age of entries loaded with Fresh; 0 means no limit.
	TTL time.Duration
	// Now returns the current time; nil uses time.Now.
	Now func() time.Time
}

type envelope struct {
	Saved int64           `json:"saved"`
	Data  json.RawMessage `json:"data"`
}

// DefaultDir returns the per-user cache directory for Zelta.
func DefaultDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return ""
	}
	return filepath.Join(dir, "zelta")
}

// Key returns a stable file-name-safe key for the given parts.
func Key(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:8])
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) path(name string) string {
	return filepath.Join(s.Dir, filepath.FromSlash(name)+".json")
}

// Load decodes the named entry into v regardless of age and returns the
// time it was saved.
func (s *Store) Load(name string, v any) (time.Time, bool) {
	if s == nil || s.Dir == "" {
		return time.Time{}, false
	}
	data, err := os.ReadFile(s.path(name))
	if err != nil {
		return time.Time{}, false
	}
	var e envelope
	if json.Unmarshal(data, &e) != nil || json.Unmarshal(e.Data, v) != nil {
		return time.Time{}, false
	}
	return time.Unix(e.Saved, 0), true
}

// Fresh is Load for entries younger than TTL.
func (s *Store) Fresh(name string, v any) bool {
	saved, ok := s.Load(name, v)
	if !ok {
		return false
	}
	return s.TTL <= 0 || s.now().Sub(saved) < s.TTL
}

// Save writes v as the named entry.
func (s *Store) Save(name string, v any) error {
	if s == nil || s.Dir == "" {
		return nil
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	out, err := json.Marshal(envelope{Saved: s.now().Unix(), Data: data})
	if err != nil {
		return err
	}
	p := s.path(name)
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if _, err := f.Write(out); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), p)
}

// Delete removes the named entry.
func (s *Store) Delete(name string) {
	if s == nil || s.Dir == "" {
		return
	}
	_ = os.Remove(s.path(name))
}
