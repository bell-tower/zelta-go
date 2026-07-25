package zfs

import (
	"context"
	"errors"
)

// ErrReadOnlyCreate is the OpenZFS "create -up" bug: one ancestor may be created
// then the command fails with "Read-only file system". Retry the same CREATE.
var ErrReadOnlyCreate = errors.New("zfs create: read-only file system")

// Executor runs ZFS-related commands (local or remote).
// Unit tests use Fake; production uses Real.
type Executor interface {
	// List runs zfs list-like output for an endpoint dataset tree.
	// depth 0 means unlimited; depth > 0 maps to zfs list -d.
	List(ctx context.Context, endpoint, dataset string, props []string, depth int) ([]string, error)

	// Snapshot runs zfs snapshot [-r] dataset@snap on the endpoint.
	Snapshot(ctx context.Context, endpoint, datasetSnap string, recursive bool) error

	// Create runs zfs create -vupo canmount=noauto dataset (cmds.tsv CREATE).
	// Already-exists is success (parent may already be present).
	Create(ctx context.Context, endpoint, dataset string) error

	// Exists reports whether dataset is present on the endpoint (cmds.tsv CHECK).
	Exists(ctx context.Context, endpoint, dataset string) (bool, error)

	// Bookmark creates sourceSnap#bookmark on the endpoint.
	Bookmark(ctx context.Context, endpoint, sourceSnap, bookmark string) error
	Clone(ctx context.Context, endpoint, sourceSnap, dataset string) error

	// RunPipe runs leftArgv | rightArgv (each side local or ssh by endpoint).
	// Argv includes the zfs binary as argv[0] (e.g. "zfs", "send", ...).
	RunPipe(ctx context.Context, leftEndpoint string, leftArgv []string, rightEndpoint string, rightArgv []string) error

	// RunPipeDirection is RunPipe with explicit multi-host direction for
	// dual-remote endpoints: "PULL" (pipe on target), "PUSH" (pipe on source),
	// "" / "0" (controller-side ssh|ssh, oracle proxy).
	RunPipeDirection(ctx context.Context, leftEndpoint string, leftArgv []string, rightEndpoint string, rightArgv []string, direction string) error
}
