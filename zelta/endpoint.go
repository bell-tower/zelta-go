package zelta

import (
	"fmt"
	"strings"

	"zelta.space/internal/endpoint"
)

// Endpoint is a dataset location in Zelta's scp-like syntax:
//
//	[[user@]host:]pool[/dataset][@snapshot]
//
// Operations take endpoints as strings; Endpoint parses and builds them.
type Endpoint struct {
	// User and Host select a remote machine; both are empty for a local
	// dataset.
	User, Host string
	// Dataset is the dataset name, such as "tank/home".
	Dataset string
	// Snapshot is a snapshot name without the "@", if any.
	Snapshot string
}

// ParseEndpoint parses an endpoint string.
func ParseEndpoint(s string) (Endpoint, error) {
	ep := endpoint.Parse(s, "", "")
	if !ep.Valid() {
		return Endpoint{}, fmt.Errorf("zelta: invalid endpoint %q", s)
	}
	out := Endpoint{Dataset: ep.DS, Snapshot: strings.TrimPrefix(ep.Snap, "@")}
	if ep.IsRemote() {
		out.User, out.Host = ep.User, ep.Host
	}
	return out, nil
}

// IsRemote reports whether the endpoint is on another machine.
func (e Endpoint) IsRemote() bool { return e.Host != "" }

// String formats the endpoint.
func (e Endpoint) String() string {
	var b strings.Builder
	if e.Host != "" {
		if e.User != "" {
			b.WriteString(e.User)
			b.WriteByte('@')
		}
		if strings.Contains(e.Host, ":") {
			b.WriteByte('[')
			b.WriteString(e.Host)
			b.WriteByte(']')
		} else {
			b.WriteString(e.Host)
		}
		b.WriteByte(':')
	} else if strings.Contains(e.Dataset, ":") {
		b.WriteString("localhost:")
	}
	b.WriteString(e.Dataset)
	if e.Snapshot != "" {
		b.WriteByte('@')
		b.WriteString(e.Snapshot)
	}
	return b.String()
}
