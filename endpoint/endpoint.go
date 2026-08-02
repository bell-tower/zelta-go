package endpoint

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Endpoint is a Zelta location: [user@]host:dataset[@snap] or local dataset[@snap].
//
// JSON: accepts a string ("host:ds" / "tank/a") or an object; always emits an
// object with dataset (and optional user/host/snapshot).
type Endpoint struct {
	Raw      string `json:"-"`
	User     string `json:"user,omitempty"`
	Host     string `json:"host,omitempty"`
	Dataset  string `json:"dataset,omitempty"`
	Snapshot string `json:"snapshot,omitempty"`
	Remote   bool   `json:"-"`
}

// MarshalJSON emits a structured object (or null when zero).
func (e Endpoint) MarshalJSON() ([]byte, error) {
	if e.IsZero() {
		return []byte("null"), nil
	}
	type out struct {
		User     string `json:"user,omitempty"`
		Host     string `json:"host,omitempty"`
		Dataset  string `json:"dataset"`
		Snapshot string `json:"snapshot,omitempty"`
	}
	return json.Marshal(out{
		User:     e.User,
		Host:     e.Host,
		Dataset:  e.Dataset,
		Snapshot: e.Snapshot,
	})
}

// UnmarshalJSON accepts a string endpoint or a structured object.
func (e *Endpoint) UnmarshalJSON(b []byte) error {
	b = bytesTrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*e = Endpoint{}
		return nil
	}
	if b[0] == '"' {
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		if s == "" {
			*e = Endpoint{}
			return nil
		}
		parsed, err := Parse(s)
		if err != nil {
			return err
		}
		*e = parsed
		return nil
	}
	type in struct {
		User     string `json:"user"`
		Host     string `json:"host"`
		Dataset  string `json:"dataset"`
		Snapshot string `json:"snapshot"`
	}
	var o in
	if err := json.Unmarshal(b, &o); err != nil {
		return err
	}
	if o.Dataset == "" {
		return fmt.Errorf("endpoint object: dataset required")
	}
	ep := Endpoint{
		User:     o.User,
		Host:     o.Host,
		Dataset:  o.Dataset,
		Snapshot: o.Snapshot,
		Remote:   o.Host != "",
	}
	ep.Raw = ep.String()
	*e = ep
	return nil
}

func bytesTrimSpace(b []byte) []byte {
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\n' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
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
