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
- **Cache**: A per-user cache directory (`CACHE`, `CACHE_DIR`, `CACHE_TIMEOUT`, `--no-cache`) holds hints only; deleting it never changes a result.
- **Replay**: `zelta replay PATTERN` repeats the last backup between endpoints matching PATTERN, with a chooser when several match.
- **Policy**: A JSON policy store (`policy.json`) with `zelta policy --save PATTERN`, `--rm PATTERN`, and `--import FILE` (from `zelta.conf`).
- **Backup**: `--remember` skips reading the target on the next sync and sends from the remembered snapshots; a refused receive falls back to a full comparison.

### Fixed
- **Upstream**: Includes Zelta fixes #83 and #84.
- **Snapshot**: `zelta snapshot` fails when the snapshot cannot be created.
- **Failover**: `zelta unlock` and `zelta failover` succeed on OpenZFS 2.2, which lacks `zfs mount -R`.
