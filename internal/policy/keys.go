package policy

import (
	"strings"
	"sync"

	"git.belltower.it/djbell/zelta-go/internal/opt"
)

type keyInfo struct {
	typ   string
	scope bool // true = policy-only key (not forwarded to backup)
}

var (
	keysOnce sync.Once
	keys     map[string]keyInfo
	legacy   map[string]string
	keysErr  error
)

func loadKeys() error {
	keysOnce.Do(func() {
		rows, err := opt.Table()
		if err != nil {
			keysErr = err
			return
		}
		keys = map[string]keyInfo{}
		legacy = map[string]string{}
		for _, r := range rows {
			if r.Key == "" || !r.AppliesTo("policy") {
				continue
			}
			keys[r.Key] = keyInfo{typ: r.Type, scope: r.Verbs == "policy"}
			if r.Alias != "" {
				for _, a := range strings.Split(r.Alias, ",") {
					a = strings.TrimSpace(a)
					if a != "" {
						legacy[a] = r.Key
					}
				}
			}
		}
	})
	return keysErr
}

func canonicalize(name string) (string, error) {
	if err := loadKeys(); err != nil {
		return "", err
	}
	k := strings.ToUpper(strings.TrimSpace(name))
	k = strings.ReplaceAll(k, "-", "_")
	k = strings.TrimPrefix(k, "ZELTA_")
	if canon, ok := legacy[k]; ok {
		k = canon
	}
	if _, ok := keys[k]; !ok {
		return "", errUnknownOption(k)
	}
	return k, nil
}

func normalizeValue(key, val string) string {
	if err := loadKeys(); err != nil {
		return val
	}
	info, ok := keys[key]
	if !ok {
		return val
	}
	switch info.typ {
	case "true", "false":
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "1", "yes", "true", "on":
			return "1"
		case "0", "no", "false", "off":
			return "0"
		}
	}
	return val
}

// policyScopeSet returns the set of policy-only keys (not forwarded to backup).
func policyScopeSet() map[string]bool {
	if err := loadKeys(); err != nil {
		return nil
	}
	out := map[string]bool{}
	for k, info := range keys {
		if info.scope {
			out[k] = true
		}
	}
	return out
}

type unknownOptionError string

func (e unknownOptionError) Error() string { return "unknown option: " + string(e) }

func errUnknownOption(k string) error { return unknownOptionError(k) }
