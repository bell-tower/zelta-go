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

## Smallest Clone/Revert Evidence Set

The minimum disposable-ZFS lifecycle check is on the `debian` test VM with a
newly created scratch pool and one recursive source tree:

1. Create a recursive source snapshot, clone it to a new target, and verify the
   target tree, dataset suffixes, writable state, and source preservation.
2. Revert the source tree to a selected snapshot and verify the current tree is
   preserved under the snapshot-derived name before the replacement clone is
   created.
3. Verify the resulting source tree and clone origins with `zfs list` and
   representative file content.

This checks the normal non-destructive clone/revert lifecycle only. Collision,
missing-snapshot, and remote-endpoint cases remain deterministic-test or manual
evidence gaps unless separately exercised.

## Smallest Rotate Evidence Set

The minimum disposable-ZFS execution check uses the same scratch pool after
the clone/revert check:

1. Run a direct-match rotation with a changed source and verify the target is
   preserved, the receive completes recursively, and the target matches the
   source snapshot tree.
2. Run a source-origin rotation from a cloned source and verify the preserved
   target origin and received `origin` property are correct.
3. Inject or induce a child-level failure only if the disposable harness can
   do so safely, and verify independent child work is reported without
   retrying an interrupted receive.

These checks establish normal direct and source-origin rotation behavior only.
Receive-token discovery, `zfs send -t`, and rollback recovery remain manual
evidence gaps.

## Golden Pool Lab

Richard's released prune fixtures are installed as file-backed pools from
release `test-fixtures/v1` (`bell-tower/zelta`):

| Host | ZFS | Golden image directory | Baseline result |
|---|---|---|---|
| `dev2` | FreeBSD 15.1, OpenZFS 2.4.2 | `/root/zelta-golden-pools` | `match`: up-to-date |
| `debian` | Debian 12, OpenZFS 2.3.2 | `/zfs-storage/zelta-golden-pools` | `match`: up-to-date |

The compressed fixture digests are:

- `apool.img.gz`: `4b7d62dab4e5ca1eb73c69a4d783af7e649f21af07b0c2ea31c93d9da2bf8fda`
- `bpool.img.gz`: `7dfd82ca545bde60236dd2cc93d3ea24e3720ae90a86bca350049815392b9edc`

Both hosts import `apool` and `bpool` from the released images with zero
read/write/checksum errors. The expected roots are `apool/treetop` and
`bpool/backups`, with matching snapshot GUIDs through the `2026-06-14_21.00.00`
endpoint. The prior host-local images were preserved under `zelta-existing`.

`dev2` accepts SSH as both `djbell` and `root`. Debian currently accepts
`root`; `djbell@debian` is rejected by public-key authentication and remains an
access setup gap. No capability is promoted in the maturity matrix by this lab
setup alone.

## Rules

- Every new capability starts at `PLANNED` in the matrix.
- Name the exact fixture, test, or host when promoting a state.
- Keep destructive operations below `RELEASE-APPROVED` until Daniel explicitly
  signs off.
- When a known safety gap is discovered, lower the ceiling rather than hiding
  the gap in a prose note.
