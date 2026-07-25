# Capability Maturity

This document prevents passing unit tests from being mistaken for release
readiness. Update it when a capability crosses a state; do not promote a state
because a model believes the code is probably correct.

## States

| State | Meaning |
|---|---|
| `PLANNED` | Contract or intent exists; implementation is absent or skeletal. |
| `IMPLEMENTED` | Code path exists, but coverage and behavior boundaries may be incomplete. |
| `FAKE-VERIFIED` | Deterministic tests exercise the path with `zfs.Fake` or injected failures. |
| `GOLDEN-VERIFIED` | Output/plan matches committed oracle fixtures for named cases. |
| `REAL-ZFS-VERIFIED` | The path has been exercised against a disposable ZFS target. |
| `CROSS-PLATFORM-VERIFIED` | Real verification exists on the relevant FreeBSD, Illumos, and/or Linux targets. |
| `RELEASE-APPROVED` | Daniel explicitly approves the capability for release or unattended use. |

States are cumulative only when the later test covers the same behavior. A
golden test does not imply real ZFS verification. A dry-run test does not imply
execution safety. `RELEASE-APPROVED` is never inferred from green CI.

## Current matrix

| Capability | Current ceiling | Main missing evidence |
|---|---|---|
| Endpoint parsing | `FAKE-VERIFIED` | broader production endpoint corpus |
| Match planning/rendering | `GOLDEN-VERIFIED` | wider oracle parity, platform list behavior |
| Backup dry-run planning | `GOLDEN-VERIFIED` | full edge-case oracle coverage |
| Backup execution | `FAKE-VERIFIED` | disposable real ZFS, interrupted receive behavior |
| Read-only prune | `GOLDEN-VERIFIED` | deferred clone-origin/send-range cases |
| Clone/revert planning | `GOLDEN-VERIFIED` | real disposable ZFS lifecycle |
| Rotate planning | `GOLDEN-VERIFIED` | receive-token and rollback recovery |
| Rotate execution | `FAKE-VERIFIED` | real failure/recovery lifecycle |
| `zprune` destructive wrapper | `PLANNED` | implementation plus safety review |
| Policy configuration | `PLANNED` | stable option/env contract |
| Public Go library | `PLANNED` | curated facade and external-package tests |
| Release binary | `IMPLEMENTED` | release reproducibility and platform verification |

## Smallest Backup Evidence Set

The minimum disposable-ZFS check for promoting backup execution beyond
`FAKE-VERIFIED` is on the `debian` test VM with a newly created scratch pool:

1. Run one recursive full backup to an absent target and verify received data,
   snapshots, and receive properties.
2. Change source data, run one incremental backup, and verify the target has
   the new data and matching snapshot.
3. Repeat the incremental case with a pre-existing divergent target snapshot
   and verify execution refuses to overwrite it.

These checks establish normal full, incremental, and divergence safety only.
Interrupted receive recovery, resume tokens, and rollback behavior remain
manual evidence gaps and must not be inferred from this set.

## Rules

- Every new capability starts at `PLANNED` in the matrix.
- Name the exact fixture, test, or host when promoting a state.
- Keep destructive operations below `RELEASE-APPROVED` until Daniel explicitly
  signs off.
- When a known safety gap is discovered, lower the ceiling rather than hiding
  the gap in a prose note.
