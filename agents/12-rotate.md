# Rotate / Clone-Origin

Rotate is not a destructive shortcut around backup. Its first Go slice should
be a root-only, dry-run-first planner with explicit safety checks.

## Divergence classes

Rotate handles three distinct diversions:

1. The source was rolled back or otherwise changed without an external
   operation log. This is intentionally handled like direct target rotation;
   the two causes are not distinguishable from ZFS state alone.
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

Do not add Rotate behavior to ordinary `backup.Run` implicitly. Keep the
request/planner/executor surface under `internal/rotate`; execution is separate
from planning and remains covered by dry-run goldens and injected-failure tests.

Origin data now flows through `match.RotateListProps`, and direct-match plus
verified source-origin dry-run plans emit per-child send/receive lineage.
Source snapshot-if-needed/always planning, target preservation collision checks
are present in the planner/CLI. The
remaining blockers are remote-aware dry-run formatting and
post-execution partial-failure recovery. Execution is available through
`internal/rotate.Execute`; it re-runs match and reports remaining divergence,
but recursive failures still stop at the first failed child.

`clone` and `revert` now represent the prerequisite lineage contracts. In
particular, `revert` is the normal producer of a source clone origin that
Rotate recognizes.

## Reference

- Oracle implementation: `~/Code/zelta/share/zelta/zelta-backup.awk`, Rotate
  branches around `1252-1419`.
- Product contract: `~/Code/zelta/doc/zelta-rotate.md`.
- Clone-origin handling must remain separate from revert and the four-endpoint
  clone workflow until those contracts are covered.
