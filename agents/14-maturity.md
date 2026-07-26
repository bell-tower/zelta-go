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
| Backup execution | `REAL-ZFS-VERIFIED` | interrupted receive behavior, resume tokens |
| Backup resume-token recovery | `FAKE-VERIFIED` | interrupted disposable-ZFS fixture and cross-platform evidence |
| Read-only prune | `GOLDEN-VERIFIED` | deferred clone-origin/send-range cases |
| Clone/revert planning | `REAL-ZFS-VERIFIED` | collision, missing-snapshot, and remote lifecycle cases |
| Rotate planning | `GOLDEN-VERIFIED` | receive-token and rollback recovery |
| Rotate execution | `REAL-ZFS-VERIFIED` | interrupted receive, child failure/recovery lifecycle |
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
`root`, `space`, and `djbell`. `space` is the ordinary non-root account;
`djbell` was created from the root account with the same authorized key so
non-root Go/CLI checks can use the usual identity. Run pool import/export and
other ZFS administration as `root`. No capability is promoted in the maturity
matrix by this lab setup alone.

### Repeat From Scratch

The following procedure is deliberately explicit so a lost context does not
require reconstructing the fixture or pool contract. It assumes the host is
one of the disposable lab hosts and that any existing same-named pool has been
identified before it is exported. Do not run it against a production host.

1. Download the two assets from the immutable release tag and verify their
   digests locally:

   ```sh
   mkdir -p /tmp/zelta-golden-pools
   curl -fL -o /tmp/zelta-golden-pools/apool.img.gz \
     https://github.com/bell-tower/zelta/releases/download/test-fixtures/v1/apool.img.gz
   curl -fL -o /tmp/zelta-golden-pools/bpool.img.gz \
     https://github.com/bell-tower/zelta/releases/download/test-fixtures/v1/bpool.img.gz
   shasum -a 256 /tmp/zelta-golden-pools/*.img.gz
   ```

   Expected SHA-256 values are listed above. The release commit is
   `e760ffc8c6db6bbbb4a7486533ef109b43205630`.

2. Before staging, inspect the destination and preserve any pre-existing
   images under a separate directory. The current installations use
   `/root/zelta-golden-pools` on `dev2` and `/zfs-storage/zelta-golden-pools` on
   Debian; the preserved images are under `zelta-existing` on each host.

   ```sh
   ssh root@HOST 'zpool status -P; df -h /root /zfs-storage 2>/dev/null || true'
   ssh root@HOST 'mkdir -p /root/zelta-golden-pools'
   scp /tmp/zelta-golden-pools/*.img.gz root@HOST:/root/zelta-golden-pools/
   ssh root@HOST 'gzip -dc /root/zelta-golden-pools/apool.img.gz > /root/zelta-golden-pools/apool.img && gzip -dc /root/zelta-golden-pools/bpool.img.gz > /root/zelta-golden-pools/bpool.img'
   ```

   Use `/zfs-storage/zelta-golden-pools` instead of the `/root` path on Debian.
   Adjust all three path occurrences to match the host path. ZFS imports the
   uncompressed `.img` files; retain the `.img.gz` files as the verified
   source assets. Preserve, do not overwrite, unrelated images or an
   unverified same-named pool.

3. As root on the disposable host, import the released pools from the image
   directory. If the pools are already imported, verify their vdev paths and
   health instead of importing again:

   ```sh
   zpool import -d /root/zelta-golden-pools apool
   zpool import -d /root/zelta-golden-pools bpool
   zpool status -P apool bpool
   zfs list -r apool/treetop
   zfs list -r bpool/backups
   ```

   On Debian, substitute `/zfs-storage/zelta-golden-pools`. The expected pool
   health is `ONLINE` with zero read, write, and checksum errors.

4. From this repository, run the baseline match check for the host:

   ```sh
   go run ./cmd/zelta match -H \
     'root@HOST:apool/treetop' 'root@HOST:bpool/backups'
   ```

   The expected result is `up-to-date`, including matching snapshot GUIDs
   through `2026-06-14_21.00.00`. Capture `zpool status -P`, `zfs list -r`, and
   this match output with any later evidence.

5. To remove only this lab installation, first confirm the pool names and
   vdev paths with `zpool status -P`, then export the pools. Keep the compressed
   assets and preserved `zelta-existing` images. Never destroy a pool merely
   because it has the expected name; the disposable-host assignment and vdev
   path must agree first.

### SSH Bootstrap

Debian currently has `space` (UID 1000) and `djbell` (UID 1001). If a fresh
disposable Debian image lacks `djbell`, root may recreate the ordinary account
and copy the existing root key without putting the key material in this repo:

```sh
ssh root@debian useradd --create-home --shell /bin/bash djbell
ssh root@debian 'install -d -m 700 -o djbell -g djbell /home/djbell/.ssh && install -m 600 -o djbell -g djbell /root/.ssh/authorized_keys /home/djbell/.ssh/authorized_keys'
ssh djbell@debian id
```

Use `space@debian` when a non-root identity is preferable. The account
bootstrap does not grant sudo and is only for ordinary CLI/configuration
checks; root remains required for pool administration.

## Real-ZFS Evidence: Debian Scratch Pool

This bounded run used Debian 12/OpenZFS 2.3.2 on 2026-07-26 with a fresh
file-backed pool:

| Item | Value |
|---|---|
| Pool image | `/zfs-storage/zelta-go-lab.img` (512 MiB) |
| Pool | `zlab` |
| Source | `zlab/source` with `zlab/source/child` |
| Backup target | `zlab/backup` |
| Clone target | `zlab/clone` |
| Initial source snapshots | `base1`, then `inc1`, then `inc2` |
| Final pool health | `ONLINE`, zero read/write/checksum errors |

The source fixture used `root.txt` and `child/child.txt`. The initial snapshot
contained `v1-root` and `v1-child`; the incremental changed the root to
`v1-root` plus `v2-root` and changed the child to `v2-child`.

### Backup

The dry-run emitted:

```text
would sync 2 datasets
+ ssh -n root@debian "{ zfs send -P -L -c -e zlab/source@base1 | zfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s zlab/backup ; }"
+ ssh -n root@debian "{ zfs send -P -L -c -e zlab/source/child@base1 | zfs recv -v -u -x mountpoint -o canmount=noauto -s zlab/backup/child ; }"
```

The full execution emitted exactly `2 full`. The target had matching `base1`
GUID `12217196464836800807`; both target datasets had `readonly=on` and
`canmount=noauto`.

After creating source snapshot `inc1` (GUID `17668740559494353207`),
`backup --no-snapshot` emitted exactly `2 incremental`. The target received
the matching `inc1` GUID and retained the read-only/noauto properties.

For divergence, the target received a target-only file and snapshot
`diverged` (GUID `13622520561543807623`), while the source advanced to `inc2`
(GUID `14682053338455067165`). The command:

```text
go run ./cmd/zelta backup --no-snapshot root@debian:zlab/source root@debian:zlab/backup
```

emitted exactly `1 incremental, 1 blocked` and exited 1. The target-only file
remained present and no `inc2` snapshot was received. This promotes the normal
full, incremental, and divergent-target refusal path to `REAL-ZFS-VERIFIED`;
interrupted receive and resume-token recovery remain unverified.

### Clone And Revert

The clone dry-run emitted:

```text
ssh -n root@debian 'zfs clone -p -o readonly=off zlab/source@inc2 zlab/clone'
ssh -n root@debian 'zfs clone -p -o readonly=off zlab/source/child@inc2 zlab/clone/child'
```

Execution succeeded with no output. Both clone roots were writable and had
the expected origins:

```text
zlab/clone       origin zlab/source@inc2       readonly off
zlab/clone/child origin zlab/source/child@inc2 readonly off
```

The clone accepted a write marker without changing the source. Revert was
run against `source@inc1`. Its dry-run emitted rename, two clone operations,
and a final recursive snapshot. Execution emitted exactly:

```text
to retain replica history, run: zelta rotate 'root@debian:zlab/source' 'TARGET'
```

The current source contained `v1-root` plus `v2-root` and `v2-child`; the
preserved `zlab/source_inc1` retained `v3-root` and `v2-child`. The replacement
source had origins `zlab/source_inc1@inc1` and
`zlab/source_inc1/child@inc1`, with final snapshot
`zlab/source@zelta_2026-07-26_03.07.19` (GUID
`11226628764449256513`). This promotes the normal clone/revert lifecycle only.

### Rotate

The first direct-source attempt used `zlab/source_inc1` (a non-clone source)
and `zlab/backup`. It exposed this defect before the fix:

```text
zelta rotate: send: sync : zfs pipe on root@debian: exit status 1
cannot receive incremental stream: destination 'zlab/backup' does not exist
zelta rotate: send /child: sync /child: zfs pipe on root@debian: exit status 1
cannot open 'zlab/source_inc1/child@zelta_2026-07-26_03.08.21': dataset does not exist
zelta rotate: preserved target remains at zlab/backup_inc1; incomplete children require manual recovery
```

The preserved target was restored without destruction. The fix makes direct
rotation seed the common source snapshot into the new target, then apply the
incremental stream. The corrected dry-run emitted, in order:

```text
ssh -n root@debian "{ zfs send -P -L -c -e zlab/source@base1 | zfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s zlab/backup ; }"
ssh -n root@debian "{ zfs send -P -L -c -e -I zlab/source@base1 zlab/source@inc1 | zfs recv -v -o readonly=on -u -x mountpoint -o canmount=noauto -s zlab/backup ; }"
ssh -n root@debian "{ zfs send -P -L -c -e zlab/source/child@base1 | zfs recv -v -u -x mountpoint -o canmount=noauto -s zlab/backup/child ; }"
ssh -n root@debian "{ zfs send -P -L -c -e -I zlab/source/child@base1 zlab/source/child@inc1 | zfs recv -v -u -x mountpoint -o canmount=noauto -s zlab/backup/child ; }"
```

Execution emitted exactly:

```text
to ensure target is up-to-date, run: zelta backup root@debian:zlab/source root@debian:zlab/backup
```

The received `base1` and `inc1` GUIDs matched the source for both root and
child. The target contained `v1-root` plus `v2-root` and `v2-child`, while the
preserved divergent target remained at `zlab/backup_base1` with its target-only
snapshot. The pool remained `ONLINE` with zero errors.

The clone-origin regression used `zlab/clonesrc` (originating at
`zlab/source@inc1`) and `zlab/backup`. Execution succeeded and emitted exactly:

```text
to ensure target is up-to-date, run: zelta backup root@debian:zlab/source root@debian:zlab/backup
```

The received root and child had origins `zlab/backup_inc1@inc1` and
`zlab/backup_inc1/child@inc1`; the received `clone1` snapshot GUID matched the
source at `16212045387473677639`. The preserved target remained at
`zlab/backup_inc1`. Together with the corrected direct case, this promotes
normal direct and clone-origin rotation to `REAL-ZFS-VERIFIED`; interrupted
receive, rollback, and child-recovery behavior remain unverified.

## Rules

- Every new capability starts at `PLANNED` in the matrix.
- Name the exact fixture, test, or host when promoting a state.
- Keep destructive operations below `RELEASE-APPROVED` until Daniel explicitly
  signs off.
- When a known safety gap is discovered, lower the ceiling rather than hiding
  the gap in a prose note.
