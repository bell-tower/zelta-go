# First-Pass Review Plan

This review precedes the Sylve PR. The purpose is to establish whether the Go
line is a safe, coherent Action Library and whether its observable behavior
matches the AWK oracle closely enough for an external consumer.

This is a review and contract-extraction exercise first, not a permission to
rewrite the repository. Each phase must produce evidence, a bounded decision,
and focused tests before implementation expands to the next phase.

## Review Rules

- Preserve the safety contracts in `agents/zelta-go/00-contracts.md`.
- Treat AWK behavior and checked-in goldens as the comparison oracle.
- Do not regenerate goldens without an explicit oracle comparison.
- Use Fake ZFS for unit and golden tests; use only disposable ZFS targets for
  integration work.
- Keep intentional deviations in `agents/zelta-go/10-deviations.md`.
- Stop when a finding requires an unresolved public API or safety decision.

## Phase 0: Baseline

Record the repository state, toolchain, recent commits, and results from:

```sh
go test ./...
go test -race ./...
go vet ./...
make build
make shelltest
```

Also verify that the match, backup, rotate, prune, and policy golden suites
load the fixtures under `testdata/golden`. Do not assume a passing test means a
fixture was exercised. The current match test path appears to resolve outside
the repository and must be checked first.

Deliverable: baseline note listing passing tests, skipped tests, broken test
paths, race findings, and environment limitations.

## Phase 1: Library Contract Map

Review the public packages:

```text
endpoint zfs match backup prune lineage rotate job zconf cmdbuild data
```

Map each package's Request -> Prepare/Plan -> Commands -> Run -> Result
lifecycle. Identify which types are public, which are CLI-only, and which
structures are currently carrying presentation, environment, or executor
concerns.

Deliverable: package responsibility map and list of public API decisions that
must be frozen before Sylve integration.

## Phase 2: ZFS Domain Model

Review `zfs/listparse.go`, `zfs/context.go`, `match/tree.go`, and all consumers
of `map[string]string` property bags.

Answer:

- Which ZFS properties deserve typed Go fields?
- Which properties are optional, version-dependent, or dataset-specific?
- How should unsupported properties and per-dataset capabilities be modeled?
- Can one domain model serve shell output, a future libzfs adapter, and tests?
- What information is lost by parsing `zfs list` and `zfs get` into strings?

The first step is a bounded libzfs research spike, not a cgo commitment. Compare
actual OpenZFS headers/API availability, portability, packaging, and privilege
requirements against the existing command adapter. Define an adapter boundary
before choosing an implementation.

Deliverable: typed ZFS model proposal, adapter interface, and libzfs spike
decision.

## Phase 3: Executor, SSH, and Concurrency

Review `zfs.Real`, `zfs.Fake`, `zfs.Remote`, `zfs.SSHConfig`,
`zfs.CommandRemote`, pipe execution, `match.Compare`, and policy execution.

Known review targets:

- `match.Compare` currently lists source and target sequentially while the
  oracle overlaps them.
- Captured stdout/stderr goroutines may outlive `cmd.Wait` and race with result
  reads.
- Shared `zfs.Real` state may not support concurrent library calls safely.
- Pipe telemetry may lose an unterminated final line.
- Dry-run SSH rendering may disagree with configured runtime transport.
- Child environments may contain duplicate variables instead of replacing
  inherited values.
- Policy retry handling may collapse per-job settings to one value.

Define ownership, cancellation, lifetime, reuse, error propagation, and
transport precedence. Add race and failure-injection tests before optimizing
concurrency.

Deliverable: executor/transport contract and concurrency test matrix.

## Phase 4: Match and Oracle Parity

Compare `match.Compare`, `match.Commands`, `match/tree.go`, `match/props.go`,
`zfs.ParseListLines`, and the AWK match implementation.

Verify list command identity, property ordering, same-endpoint handling,
bookmark semantics, dataset suffixes, depth filtering, feature probing,
blocked-pair behavior, malformed-row handling, and dry-run output.

Classify every difference as one of:

1. Go bug to fix.
2. Intentional documented deviation.
3. Missing oracle or test coverage.

Deliverable: match parity matrix with focused regression tests.

## Phase 5: Typed Serialization and Configuration

Review `job`, endpoint JSON, `internal/report`, `zconf`, golden metadata readers,
and the data files.

The target invariant is:

> External formats decode into typed Go structures before business logic runs.
> Action packages do not inspect JSON, YAML, maps, or CLI strings.

Decide how to handle polymorphic JSON, unknown fields, trailing input, absent
versus explicit zero values, versioning, and the intentional separation between
native SDK JSON and AWK-compatible CLI JSON. Determine whether `zconf` remains
a custom policy grammar or becomes an internal loader for the same typed
Requests used by `job`.

Deliverable: serialization/configuration contract and format ownership map.

## Phase 6: Generated Tables

Review `data/*.tsv`, `data/table.go`, `cmdbuild`, `internal/opt`, and
`internal/report`.

Keep TSV as the source of truth during the review. Design a deterministic
`go:generate` generator that validates schemas, field counts, duplicate keys,
references, and generated output. Migrate runtime parsing only after generated
structures reproduce current behavior. Explicitly classify `json.tsv` as
runtime data, generated data, or reference-only.

Deliverable: generation design and one small proof-of-concept generator.

## Phase 7: Safety Regression Review

Recheck `backup`, `rotate`, `lineage`, `prune`, policy execution, and CLI wiring
after the core contracts are understood.

The review must preserve these invariants:

- Blocked match means no send.
- Core verbs never use default `zfs recv -F`.
- Divergence is preserved, never overwritten.
- Rotate and revert remain nondestructive.
- Prune analysis remains separate from destruction.
- Policy execution does not silently bypass typed action APIs.

Deliverable: safety regression matrix and explicit unresolved risks.

## Sylve Readiness Gate

Do not start the Sylve PR until:

- Public request/result types are stable enough for an external consumer.
- Executor ownership, concurrency, and cancellation are documented.
- JSON import/export is typed and versioned.
- SSH and environment precedence have one clear contract.
- Golden tests exercise the checked-in fixtures.
- `go test -race ./...` is clean or every exception is understood.
- Generated data is reproducible.
- Remaining oracle deviations are documented.
- No safety or lifecycle ambiguity remains.

## First Step

Begin with Phase 0. The first implementation slice is not a refactor: it is a
baseline audit that fixes only test-discovery defects and records actual
failures. After that baseline, perform the bounded libzfs research spike and
the executor/concurrency review in parallel only if their contracts remain
independent.

The first stop condition is a committed baseline report containing:

- exact commands and results;
- verified golden fixture coverage;
- race detector findings;
- the first prioritized implementation package;
- explicit questions requiring a product decision.

Sylve remains deferred until that report is reviewed.
