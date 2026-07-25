# Roadmap

This is the ordered backlog after checkpoint `bf37bb1`. Keep implementation
work in small commits and update this file when a contract changes.

## Current library boundary

The useful exported implementation symbols are currently internal:

- `internal/match`: `Compare`, request/result/tree types, rendering and filters
- `internal/backup`: `Run`, request/result, planning and execution types
- `internal/prune`: `Run`, retention analysis and formatting
- `internal/zfs`: `Executor`, `Real`, `Fake`, and pipe support
- `internal/cmdbuild`: data-driven command construction
- `internal/endpoint`: endpoint parsing and dataset suffix handling
- `internal/opt`: defaults, environment resolution, and opts.tsv parsing

The Go module is therefore not yet a public library. The next API decision is
to promote a curated facade, likely under top-level packages, or explicitly
keep this repository as a private CLI. Until that decision is made, avoid
making `internal` types part of a promised external contract.

## Ordered work

1. Add prune golden fixtures and CLI integration coverage. Existing prune
   goldens and fixture-backed CLI coverage now cover the current read-only
   analysis; keep clone-origin and send-range cases deferred with zprune.
2. Bookmark MVP is complete for verification, creation, dry-run rendering, and
   non-fatal failure status; keep clone/revert exclusions explicit.
3. Compose the four-endpoint clone-and-backup workflow from ordinary `clone`
   plus `backup --clone-origin`; keep it as orchestration rather than a new
   lineage primitive.
4. Finish the Rotate lifecycle in `agents/12-rotate.md`: exact receive-token
   and rollback recovery remain manual; do not blindly retry interrupted
   receives.
5. Complete filtered intermediate sends across snapshot creation, bookmarks,
   resume handling, and oracle edge cases. The core per-dataset selector is
   implemented; retain the reusable planning abstraction rather than copying
   the Awk loop blindly.
6. Decide and document the public library facade. Only then move or re-export
   packages and add external-package tests.
7. Implement `zprune` as a separate destructive wrapper with prompt, force,
   guard, and send-range semantics. Keep destructive operations out of core
   `zelta prune`.
8. Implement policy configuration (`zelta.conf`) after the option/env contract
   is stable; add precedence tests against `zelta.env`, process environment,
   and CLI flags.
9. Polish README and public API documentation after the API and behavior are
   stable.

## Explicit non-goals

- Do not add S3/blob support to this Go line.
- Do not make `zprune` silently destructive.
- Do not treat the current exported `internal` symbols as a public API.
