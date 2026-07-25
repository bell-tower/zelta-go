package zfs

import "context"

// Executor runs ZFS-related commands (local or remote).
// Unit tests use Fake; production uses Real.
type Executor interface {
	// List runs zfs list-like output for an endpoint dataset tree.
	// Returns stdout lines (no trailing empties required).
	List(ctx context.Context, endpoint, dataset string, props []string) ([]string, error)
}
