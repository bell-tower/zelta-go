# Changelog

All notable changes to zelta-go will be documented in this file. Changes to
Zelta itself are tracked in the [Awk changelog](https://github.com/bell-tower/zelta/blob/main/CHANGELOG.md);
zelta-go follows its `main` branch and is planned to release as Zelta 1.3,
replacing the Awk implementation.

## [Unreleased] - 1.3.0-dev

### Changed
- **Rewrite**: zelta-go is now a drop-in replacement for Zelta (Awk): same commands, flags, `ZELTA_*` variables, `zelta.env`, `zelta.conf`, output, and exit codes. Intentional differences are listed in [doc/COMPATIBILITY.md](doc/COMPATIBILITY.md).
- **API (breaking)**: The old public packages (`backup`, `cmdbuild`, `data`, `endpoint`, `job`, `lineage`, `match`, `prune`, `rotate`, `sdk`, `zconf`, `zfs`) are removed. Use `github.com/bell-tower/zelta-go/zelta`.
- **License**: zelta-go is now released under the BSD 2-Clause License, like Zelta.

### Added
- **Backup**: `--parallel N` replicates up to N datasets at once; one multiplexed ssh connection per host.
- **CLI**: Per-command `--help`, embedded manual pages for `zelta help`, and shell completion for flags, datasets, snapshots, ssh hosts, and policy sites.
- **Backup**: Ctrl+T (SIGINFO, BSD and macOS) or SIGUSR1 (any system) prints bytes sent for every stream in flight, local or remote. `ZELTA_SEND_PROGRESS` (`auto`, `1`, `0`) controls the `zfs send -v` progress reports behind it.

### Fixed
- **Upstream**: Includes Zelta fixes #83 and #84.
- **Snapshot**: `zelta snapshot` fails when the snapshot cannot be created.
- **Failover**: `zelta unlock` and `zelta failover` succeed on OpenZFS 2.2, which lacks `zfs mount -R`.
- **Prune**: `--prune-size` selects exactly what `zfs destroy -nvp` frees, and any positive size selects at least one snapshot. It no longer destroys far more than asked when older snapshots are kept (clone origins, grid, `--prune-name`) or on clones.

### Known Issues
- **Parallel order**: With `--parallel`, datasets whose targets exist take free slots in arbitrary order, not tree order, so a small top dataset can wait behind long-running children.
- **Interrupted sync message**: Interrupting a sync (SIGINT) reports `sync command failed before receiving stream` even when data was received and a resume token was saved; the next run resumes normally.
