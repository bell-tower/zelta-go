package conf

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EtcDir resolves ZELTA_ETC (oracle bin/zelta):
// env → ~/.config/zelta (if the dir exists) → /usr/local/etc/zelta.
func EtcDir() string {
	if v := os.Getenv("ZELTA_ETC"); v != "" {
		return v
	}
	if home, err := os.UserHomeDir(); err == nil {
		d := filepath.Join(home, ".config", "zelta")
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	return "/usr/local/etc/zelta"
}

// EnvPath resolves ZELTA_ENV: env → $ZELTA_ETC/zelta.env.
func EnvPath() string {
	if v := os.Getenv("ZELTA_ENV"); v != "" {
		return v
	}
	return filepath.Join(EtcDir(), "zelta.env")
}

// Keys the oracle refuses to honor from zelta.env (must be exported prior).
var rejectedKeys = map[string]bool{
	"AWK": true, "ETC": true, "ENV": true, // compared bare; stored ZELTA_-prefixed
}

// LoadEnvFile parses a zelta.env KEY=value file. Returned keys are bare
// (ZELTA_ prefix stripped). A missing file is not an error (empty map).
// Malformed lines are silently tolerated (oracle behavior).
//
// Oracle load_env (bin/zelta): skip blank/comment lines, strip leading
// whitespace, prepend ZELTA_ if missing, strip trailing inline comments
// (\s*#.*), split on the first '='. DEVIATION: the oracle evals values as
// shell words (live quotes and $(...)); Go stores the raw word, stripping at
// most one layer of surrounding quotes — no command substitution.
func LoadEnvFile(path string) (vals map[string]string, warnings []string, err error) {
	vals = map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return vals, nil, nil
		}
		return nil, nil, err
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimLeft(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Strip trailing inline comment: whitespace followed by '#'.
		for i := 1; i < len(line); i++ {
			if line[i] == '#' && (line[i-1] == ' ' || line[i-1] == '\t') {
				line = line[:i]
				break
			}
		}
		line = strings.TrimRight(line, " \t")
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue // tolerated (oracle: silently)
		}
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		k = strings.TrimPrefix(k, "ZELTA_")
		if rejectedKeys[k] {
			warnings = append(warnings, fmt.Sprintf("ZELTA_%s ignored; it must be exported prior to zelta.env", k))
			continue
		}
		v = strings.TrimSpace(v)
		if len(v) >= 2 {
			if (v[0] == '\'' && v[len(v)-1] == '\'') || (v[0] == '"' && v[len(v)-1] == '"') {
				v = v[1 : len(v)-1]
			}
		}
		if v == "" {
			continue // oracle := semantics: empty counts as unset
		}
		vals[k] = v
	}
	return vals, warnings, nil
}
