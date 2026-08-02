package zfs

import "github.com/bell-tower/zelta-go/endpoint"

// Command kinds for structured dry-run / audit trails.
const (
	CmdList     = "list"
	CmdSnapshot = "snapshot"
	CmdSendRecv = "send_recv"
	CmdRename   = "rename"
	CmdClone    = "clone"
	CmdBookmark = "bookmark"
	CmdCreate   = "create"
	CmdDestroy  = "destroy"
	CmdOther    = "other"
)

// Command is one planned or executed ZFS operation as data (not a shell line).
// CLI and integrators render transcripts via PipeShellDirection / CommandShell.
type Command struct {
	Kind string // CmdList, CmdSnapshot, CmdSendRecv, …
	// Endpoint is the primary host for single-sided ops (list, snapshot, rename, …).
	Endpoint endpoint.Endpoint
	// Argv is the local/single-host argv.
	Argv []string
	// Source/Target + Send/Recv/Direction describe CmdSendRecv pipes.
	Source    endpoint.Endpoint
	Target    endpoint.Endpoint
	Send      []string
	Recv      []string
	Direction string // pull|push|proxy
}

// ShellLine renders this command as an endpoint-aware shell string (no "+ " prefix).
func (c Command) ShellLine() (string, error) {
	switch c.Kind {
	case CmdSendRecv:
		return PipeShellDirection(c.Source.String(), c.Target.String(), c.Send, c.Recv, c.Direction)
	case CmdList:
		// Match dry-run shows "zfs list … user@host:ds" without ssh wrapping (oracle).
		return SoftJoin(c.Argv), nil
	case CmdSnapshot:
		if len(c.Argv) == 0 {
			return "", nil
		}
		// Prefer SnapshotShell when argv looks like zfs snapshot [-r] ds@snap.
		dsSnap := c.Argv[len(c.Argv)-1]
		rec := false
		for _, a := range c.Argv {
			if a == "-r" {
				rec = true
				break
			}
		}
		return SnapshotShell(c.Endpoint.String(), dsSnap, rec)
	default:
		ep := c.Endpoint.String()
		if ep == "" && len(c.Argv) > 0 {
			return SoftJoin(c.Argv), nil
		}
		return CommandShell(ep, c.Argv)
	}
}
