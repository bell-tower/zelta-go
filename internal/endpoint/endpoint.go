// Package endpoint parses Zelta endpoints, which follow scp(1) conventions:
//
//	[[user@]host:]pool[/dataset][@snapshot]
//	[[user@][ipv6]:]pool[/dataset][@snapshot]
//
// A leading "localhost:" without a user selects the local host, which allows
// local dataset names that contain a colon.
package endpoint

import (
	"regexp"
	"strings"

	"zelta.space/internal/command"
)

// Endpoint is a parsed dataset location.
type Endpoint struct {
	// ID is the endpoint exactly as the user wrote it.
	ID string
	// Remote is the "[user@]host" ssh destination; empty for local.
	Remote string
	// User is the remote user, or the local user name.
	User string
	// Host is the remote host, or the local hostname for local endpoints.
	Host string
	// DS is the dataset name without any snapshot.
	DS string
	// Snap is the snapshot including its leading "@", if any.
	Snap string
	// Pool is the first component of DS.
	Pool string
	// Leaf is the last component of DS.
	Leaf string
	// Depth is the number of components in DS.
	Depth int
}

var ipv6Remote = regexp.MustCompile(`^([^/: ]+@)?\[[^]]+\]:`)
var plainRemote = regexp.MustCompile(`^[^ :/]+:`)

// Parse parses an endpoint string. hostname and user describe the local
// machine and are used for logging when the endpoint has no remote part.
func Parse(id, hostname, user string) Endpoint {
	ep := Endpoint{ID: id}
	if id == "" {
		return ep
	}
	rest := id
	if loc := ipv6Remote.FindStringIndex(rest); loc != nil {
		remote := strings.NewReplacer("[", "", "]", "").Replace(rest[:loc[1]-1])
		rest = rest[loc[1]:]
		ep.Remote = remote
		if u, h, ok := strings.Cut(remote, "@"); ok {
			ep.User, ep.Host = u, h
		} else {
			ep.Host = remote
		}
	} else if plainRemote.MatchString(rest) {
		remote, after, _ := strings.Cut(rest, ":")
		rest = after
		ep.Remote = remote
		if u, h, ok := strings.Cut(remote, "@"); ok && !strings.Contains(h, "@") {
			ep.User, ep.Host = u, h
		} else {
			ep.Host = remote
		}
		if ep.User == "" && ep.Host == "localhost" {
			ep.Remote = ""
		}
	}
	if ep.Host == "" || ep.Host == "localhost" {
		ep.Host = hostname
	}
	ds, snap, hasSnap := strings.Cut(rest, "@")
	if hasSnap && !strings.Contains(snap, "@") {
		ep.Snap = "@" + snap
	}
	ep.DS = ds
	parts := strings.Split(ds, "/")
	ep.Depth = len(parts)
	ep.Pool = parts[0]
	ep.Leaf = parts[len(parts)-1]
	if ep.User == "" {
		ep.User = user
	}
	return ep
}

// IsRemote reports whether the endpoint is reached through a remote shell.
func (e Endpoint) IsRemote() bool { return e.Remote != "" }

// Valid reports whether the endpoint names a dataset.
func (e Endpoint) Valid() bool { return e.DS != "" }

// RemoteFor returns the remote shell destination for commands on this
// endpoint using the given remote shell command (e.g. "ssh -n"), or nil for
// local endpoints.
func (e Endpoint) RemoteFor(prefix string) *command.Remote {
	if e.Remote == "" {
		return nil
	}
	return command.NewRemote(prefix, e.Remote)
}

// SameHost reports whether two endpoints reach the same machine the same way.
func (e Endpoint) SameHost(o Endpoint) bool { return e.Remote == o.Remote }
