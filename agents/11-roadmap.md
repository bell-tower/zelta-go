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
2. Implement remaining backup parity: send-check, bookmarks, clone-origin,
   and filtered intermediate sends. Add one contract note and focused tests per
   behavior rather than broad rewrites.
3. Decide and document the public library facade. Only then move or re-export
   packages and add external-package tests.
4. Implement `zprune` as a separate destructive wrapper with prompt, force,
   guard, and send-range semantics. Keep destructive operations out of core
   `zelta prune`.
5. Implement policy configuration (`zelta.conf`) after the option/env contract
   is stable; add precedence tests against `zelta.env`, process environment,
   and CLI flags.
6. Polish README and public API documentation after the API and behavior are
   stable.

## Explicit non-goals

- Do not add S3/blob support to this Go line.
- Do not make `zprune` silently destructive.
- Do not treat the current exported `internal` symbols as a public API.
