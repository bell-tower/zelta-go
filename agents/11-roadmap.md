# Roadmap

This is the ordered backlog after checkpoint `069979f`. Keep implementation
work in small commits and update this file when a contract changes.

## Current phase: hardening and evidence

Pause feature breadth while the current experimental surface is classified and
verified. The immediate work is to reconcile docs, establish capability
maturity, and run bounded disposable-ZFS checks. Do not add a new verb,
implement `zprune`, or design the public facade inside an unrelated bug-fix
session.

## Current library boundary

The useful exported implementation symbols are currently internal:

- `internal/match`: `Compare`, request/result/tree types, rendering and filters
- `internal/backup`: `Run`, request/result, planning and execution types
- `internal/prune`: `Run`, retention analysis and formatting
- `internal/zfs`: `Executor`, `Real`, `Fake`, and pipe support
- `internal/cmdbuild`: data-driven command construction
- `internal/endpoint`: endpoint parsing and dataset suffix handling
- `internal/opt`: defaults, environment resolution, and opts.tsv parsing
- `internal/lineage`: clone and revert planning/execution
- `internal/rotate`: rotate planning/execution and failure reporting

The Go module is therefore not yet a public library. The next API decision is
to promote a curated facade, likely under top-level packages, or explicitly
keep this repository as a private CLI. Until that decision is made, avoid
making `internal` types part of a promised external contract.

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
7. **MVP complete:** policy dry-run (`-n`/`-H`/`-C`, import, fan-out, operand
   filter, prefix resolution, `-n -v` command dump) matches the centralized
   example oracle. **Complete:** sequential job execution with env forwarding
   and per-job `LOG_PREFIX` injection. Next: `JOBS`/`RETRY` for parallel
   dispatch and retry; `--backup-command` and backup-flag forwarding remain
   deferred; keep precedence tests for conf vs env/CLI tight as execution lands.
8. Stabilize the practical work-alike CLI and its cross-platform evidence
   before committing to the public Go library facade. Keep the facade decision
   explicit, then promote or re-export packages and add external-package tests.
9. Use the upstream Bourne `zprune` for feature-parity testing for now. Defer a
   new Go `zprune` wrapper until the CLI and policy surfaces justify it; when
   implemented, keep destructive operations separate from core `zelta prune`.
10. Polish README and public API documentation after Zelta Policy and the API
    behavior are stable.

## Explicit non-goals

- Do not add S3/blob support to this Go line.
- Do not make `zprune` silently destructive.
- Do not treat the current exported `internal` symbols as a public API.
