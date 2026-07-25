# Prune / zprune

- `zelta prune` — **read-only** candidate selection (implemented)
- `zprune` — destructive destroy wrapper + prompt (deferred; `cmd/zprune` stub)

## Implemented (`internal/prune`)

`zelta prune [OPTIONS] ENDPOINT`

- Retention: `--prune-num=N`, `--prune-time=T`, `--prune-size=N`, `--prune-grid=GRID`
  (default `30 / 30days` when all unset — oracle `prune_init`)
- Guard: `--match-endpoint=GUARD`, `--prune-guard=latest|unsynced|none`, `--no-prune-guard`
  (no second operand → GUARD_NONE)
- Filters: `-X/--exclude`, `--include` (filtered snaps/datasets are kept)
- Output: compressed ranges `@old%new` (oldest-first per DS); `--no-ranges`; `--visual` (❌/🔹)
- List props: source `name,guid,written,creation,used,referenced,clones`;
  guard target without `clones` (oracle)

## Analysis rules (oracle `analyze_prune_candidates`)

Per source DS, newest-first; only snaps **older than match** (match = newest snap
with GUID on guard target). A snap is **kept** when:

1. prune_filtered (`-X`/`--include`) or has clones (`clones` non-empty/non-`-`)
2. guard `unsynced` → **always kept** (oracle 1.2.0 quirk: never prunes)
3. grid keeps (first/last snap or first-in-bucket), `--prune-num` snaps after match,
   or `creation ≥ now − --prune-time`
4. `--prune-size`: select oldest eligible until Σwritten−referenced ≥ size

Ranges compress by contiguous snap index (oracle `compress_snapshot_ranges`).

## Defer

zprune destroy wrapper + prompt/force, `--send-range` (prune for backup intermediates),
clone-origin awareness, bookmarks in output, prune golden fixtures.

Intentional Awk≠Go notes (unsynced quirk, visual prefix, …): **`agents/10-deviations.md`**.
