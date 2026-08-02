package endpoint

import (
	"fmt"
	"strings"
)

// Endpoint is a Zelta location: [user@]host:dataset[@snap] or local dataset[@snap].
type Endpoint struct {
	Raw      string
	User     string
	Host     string
	Dataset  string
	Snapshot string
	Remote   bool
}

// Parse splits a Zelta endpoint string.
func Parse(ep string) (Endpoint, error) {
	out := Endpoint{Raw: ep}
	if ep == "" {
		return out, fmt.Errorf("empty endpoint")
	}

	rest := strings.ReplaceAll(ep, "\r", "")
	// user@host: or user@[ipv6]:
	if at := strings.Index(rest, "@"); at >= 0 {
		// Only treat as user if @ appears before the dataset path colon rules.
		if isRemotePrefix(rest) {
			out.User = rest[:at]
			rest = rest[at+1:]
		}
	}

	if host, ds, ok := splitHostDataset(rest); ok {
		out.Host = host
		out.Remote = true
		rest = ds
	}

	if i := strings.LastIndex(rest, "@"); i >= 0 {
		// snapshot only if @ is after the last /
		if j := strings.LastIndex(rest, "/"); j < i {
			out.Dataset = rest[:i]
			out.Snapshot = rest[i+1:]
			if out.Dataset == "" {
				return out, fmt.Errorf("missing dataset in %q", ep)
			}
			return out, nil
		}
	}
	out.Dataset = rest
	if out.Dataset == "" {
		return out, fmt.Errorf("missing dataset in %q", ep)
	}
	return out, nil
}

func isRemotePrefix(s string) bool {
	// remote if we see host:dataset pattern (including [ipv6]:)
	_, _, ok := splitHostDataset(s)
	if ok {
		return true
	}
	// user@ still might be local nonsense; require host: after @
	if at := strings.Index(s, "@"); at >= 0 {
		_, _, ok = splitHostDataset(s[at+1:])
		return ok
	}
	return false
}

func splitHostDataset(s string) (host, dataset string, ok bool) {
	if strings.HasPrefix(s, "[") {
		end := strings.Index(s, "]:")
		if end < 0 {
			return "", "", false
		}
		return s[1:end], s[end+2:], true
	}
	// host:dataset — host has no /
	colon := strings.Index(s, ":")
	if colon <= 0 {
		return "", "", false
	}
	hostPart := s[:colon]
	if strings.Contains(hostPart, "/") {
		return "", "", false
	}
	return hostPart, s[colon+1:], true
}

// IsZero reports an unset endpoint (no dataset).
func (e Endpoint) IsZero() bool {
	return e.Dataset == "" && e.Host == "" && e.User == "" && e.Snapshot == "" && e.Raw == ""
}

// MustParse is for tests and static fixtures; panics on error.
func MustParse(s string) Endpoint {
	e, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return e
}

// String rebuilds a canonical endpoint string.
func (e Endpoint) String() string {
	ds := e.Dataset
	if e.Snapshot != "" {
		ds = ds + "@" + e.Snapshot
	}
	if !e.Remote && e.Host == "" {
		return ds
	}
	host := e.Host
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	if e.User != "" {
		return e.User + "@" + host + ":" + ds
	}
	return host + ":" + ds
}

// DSSuffix returns the path of full relative to root, with a leading /.
// If full equals root, suffix is "".
func DSSuffix(root, full string) (string, error) {
	root = strings.TrimSuffix(root, "/")
	full = strings.TrimSuffix(full, "/")
	if full == root {
		return "", nil
	}
	prefix := root + "/"
	if !strings.HasPrefix(full, prefix) {
		return "", fmt.Errorf("%q is not under %q", full, root)
	}
	return "/" + strings.TrimPrefix(full, prefix), nil
}

// SplitOrigin splits a clone origin "dataset@snap" into the dataset and the
// "@snap" savepoint. ok is false when there is no usable "@" separator.
func SplitOrigin(origin string) (dataset, savepoint string, ok bool) {
	i := strings.LastIndex(origin, "@")
	if i <= 0 || i == len(origin)-1 {
		return "", "", false
	}
	return origin[:i], origin[i:], true
}
