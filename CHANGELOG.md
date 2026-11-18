# Changelog

All notable changes to zelta-go will be documented in this file. Changes to
Zelta itself are tracked in the [Awk changelog](https://github.com/bell-tower/zelta/blob/main/CHANGELOG.md);
zelta-go follows its `main` branch and is planned to release as Zelta 1.3,
replacing the Awk implementation.

## [Unreleased] - 1.3.0-dev

### Changed
- **Rewrite**: zelta-go is now a drop-in replacement for Zelta (Awk): same commands, flags, `ZELTA_*` variables, `zelta.env`, `zelta.conf`, output, and exit codes. Intentional differences are listed in [doc/COMPATIBILITY.md](doc/COMPATIBILITY.md).
- **API (breaking)**: The old public packages (`backup`, `cmdbuild`, `data`, `endpoint`, `job`, `lineage`, `match`, `prune`, `rotate`, `sdk`, `zconf`, `zfs`) are removed. Use `zelta.space/zelta`.
- **zprune**: `zprune` is a separate binary again, as in Zelta 1.2, instead of a link to `zelta`. `zelta zprune` is gone, so `zelta` never destroys snapshots. `go install zelta.space/cmd/...@latest` installs both. Reinstall to replace an old `zprune` link, which now only prints a reinstall notice.
- **Module path (breaking)**: The module is now `zelta.space` (was `github.com/bell-tower/zelta-go`). Import `zelta.space/zelta`; install with `go install zelta.space/cmd/zelta@latest`.
- **License**: zelta-go is now released under the BSD 2-Clause License, like Zelta.

### Added
- **Backup**: `--parallel N` replicates up to N datasets at once; one multiplexed ssh connection per host.
- **CLI**: Per-command `--help`, embedded manual pages for `zelta help`, and shell completion for flags, datasets, snapshots, ssh hosts, and policy sites.
- **Backup**: Ctrl+T (SIGINFO, BSD and macOS) or SIGUSR1 (any system) prints bytes sent for every stream in flight, local or remote. `ZELTA_SEND_PROGRESS` (`auto`, `1`, `0`) controls the `zfs send -v` progress reports behind it.
- **Prune**: `--prune-grid-type=union` (and `PruneOptions.GridType`) measures every grid term from the anchor and keeps a snapshot that any term keeps. The default, `band` (alias `zrepl`), keeps the consecutive bands of Zelta 1.2; union is planned as the default for Zelta 2.
- **API**: `PruneOptions.Approved` makes `Prune` destroy only snapshots from a reviewed plan that are still candidates; the rest are reported in `PrunePlan.Skipped`. Snapshots are matched by GUID, so planner UIs can re-run `PlanPrune` freely and then destroy exactly what they showed.

### Fixed
- **Upstream**: Includes Zelta fixes #83 and #84.
- **Prune**: `--prune-grid` terms are ordered by interval, so a grid written longest first (`1 year, 1 week, 1 day`) no longer keeps only the longest interval.
- **Snapshot**: `zelta snapshot` fails when the snapshot cannot be created.
- **Failover**: `zelta unlock` and `zelta failover` succeed on OpenZFS 2.2, which lacks `zfs mount -R`.
- **Prune**: `--prune-size` selects exactly what `zfs destroy -nvp` frees, and any positive size selects at least one snapshot. It no longer destroys far more than asked when older snapshots are kept (clone origins, grid, `--prune-name`) or on clones.
- **Prune**: `--prune-size` reads `written@` only where the listing's `used` and `written` can't decide the selection, a few snapshots at a time, instead of every snapshot after a kept one. Same selection; on a 187-snapshot clone over ssh, planning dropped from about 6s to under 2s.

### Known Issues
- **Parallel order**: With `--parallel`, datasets whose targets exist take free slots in arbitrary order, not tree order, so a small top dataset can wait behind long-running children.
- **Interrupted sync message**: Interrupting a sync (SIGINT) reports `sync command failed before receiving stream` even when data was received and a resume token was saved; the next run resumes normally.
