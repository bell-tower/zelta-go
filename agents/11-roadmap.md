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

1. Add prune golden fixtures and CLI integration coverage. This closes the
   largest read-only behavior gap before destructive work.
2. Extend the bookmark MVP: execute-mode verification and creation are done;
   add dry-run rendering and oracle-compatible non-fatal failure status, while
   keeping clone/revert exclusions explicit.
3. Implement the prerequisite `clone` and `revert` lineage operations in
   dry-run-first form; recursive/latest planning, CLI wiring, execution, and
   post-revert snapshot behavior now exist. The four-endpoint clone workflow
   and failure recovery remain.
4. Implement the dry-run-first Rotate contract in `agents/12-rotate.md`;
   recursive direct-divergence and verified source-clone-origin planning now
   exist. Add oracle goldens, rollback classification, and execution only
   after the rename/receive lifecycle review.
5. Implement filtered intermediate sends. The Awk approach is useful but
   brute-force; record a possible reusable upstream abstraction while porting
   it rather than copying the loop blindly.
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
