package backup

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"git.belltower.it/djbell/zelta-go/zfs"
)

// parentDataset strips the last path component (oracle validate_target_parent_dataset).
// "pool/a/b" → "pool/a"; "pool/a" → "pool"; "pool" → "".
func parentDataset(ds string) string {
	i := strings.LastIndex(ds, "/")
	if i <= 0 {
		return ""
	}
	return ds[:i]
}

// parentCreateAttempts is oracle _depth-1 (path components minus one).
// "pool/a/b" → 2 retries max; "pool/a" → 1; "pool" → 0 (skip create).
func parentCreateAttempts(parent string) int {
	if parent == "" {
		return 0
	}
	n := strings.Count(parent, "/") + 1
	if n < 2 {
		return 0
	}
	return n - 1
}

// ensureTargetParent creates or checks the target parent when the target root is missing.
// Oracle: CREATE_PARENT default on; dry-run still runs create (side effect, no + line).
// OpenZFS bug: create -up on readonly hierarchies fails one level at a time — retry.
func ensureTargetParent(ctx context.Context, exec zfs.Executor, tgtEp, tgtDS string, targetExists, createParent bool) error {
	if targetExists {
		return nil
	}
	parent := parentDataset(tgtDS)
	if parent == "" {
		return nil
	}
	if !createParent {
		ok, err := exec.Exists(ctx, tgtEp, parent)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("target has no parent dataset: '%s'", parent)
		}
		return nil
	}
	return createParentRetry(ctx, exec, tgtEp, parent)
}

func createParentRetry(ctx context.Context, exec zfs.Executor, tgtEp, parent string) error {
	attempts := parentCreateAttempts(parent)
	var last error
	for i := 0; i < attempts; i++ {
		err := exec.Create(ctx, tgtEp, parent)
		if err == nil {
			return nil
		}
		if errors.Is(err, zfs.ErrReadOnlyCreate) {
			last = err
			continue
		}
		return err
	}
	if last != nil {
		return fmt.Errorf("incomplete zfs create for parent '%s': %w", parent, last)
	}
	return nil
}
