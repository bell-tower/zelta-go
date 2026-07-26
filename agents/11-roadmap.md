# Roadmap

This is the ordered backlog after checkpoint `069979f`. Keep implementation
work in small commits and update this file when a contract changes.

## Current phase: Sylve integration (Phase D PoC)

Phases A–C done. Phase D **PoC on devhost**: Sylve `SYLVE_ZELTA_GO=1` gates
`backup.Run` behind `backupWithEventProgressSnapshotNameRecursive`; default
still embedded Awk. Details and verified smoke: `agents/16-sdk.md` Phase D.
Transport: OpenSSH client only in zelta-go; Sylve cluster SSH server unchanged.

## Current library boundary

Public packages at module root:

- `match`: `Compare`, request/result/tree types, rendering and filters
- `backup`: `Run`, request/result, planning and execution types
- `prune`: `Run`, retention analysis and formatting
- `zfs`: `Executor`, `Real`, `Fake`, SSHConfig, and pipe support
- `endpoint`: endpoint parsing and dataset suffix handling
- `report`: column expansion (`cols.tsv`), byte formatting, and
  JSON output structures (`json.go`, `BackupResult`)
- `lineage`: clone and revert planning/execution
- `rotate`: rotate planning/execution and failure reporting

Stay internal: `cmdbuild`, `opt`, `conf`, `policy`.

## Ordered work

1. **Complete for normal paths:** documentation reconciliation and the
   smallest disposable real-ZFS verification set are complete on Debian.
   Backup, clone/revert, direct-source rotate, and clone-origin rotate passed.
   Interrupted receive, resume-token, and child-recovery behavior are now
    real-ZFS verified over a remote stream. Native rollback is covered by the
    divergent-target rotate continuity contract rather than a separate test.
2. **Complete:** prune golden fixtures and CLI integration coverage now cover
   the current read-only analysis; keep clone-origin and send-range cases
   deferred with zprune.
3. **Complete:** Bookmark MVP covers verification, creation, dry-run rendering,
   and non-fatal failure status; clone/revert exclusions remain explicit.
4. **Complete:** the four-endpoint clone-and-backup workflow composes ordinary
   `clone` with `backup --target-origin`; orchestration remains separate from
   lineage primitives.
5. **Complete for normal planned/execution paths:** Rotate planning, direct and
   clone-origin execution, and failure reporting are covered by deterministic
   and disposable-ZFS evidence. Native rollback is covered by divergent-target
    continuity; interrupted receive and child recovery are verified in backup
    execution.
6. **Complete for planned filtered sends:** snapshot creation, final bookmarks,
   recursive filter precedence, and zero-eligible no-ops are covered;
   receive-token discovery and `zfs send -t` recovery are real-ZFS verified
   over remote root and child interruptions. Retain the reusable planning
   abstraction rather than copying the Awk loop blindly.
7. **Complete:** policy dry-run (`-n`/`-H`/`-C`, import, fan-out, operand
   filter, prefix resolution, `-n -v` command dump) matches the centralized
   example oracle. **Complete:** sequential and parallel job execution with
   env forwarding, per-job `LOG_PREFIX` injection, `JOBS`-driven parallel
   dispatch (AWK `should_xargs` parity), and `RETRY` retry loop. `--backup-command`
   and backup-flag forwarding remain deferred; keep precedence tests for conf
   vs env/CLI tight as execution lands.
8. **Complete (A–C):** public SDK packages, `zfs.Remote`/`SSHConfig`/
   `CommandRemote`, `backup.OnLine` + `ErrCode`, `sdk/` external smoke.
   **Next (D, out of repo):** Sylve cutover — see drop-in map in
   `agents/16-sdk.md`.
9. **Complete:** `zprune` implemented as a direct Go verb (+ argv[0] dispatch
   for `zprune` binary/symlink). Uses in-process `prune.Run` (no ipc-*),
   prints candidate output, confirms (unless `--force`), and executes `zfs
   destroy` grouped by dataset. Dry-run behavior matches `zelta prune`.
10. **Complete:** `install.sh` workalike with `PRE_RUN='make'` at top; gracefully
    skips `share/` when absent (embedded in Go binary). Closely mirrors
    upstream structure.
11. **Complete:** cross-platform `make shelltest` (POSIX smoke tests) and
    Shellspec proof-of-concept (8/8 no-op tests pass); covered in
    `test/shell/basic_test.sh`.
12. **Complete:** `zelta backup --json` produces JSON output matching upstream
    Awk schema: `output_version`, flat fields, `sentStreams`, `errorMessages`.
    Populates source/target endpoints and stream counts from Plan; tracks
    startTime/endTime/runTime in execution path. `zelta policy --json` outputs
    JSON job table in dry-run mode and forwards `LOG_MODE=json` to child backup
    processes in execution mode.
13. **Deferred until SDK stabilized:** polish README and public API
    documentation. The README Library Status section and agents/ docs will be
    updated as part of the SDK promotion in item 8.

## Explicit non-goals

- Do not add S3/blob support to this Go line.
- Do not make `zprune` silently destructive.
- SDK promotion happens from `internal/` to top-level packages; do not expose
  `internal/cmdbuild`, `internal/opt`, `internal/conf`, or `internal/policy`
  as public API in v1. See `agents/16-sdk.md` for the curated boundary.
- Do not wire `data/json.tsv` into a TSV-driven JSON field loader. The Go
  `report.BackupResult` struct statically mirrors the same schema; the orphaned
  TSV is kept for reference but not parsed.
