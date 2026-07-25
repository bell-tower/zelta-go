# Intentional deviations — Awk (`~/Code/zelta`) → Go (this repo)

**Full log.** Infrequent reads: open when porting, pixel-diffing, or changing shared contracts.
**Not** a todo list (see package `## Defer`). **Not** pixel goldens (see `testdata/golden/`).

Format per bullet: **area** — Awk → Go — *why / where / example*.

---

## Architecture (product-level)

- **No recursive self-calls** — Awk `zelta ipc-run match|backup|prune` → Go in-process `match.Compare` / library APIs. *Why:* single binary + library; no shell recursion. *Where:* `internal/match`, `internal/backup`; cmds.tsv `MATCH*` rows unused for execution.
- **cmds.tsv → argv, not shell strings** — Awk `build_command` returns one shell line with remote prefix baked in → Go `cmdbuild.Build` = argv only; `zfs.Real` wraps ssh by endpoint + `RemoteRole`. *Example:* SEND → `[]string{"zfs","send",…}` then `ssh -n host '…'` at exec/dry-run format time.
- **REMOTE_* roles vs env strings** — Awk `REMOTE_SEND="ssh -n"`, `REMOTE_RECV="ssh"` (full override strings) → Go uses cmds.tsv role column + `cmdbuild.StdinNull` (`RECV` keeps stdin; else `-n`). Env override of the whole prefix string **not wired** yet (`opt`).
- **Embed data** — Awk `ZELTA_SHARE`/on-disk TSV → Go `//go:embed data/*`. Man pages stay in main tree / `ZELTA_DOC`.
- **Fake executor** — Awk always real zfs/ssh → Go unit/golden path uses `zfs.Fake` canned lists; no ZFS on the Mac.
- **Policy YAML** — Awk loose YAML-like conf → Go will standardize later (`06-policy`); not a match/backup blocker.

---

## Match

- **Root ds_suffix** — Awk/Go both use `""` for tree root; human mode prints `[LEAF]` (source basename), `-H` empty. *Freeze:* goldens under `testdata/golden/match/`.
- **List flags outside LIST template** — Awk adds `-t all -Screatetxg` (+ `-dN`) via `flags` var into LIST → Go `cmdbuild.ListArgv` hardcodes the same flag string after TSV `-Hpr -o`. *If oracle list shape drifts, change ListArgv flags, not Real ad hoc.*
- **Missing target list** — Awk ignores `dataset does not exist` on tgt list → empty tree → `syncable (full)`. Go `Real.List` same (`isMissingDataset`). *Example:* backup to brand-new `bpool/zelta-go-test`.
- **Filter warnings** — Awk stderr `warning: invalid filter pattern '…'` → Go `match.Result.Warnings`; match + backup CLIs print `warning: …` on stderr.
- **`--time` location** — times on **stdout** after table (both). Not stderr.
- **ivset / origin match columns** — Awk MATCH_IVSET / origin paths → Go not fully wired; basic GUID match only unless extended.

---

## Backup / send-recv

- **Dry-run intermediate full = first pass only** — Awk `run_backup`: dry-run skips second `run_zfs_sync`; execute runs full@`src_next` then incr→`src_last`. Go same: dry-run one `+` line @earliest; execute two `RunPipe`s when `FinalEnd` set. *Example:* `-I` full dry-run shows `@snap1` only; after execute tgt has full snap history.
- **Post-snap plan without re-list** — Awk may refresh match after snap; Go `ApplySourceSnap` rewrites ends in-memory (predicted). *Assumes* recursive `@name` exists on every syncable src DS after `zfs snapshot -r`.
- **Send/recv flags via opt** — Awk Opt[] from env/CLI → Go `opt.Parse`/`opt.SendRecvFrom` (full chain: defaults → zelta.env → process env → CLI); `backup.Request.Flags` override.
- **Volume type** — backup lists with `BackupListProps` (+`type`); `Pair.SrcType` → RECV_VOL vs RECV_FS. Match default list still omits `type` (not needed for compare cols).
- **Quoting on dry-run** — Awk often single-quotes paths inside ssh; Go `SoftJoin` quotes only when needed (spaces/meta). *Pixel:* same tokens, different quotes — not a behavioral bug.
- **Progress / -P parse** — Awk parses send `-P` size / recv `received` into `syncing: …` → Go dry-run has `would sync N` + `+` cmds; execute mostly quiet (pipe output not summarized yet).
- **Parent create** — Awk/Go both `CREATE` parent when tgt missing (default on); dry-run still creates (no `+` line). OpenZFS readonly `-up` bug: retry same CREATE up to depth−1 (`ErrReadOnlyCreate`). `already exists` → OK.
- **Push/pull dual-remote** — Awk/Go both: default PULL (pipe on target), `--push` (pipe on source), `--no-pull`/SYNC_DIRECTION=0 → controller `ssh|ssh` proxy + one warning. Same remote still hairpins. Execute + dry-run shapes mirror `get_sync_command`.
- **Filtered intermediate** (`-I` + include/exclude) — Awk carries filtered
  snapshot state through recursive child handling and a brute-force prune
  send-range loop; Go retains full source history for backup, selects snapshots
  independently per dataset, and emits one forced `-i` stream per selected
  endpoint. This is an intentional architecture diversion: the per-dataset
  replication plan is straightforward in Go (and should be reusable in Ruby),
  while expressing the same stateful recursion in Kernighan Awk is a major
  source of complexity. *Where:* `internal/match`, `internal/backup`.
- **Backup forces written list props** — even if match default cols would skip written, backup passes `match.BackupListProps` so snap-if-needed sees `written` and recv sees `type`.
- **Bookmark MVP** — Go verifies each successfully executed target snapshot and creates the corresponding source bookmark; default naming uses `<target-host>_`, explicit `BOOKMARK_PREFIX` is honored. Dry-run renders both verification and creation commands; bookmark failures continue and produce a non-zero replication status. Clone/revert paths remain excluded.

---

## Prune

- **`--prune-guard=unsynced` prunes nothing** — Awk 1.2.0 `synced_allows_prune` never fires against the guard target in prune mode (verified against full/partial/empty/missing guards). Go matches: unsynced keeps every snap. Likely an oracle bug; re-check when oracle moves.
- **Visual prefix** — Awk prints `Source[ID]` (`root@debian:apool/…`) before ❌/🔹 lines; Go prints the bare dataset name.
- **`listing source/target` noise** — Awk `-v` prints these LOG_INFO lines; Go `-v` prints only `keeping: …` (matches LOG_INFO keeping content).
- **Bookmarks** — Awk tracks bookmarks in rows but skips in analysis; Go skips them at load (same net effect).

---

## cmdbuild / zfs exec

- **SNAP always `-r` in TSV** — non-recursive Snapshot path bypasses template (rare).
- **Pipe hairpin** — same remote host → one `ssh -n host "{ send | recv ; }"` (oracle). Different hosts → two ssh; recv side **must not** use `-n` (`StdinNull(RECV)==false`).
- **LIST binary** — TSV says `zfs`; `Real.ZFS` rewrites argv[0] when overridden.

---

## CLI / product surface

- **No man pages in-repo** — point at `~/Code/zelta/doc` / `ZELTA_DOC`.
- **Verbs** — Go currently exposes `match`, `backup`, `prune`, root `clone`, root
  `revert`, and root direct-divergence `rotate`; recursive and clone-origin
  variants remain intentionally incomplete.
- **Private Gitea** — not public until Daniel says docs are real.

---

## Options / env resolution

- **zelta.env values not shell-eval'd** — Awk `eval`s file values as shell words (live quotes, `$(...)`) → Go stores raw, stripping one quote layer; no command substitution. *Why:* no shell in the Go binary; shipped oracle examples use `SNAP_NAME='$(date …)'` which Go generates natively instead. *Where:* `internal/conf.LoadEnvFile`.
- **Env injection into process env** — Awk loads file vars into shell env for awk children → Go `cmd/zelta` `os.Setenv`s only unset-or-empty `ZELTA_*` (`:=` semantics) at startup; library `opt.Lookup` paths stay file-aware. *Effect:* empty exported `ZELTA_X=""` loses to the file (oracle parity).
- **CLI parser from opts.tsv** — Awk `zelta-args.awk` → Go `opt.Parse`: exact flags, `--opt=v`/`--opt v`, short bundling, first operand/`--` ends flags, incr/decr env-seeded, arglist appends the flag itself, legacy-alias global latch (alias **overwrites** primary — oracle quirk). Old Go-only flags (`--snap`, `--no-snap`, `--nosnap`, `--nopull`) dropped for oracle names (`--snapshot`, `--no-snapshot`, `--no-pull`).
- **`-h` on send/recv verbs: no warning** — oracle opts.tsv puts the "ambiguous" text in the DESCRIPTION column ($7), WARNING ($8) is empty → oracle emits none; Go matches (data quirk, not code).
- **SNAP_MODE `SKIP` ≈ `IF_NEEDED`** — `--snapshot-skip` parses but Go backup has no SKIP semantics yet; treated as IF_NEEDED.
- **CLI error prefix** — oracle `stop()` prints `error: <msg>`; Go parse/depth errors use `error:` too, engine errors keep `zelta <verb>:`.
- **RECV_PROPS_ADD/DEL (`-o`/`-x`)** — repeated values are preserved and emitted as ordered `-o value`/`-x value` receive argv pairs; `RECV_OVERRIDE` still replaces the composed receive flags. *Where:* `internal/opt/sendrecv.go`, `internal/backup/plan.go`.
- **Bare-key process env** — Go `opt.Lookup` also honors bare `KEY` (no `ZELTA_`); oracle reads only `ZELTA_*`. Superset kept for library ergonomics.

---

## Clone / revert / rotate

- **Clone scope** — Awk also supports a four-endpoint clone-and-backup form →
  Go now supports recursive/latest root cloning with depth and target
  preflight, but the four-endpoint workflow remains deferred. It is a composer
  of ordinary `clone` followed by `backup --clone-origin`, not a new clone
  primitive. *Where:* `internal/lineage`, `cmd/zelta/clone.go`.
- **Revert scope** — Root and recursive latest/explicit planning, preservation
  collision checks, preserved-tree clone sources, and the post-revert snapshot
  are now wired, including snapshot-name option plumbing. Partial-failure
  recovery/reporting remains deferred. *Where:* `internal/lineage`,
  `cmd/zelta/revert.go`.
- **Rotate lineage** — Awk handles source rollback/direct target divergence
  through the same direct-match rotation path and uses source `origin` only for
  clone-created trees → Go now carries dataset `origin` through match and
  plans recursive direct-divergence plus verified source-clone-origin paths.
  No external rollback/target-cause classification is attempted.
- **Rotate receive lifecycle** — The final destination after preserving a
  divergent target is not frozen yet. The current dry-run planner emits the
  preservation rename and receives into the original target path with the
  preserved target snapshot as `origin`, matching the bounded oracle contract.
  Exact rollback failure recovery remains. Execution is available through
  `internal/rotate.Execute`; it re-runs match after execution and reports
  remaining divergence like upstream, without a strict confirmation failure.
   Source snapshot and preservation failures stop safely; independent child
   stream failures continue and are returned as structured partial progress.
   Exact receive-token/rollback recovery remains manual.
- **Lineage dry-run remotes** — Backup dry-run uses endpoint-aware pipe
  formatting, and Clone/Revert/Rotate now use endpoint-aware command
  formatting. *Where:* `internal/lineage`, `internal/rotate`,
  `cmd/zelta/{clone,revert,rotate}.go`.
- **Snapshot thresholds** — Go now collects `snapshots_changed` when
  `SNAP_TIME`/`SNAP_SIZE` are configured and applies conjunctive thresholds in
  `IF_NEEDED` mode to backup and Rotate. Invalid or missing threshold data
  forces a snapshot; `ALWAYS` and `NEVER` remain authoritative. *Where:*
  `internal/backup/snap.go`, `cmd/zelta/rotate.go`.
- **Rotate snapshot phase** — Go now predicts/executes the recursive source
  snapshot when Rotate is at the common latest snapshot, when source state is
  written, or when snapshot mode is forced. Threshold-based snapshot skipping
  is handled by the threshold rule above.

---

## How to add an entry

1. Confirm it is **intentional** (or a known gap with a reason), not a random bug.
2. One bullet under the right section; include a concrete example or file path.
3. If it becomes parity work, move the actionable bit to the package `## Defer` and leave a one-line pointer here.
4. Pixel output truth stays in goldens — do not duplicate full tables here.
