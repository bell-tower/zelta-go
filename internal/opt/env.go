package opt

import (
	"os"
	"strings"
)

// Lookup returns ZELTA_<key> or bare <key> from the process environment.
func Lookup(key string) (string, bool) {
	if v, ok := os.LookupEnv("ZELTA_" + key); ok {
		return v, true
	}
	if v, ok := os.LookupEnv(key); ok {
		return v, true
	}
	return "", false
}

// LookupBool reads a truthy/falsey env value; missing → def.
// Falsey: "", "0", "no", "false", "off" (case-insensitive).
func LookupBool(key string, def bool) bool {
	v, ok := Lookup(key)
	if !ok {
		return def
	}
	return truthy(v, def)
}

func truthy(v string, def bool) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "":
		return def
	case "0", "no", "false", "off":
		return false
	default:
		return true
	}
}

// Env is a resolved option environment: bare KEY → value.
type Env map[string]string

// Get returns the raw value ("" if unset).
func (e Env) Get(key string) string { return e[key] }

// Bool reads a truthy/falsey value; missing/empty → def.
func (e Env) Bool(key string, def bool) bool {
	v, ok := e[key]
	if !ok {
		return def
	}
	return truthy(v, def)
}

// List splits a comma-joined list value (EXCLUDE, INCLUDE, RECV_PROPS_ADD…).
func (e Env) List(key string) []string {
	v := e[key]
	if v == "" {
		return nil
	}
	return strings.Split(v, ",")
}

// normalize applies oracle zelta_init value normalization:
// no/false/0 (case-insensitive) → "0". No true-side normalization.
func normalize(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "no", "false":
		return "0"
	default:
		return v
	}
}

// seed builds the pre-CLI environment: built-in defaults, then process
// env ZELTA_* (non-empty wins), then legacy aliases (first set wins).
// Returns deprecation warnings for warned legacy aliases.
func seed() (Env, []string) {
	e := Env{}
	for k, v := range defaults {
		e[k] = v
	}
	for _, kv := range os.Environ() {
		k, v, _ := strings.Cut(kv, "=")
		bk, ok := strings.CutPrefix(k, "ZELTA_")
		if !ok || v == "" { // empty export counts as unset (oracle :=)
			continue
		}
		e[bk] = normalize(v)
	}
	// Legacy aliases (oracle zelta-args load_option_list): any row with a
	// KEY_ALIAS, in TSV order, global latch — only the FIRST set alias is
	// honored per run, and it OVERWRITES the primary key (oracle quirk).
	var warns []string
	if rows, err := Table(); err == nil {
		for _, r := range rows {
			if r.Alias == "" {
				continue
			}
			v := os.Getenv("ZELTA_" + r.Alias)
			if v == "" {
				continue
			}
			e[r.Key] = normalize(v)
			if r.Warn != "" {
				warns = append(warns, r.Warn)
			}
			break // LEGACY_ENV latch
		}
	}
	return e, warns
}
