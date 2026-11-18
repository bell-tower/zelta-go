![Zelta Logo](https://zelta.space/index/zelta-banner.svg)
# The Zelta Backup and Recovery Suite (Go)
*Development line: 1.3.0-dev, the planned Zelta 1.3 and successor to the Awk release*

---
> **Alpha.** zelta-go is pre-beta. Commands, output, and the Go API can still change without notice. For production backups, use [Zelta 1.2 (Awk)](https://github.com/bell-tower/zelta), the current stable release. When zelta-go reaches beta, it moves into the main Zelta repository as Zelta 1.3. Known differences from 1.2 are listed in [doc/COMPATIBILITY.md](doc/COMPATIBILITY.md).
>
> - **What's New:** Check [CHANGELOG.md](CHANGELOG.md) for the latest changes
> - **Found a Bug?** Please [open an issue](https://github.com/bell-tower/zelta-go/issues)
> - **Stable Release:** [Zelta 1.2 (Awk)](https://github.com/bell-tower/zelta)
>
>&nbsp;&nbsp;&nbsp;&nbsp;&nbsp;  ![CI](https://github.com/bell-tower/zelta-go/actions/workflows/ci.yml/badge.svg)
---

[zelta.space](https://zelta.space) | [Documentation](https://zelta.space/docs/) | [GitHub](https://github.com/bell-tower/zelta-go)

**Zelta** provides bulletproof backups that meet strict compliance requirements while remaining straightforward to deploy and operate. It transforms complex backup and recovery operations into safe, auditable commands—protecting your data without requiring specialized expertise.

Zelta orchestrates backup operations across any modern Unix system. It has been battle-tested in production for over six years, managing tens of millions of snapshots across thousands of systems—with zero runtime dependencies.

zelta-go is a drop-in replacement for the original Awk implementation. It accepts the same commands, flags, `ZELTA_*` environment variables, `zelta.env`, and `zelta.conf`, prints the same output, and returns the same exit codes.

> **Zelta requires ZFS.** If ZFS isn't yet part of your workflow, it's easier than ever to improve your infrastructure with ZFS and Zelta. See [Getting Started with ZFS](https://zelta.space/conf/zfs) to begin.

---

## Why Zelta?

### Compliance-Ready by Design
Zelta preserves every version of your data without destructive overwrites. When source and backup diverge, Zelta keeps both versions intact. Your team is already telling auditors you do this—Zelta makes it practical and verifiable.

### Safe Defaults, No Surprises
Backups are created read-only. Dangerous operations are rejected, not just discouraged. Core commands never destroy data and never require force flags to function correctly. Snapshot destruction lives in the separate `zprune` command, and `zfs recv -F` is only passed through when you ask for it explicitly.

### Zero Footprint on Endpoints
Zelta runs entirely from a management host using SSH. Your backup sources and targets need only standard system tools—no agents, no daemons, no additional attack surface.

### Portable and Dependency-Free
zelta-go is a single static binary with its manual pages built in. No package managers, no runtime dependencies, no version conflicts.

---

## Installation

### From Source
Requires Go 1.26 or newer to build.

```sh
git clone https://github.com/bell-tower/zelta-go.git
cd zelta-go
sudo ./install.sh          # or: make && sudo make install PREFIX=/usr/local
```

`install.sh` honors the same `ZELTA_BIN`, `ZELTA_SHARE`, `ZELTA_ETC`, and `ZELTA_DOC` variables as the original installer and installs into your home directory when run as a regular user. It never overwrites an existing `zelta.env` or `zelta.conf`.

The one-shot installer at `zelta.space` and the FreeBSD port install the stable Awk release, not zelta-go.

---

## First Backup

Zelta commands use endpoint syntax familiar from **scp(1)**:

```text
[user@][host:]pool/dataset[@snapshot]
```

Compare a source and target before backing up:

```sh
zelta match rpool/data backup-user@storage.example.com:tank/Backups/data
```

Create or update the backup:

```sh
zelta backup rpool/data backup-user@storage.example.com:tank/Backups/data
```

Run the same command again later to update incrementally. For non-root operation, delegate ZFS permissions with `zfs allow`; see the SSH and ZFS delegation guides for complete examples.

Run `zelta --help` for the command list, `zelta COMMAND --help` for a command's options, and `zelta help [TOPIC]` for the manual pages (embedded in the binary, so they work without installation).

---

## Core Tools

All Zelta commands operate recursively on backup sets and work locally or remotely via SSH.

### `zelta backup`
Robust backup with safe defaults. Creates consistent, read-only backups with intelligent incremental detection and optional pre-backup snapshots to ensure backups are current.

### `zelta match`
Compares two backup sets and reports matching snapshots or discrepancies. Essential for validating backups, planning rollbacks, and auditing.

### `zelta policy`
Runs multiple backup jobs concurrently from a single configuration file. Policies are hierarchical: global settings, site, host, and dataset-level options cascade to specific jobs.

### `zelta clone`
Creates temporary read-write clones of a backup set for recovery testing, inspection, or development—without disturbing the original. There is never a reason to make your backup datasets writable.

### `zelta revert`
Carefully rewinds a dataset in place by renaming and cloning. Ideal for forensic analysis, testing, or recovering from mistakes without losing current state.

### `zelta rotate`
Performs a multi-way rename and clone operation to keep backups rolling even after source or target has diverged. Preserves all versions without destructive receives.

### `zelta prune`
Plans snapshot pruning without destroying data. Candidate selection is separate from destruction and can use time, count, grid, reclaim-size, name, policy, and guard controls.

### `zprune`
Destructive companion for `zelta prune`. Validates prune candidates, previews `zfs destroy -nvp`, groups transactions, and prompts before destroying snapshots.

### `zelta failover`
Locks an active source, performs a final backup, syncs local ZFS properties, and unlocks the promoted target.

### `zelta rebase`
Rebase a dataset onto an upgraded upstream while preserving local files and incremental backup continuity.

### `zelta lock` and `zelta unlock`
Apply ordered dataset-tree readonly, canmount, unmount, and remount workflows for promotion and maintenance.

### `zelta propsync`
Replays local ZFS properties from one dataset tree to another while preserving target-only local overrides.

### Additional Commands

- `zelta snapshot`: Creates recursive snapshots on a local or remote endpoint.

Compatibility aliases such as `zelta sync`, `zpull`, `zmatch`, and `zp` are supported for existing operators. New documentation uses explicit `zelta backup` commands.

### Large Trees

Each `zfs receive` waits for ZFS to commit, so trees with many datasets spend most of a run waiting rather than sending. `--parallel N` syncs up to N datasets at once (new datasets still wait for their parents):

```sh
zelta backup --parallel 8 tank/data backup:tank/Backups/data
```

On a 221-dataset tree replicated over ssh, a daily incremental took 142 s serially and 29 s with `--parallel 8`. Commands to a remote host already share one ssh connection (see `REMOTE_MULTIPLEX` in `zelta help options`).

### Shell Completion

```sh
# bash
source <(zelta completion bash)
# zsh
zelta completion zsh > "${fpath[1]}/_zelta"
# fish
zelta completion fish | source
```

Completion covers commands, flags and their values, local and remote datasets and snapshots (`host:pool/da<TAB>`), ssh hosts, `zelta match -o` columns, policy sites and hosts, and help topics.

---

## Safety by Design

Zelta prioritizes data integrity above all else. Safety is built into every design decision.

### Safe Defaults
- Backups are created as read-only by default
- Child dataset mountpoints are reset to prevent dangerous overlapping mounts
- Snapshots are created before backup when needed to ensure up-to-date backups

### No Forced Overwrites
Zelta never suggests or requires destructive actions. The `zelta rotate` feature preserves divergent datasets by cloning before receiving new history.

### Remote and Recursive
All operations work remotely and recursively by default. Zelta backs up as much as possible and reports clearly about any discrepancies.

### Environment Agnostic
Backup decisions are based on metadata and available features, not naming conventions. This makes Zelta an effective recovery tool for complex, mixed environments.

### Minimal Attack Surface
Zelta can run entirely from a bastion host using SSH keys or agent forwarding. The Zelta team at Bell Tower runs its core backup loop from a locked-down system with configurations ensuring that no backup user has access to any unencrypted dataset throughout the entire workflow.

---

## Using Zelta from Go

The [`zelta`](zelta) package runs the same engine in-process, with typed options and results instead of flags and text output:

```go
import "zelta.space/zelta"

c, err := zelta.New(zelta.WithLogger(slog.Default()))
if err != nil {
	return err
}
res, err := c.Backup(ctx, "tank/home", "backup@vault:pool/backups/home", &zelta.BackupOptions{
	Snapshot: zelta.SnapshotPolicy{Mode: zelta.SnapshotIfNeeded, MinInterval: time.Hour},
	Parallel: 4,
})
for _, d := range res.Datasets {
	fmt.Println(d.Target, d.UpToDate, d.Notice)
}
```

It covers match, backup (and replicate), snapshot, prune planning and guarded destruction, clone, revert, rotate, rebase, lock, unlock, failover, propsync, and policy runs. A client uses only built-in defaults and its own settings unless created with `zelta.WithEnvironment()`, which reads `ZELTA_*` variables and `zelta.env` the way the command does. Failures are `*zelta.Error` values carrying the command's exit status and logged messages; nothing is printed. Run `go doc zelta.space/zelta` for the full API.

---

## Development

```sh
make            # build bin/zelta and bin/zprune
make test       # unit tests (race detector)
make cover      # coverage report
make lint       # gofmt, go vet, staticcheck, gopls
sudo make integration   # end-to-end tests on temporary file-backed pools
sudo make shellspec     # the Zelta ShellSpec compatibility suite
```

Integration and ShellSpec tests create their own file-backed pools and never touch existing ones. Optional switches:

- `ZELTA_AWK_ROOT=<zelta checkout>` compares this implementation's output with the Awk implementation, command by command.
- `ZELTA_TEST_SSHD=1` starts a temporary loopback sshd to test remote replication and connection reuse with real OpenSSH.
- `ZELTA_TEST_BINARY=<path>` (integration) and `ZELTA_BINARY=<path>` (`test/run_shellspec.sh`) test a prebuilt binary instead of building one.

The golden-pool prune scenarios need the `test-fixtures/v1` images from the [Zelta releases](https://github.com/bell-tower/zelta/releases/tag/test-fixtures/v1), unpacked into `test/spec/helpers/golden/0200_prune/`; then run `sudo ./test/run_shellspec.sh --tag install,prune-scenario,cleanup:install`.

See [doc/ARCHITECTURE.md](doc/ARCHITECTURE.md) for the package layout and the conventions every command follows.

---

## Community & Support

Zelta is open source under the BSD 2-Clause License and will always remain permissively licensed. Contributions welcome.

### Contact

We welcome questions, bug reports, and feature requests at [GitHub Issues](https://github.com/bell-tower/zelta-go/issues).

For other inquiries including business questions, you can reach the Zelta team at Bell Tower via our [contact form](https://belltower.it/contact/).

### Conference Talks

**BSDCan 2024: Zelta: A Safe and Powerful Approach to ZFS Replication** 
By Daniel J. Bell 
[Watch on YouTube](https://www.youtube.com/watch?v=_nmgQTs8wgE)

**OpenZFS Summit 2025: Responsible Replication with Zelta** 
[Watch on YouTube](https://www.youtube.com/watch?v=G3weooQqcXw)

### Bell Tower Services

For commercial support, custom feature development, and consulting on secure, high-efficiency infrastructure, contact us at [Bell Tower](https://belltower.it/). We provide consulting services for advanced policy management, cost control, compliance, and business continuity.
