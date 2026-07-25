# Clone / Revert

Clone and revert are non-destructive lineage operations. They preserve the
existing dataset tree rather than rolling back or overwriting it in place.

## Clone contract

- `zelta clone [options] SOURCE[@SNAPSHOT] TARGET`
- Source snapshot is optional; omitted means each dataset uses its latest
  usable snapshot.
- Clone is recursive over the dataset tree and preserves source-to-target
  dataset suffixes.
- Source and target must resolve to the same host and pool.
- Target must not already exist; validate before issuing any clone.
- Each clone uses `zfs clone -p -o readonly=off source@snapshot target`.
- Children without a usable snapshot are skipped according to the oracle.
- The four-endpoint clone-and-backup workflow is separate from ordinary clone.

## Revert contract

- `zelta revert [options] DATASET[@SNAPSHOT]`
- Snapshot is optional; omitted means the latest usable snapshot.
- Revert is recursive and preserves each current dataset under
  `dataset_<snapshot>` before cloning the selected snapshot back.
- Required command order per dataset is rename, clone, then create a new
  source snapshot when the oracle requires it.
- Use `zfs rename -fp` and `zfs clone -p -o readonly=off`; never use
  `zfs rollback -F`.
- Detect preservation-name collisions before changing the tree.
- Report that `zelta rotate` should be run to preserve replication continuity.

## Go boundary

Planning remains separate from `zfs.Executor` execution. Recursive planning
must carry endpoint host context separately from dataset argv: a remote
endpoint is passed to the executor, while ZFS argv contains only dataset names.
