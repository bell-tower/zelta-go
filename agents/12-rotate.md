# Rotate / Clone-Origin

Rotate is not a destructive shortcut around backup. Its first Go slice should
be a root-only, dry-run-first planner with explicit safety checks.

## Divergence classes

Rotate handles three distinct diversions:

1. The source was rolled back.
2. The source was cloned, for example by `zelta revert`.
3. The target has diverged.

Do not collapse these into one "no match" case. Their lineage and safety
decisions differ.

## Required behavior

1. Compare source and target state.
2. If the target diverged, preserve it with `zfs rename -fp` to a name tied to
   the matching snapshot.
3. Receive the source incrementally against the preserved target when a direct
   common snapshot exists.
4. If there is no direct match, inspect the source dataset's ZFS `origin` and
   verify the corresponding target origin snapshot exists.
5. Use `zfs recv -o origin=<preserved-target>@<origin-snapshot>` for the
   source-origin path.
6. Refuse unsafe cases: no usable root match/origin, missing origin snapshot,
   non-replica trees, or an up-to-date source with no new snapshot.

## Go boundary

Do not add Rotate behavior to ordinary `backup.Run` implicitly. Start with a
separate request/result/planner surface under `internal/rotate`, then connect
execution only after dry-run goldens establish the rename and receive order.

Origin data now flows through `match.RotateListProps`, and root direct-match
and verified source-origin dry-run plans emit the corresponding send/receive
lineage. The remaining blockers are recursive per-child actions, target
preservation collision/preflight handling, and execution after golden review.
Root `CLONE`/`RENAME` builders and lineage planners are available under
`internal/lineage`.

Implement `clone` and `revert` lineage operations before Rotate if their
contracts are not already represented. In particular, `revert` is the normal
producer of a source clone origin that Rotate must recognize.

## Reference

- Oracle implementation: `~/Code/zelta/share/zelta/zelta-backup.awk`, Rotate
  branches around `1252-1419`.
- Product contract: `~/Code/zelta/doc/zelta-rotate.md`.
- Clone-origin handling must remain separate from revert and the four-endpoint
  clone workflow until those contracts are covered.
