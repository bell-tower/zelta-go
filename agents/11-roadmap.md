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
   Interrupted receive, resume-token, rollback, and child-recovery cases remain
   explicit manual gaps.
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
   and disposable-ZFS evidence. Exact receive-token, rollback, and child
   recovery remain manual, with no blind retry of interrupted receives.
6. **Complete for planned filtered sends:** snapshot creation, final bookmarks,
   recursive filter precedence, and zero-eligible no-ops are covered;
    receive-token discovery and `zfs send -t` recovery are implemented and
    fake-verified; disposable interrupted-receive evidence remains. Retain the
    reusable planning abstraction rather than copying the Awk loop blindly.
7. Decide and document the public library facade. Only then move or re-export
   packages and add external-package tests.
8. Implement `zprune` as a separate destructive wrapper with prompt, force,
   guard, and send-range semantics. Keep destructive operations out of core
   `zelta prune`.
9. Implement policy configuration (`zelta.conf`) after the option/env contract
   is stable; add precedence tests against `zelta.env`, process environment,
   and CLI flags.
10. Polish README and public API documentation after the API and behavior are
   stable.

## Explicit non-goals

- Do not add S3/blob support to this Go line.
- Do not make `zprune` silently destructive.
- Do not treat the current exported `internal` symbols as a public API.
