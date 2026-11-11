# First-Pass Review Baseline

Date: 2026-08-08

## Initial Results

Before the fixture-path fix:

- `go test ./...`: passed.
- `go test -race ./...`: passed.
- `go vet ./...`: passed.
- `make build`: passed.
- `make shelltest`: passed.
- Match goldens: silently skipped with `no golden dir`.
- Prune goldens: silently skipped with `no golden dir`.
- Rotate goldens: silently skipped with `no rotate golden dir`.
- Policy goldens: ran and passed.

The three skipped suites used `../../testdata` from package directories. The
repository fixtures are at `../testdata` relative to those packages.

## First Slice

Changed only the fixture paths in:

- `match/golden_test.go`
- `prune/golden_test.go`
- `rotate/golden_test.go`

Focused results after the fix:

- Match: 29 golden cases passed.
- Prune: 3 golden cases passed.
- Rotate: 2 golden cases passed.

No goldens were regenerated.

## Baseline Decision

The first substantive review target is `zfs` plus `match` execution. The
baseline now exercises the match oracle fixtures, and the existing review has
already identified sequential source/target listing, command-rendering drift,
capture goroutine lifetime concerns, and shared executor state as likely
contract issues.

The libzfs work remains a bounded research spike. It must not become a cgo or
public API commitment until portability, packaging, privilege, and parity
questions are answered.

## Next Stop Condition

Before changing the ZFS model, produce a focused execution review containing:

- the actual runtime and dry-run command shapes;
- source/target list concurrency behavior;
- executor reuse and cancellation rules;
- race/failure-injection coverage gaps;
- a recommendation for the typed ZFS model and adapter boundary.
