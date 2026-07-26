# Prune / zprune

- `zelta prune` — **read-only** candidate selection (implemented)
- CLI integration is covered by fixture-backed command-level golden tests.
- `zprune` — destructive destroy wrapper + prompt (implemented; CLI only)

## Implemented (`prune` package)

`zelta prune [OPTIONS] ENDPOINT`

- Retention: `--prune-num=N`, `--prune-time=T`, `--prune-size=N`, `--prune-grid=GRID`
  (default `30 / 30days` when all unset — oracle `prune_init`)
- Guard: `--match-endpoint=GUARD`, `--prune-guard=latest|unsynced|none`, `--no-prune-guard`
  (no second operand → GUARD_NONE)
- Filters: `-X/--exclude`, `--include` (filtered snaps/datasets are kept)
- Output: compressed ranges `@old%new` (oldest-first per DS); `--no-ranges`; `--visual` (❌/🔹)
- List props: source `name,guid,written,creation,used,referenced,clones`;
  guard target without `clones` (oracle)

## Implemented (`zprune` CLI)

`zelta zprune` / argv[0] `zprune` binary:

- Same analysis as `zelta prune` via in-process `prune.Run`
- `--dryrun` / `-n` — print candidates only (no destroy)
- Confirm prompt unless `--force` / `-f` (`PRUNE_FORCE`)
- Destroy: one `zfs destroy ds@snap1,snap2,…` per dataset (comma form)
- Remote: transport host from the **source endpoint** (list names are bare `ds@snap`);
  uses `remoteFromEnv()` / `zfs.Remote`

### Real-ZFS evidence (scratch only — never golden treetop)

| Host | Local | Remote hop from Mac |
|------|-------|---------------------|
| `dev2` | `cpool/zprune-evidence` | `root@dev2:cpool/zprune-remote` |
| `debian` | `apool/zprune-evidence` | `root@debian:apool/zprune-remote` |

Checked: dry-run list, abort (`n`), `--force` destroy keeps newest N, remote SSH destroy.

**Note:** `--prune-time=0` with fresh snaps keeps everything (`creation >= now - 0`).
Use num-only retention for scratch smoke tests (`--prune-num=N --no-prune-guard`).

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

`--send-range` (prune for backup intermediates), clone-origin awareness,
bookmarks in output.

Intentional Awk≠Go notes (unsynced quirk, visual prefix, …): **`agents/10-deviations.md`**.
