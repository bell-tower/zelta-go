# Compatibility with Zelta 1.2 (Awk)

zelta-go aims to be a drop-in replacement for the Awk implementation of
Zelta 1.2. Command lines, `ZELTA_*` variables, `zelta.env`, `zelta.conf`,
human and machine-readable output, and exit codes are preserved. This file
lists the places where behavior intentionally differs, almost always because
the Awk behavior contradicted the documentation or was a latent bug.

## Fixes made in Zelta after 1.2.0

zelta-go tracks fixes on the Awk `main` branch:

- `--snapshot` stops with "error creating snapshot" when the snapshot cannot
  be created, instead of syncing an older snapshot
  ([zelta#83](https://github.com/bell-tower/zelta/pull/83)). This also stops
  `zelta failover`, whose final backup always takes a snapshot.
- A source endpoint's `@snapshot` ends the sync even when newer snapshots
  exist: the backup stops there (also with `--include`/`--exclude`), a target
  that already has it is up-to-date, and `zelta rotate` stops before renaming
  ([zelta#84](https://github.com/bell-tower/zelta/pull/84)).

## Performance

These changes make runs faster without changing their results.

- Planning reads only the six properties it uses instead of `zfs get all`
  (several times faster on large trees). Hosts older than OpenZFS 2.2, which
  reject `snapshots_changed`, are asked for `all` as before.
- With plain `ssh`, commands to a host share one connection
  (`REMOTE_MULTIPLEX`, on by default; `0` restores a connection per command).
  Displayed and dry-run commands are unchanged.
- `--parallel N` (`PARALLEL`) syncs up to N datasets at once. The default of 1
  keeps Zelta 1.2's order and output exactly. With N above 1, per-dataset
  messages may interleave and the summary line reports elapsed time instead
  of the summed stream times (JSON `replicationTime` is still the sum).
- When a send option is dropped and retried (`--send-check`), failures of
  other datasets are no longer masked.
- Zelta keeps a cache directory (`CACHE`, `CACHE_DIR`, `CACHE_TIMEOUT`,
  `--no-cache`). It only holds hints; deleting it never changes a result.
  `zelta replay PATTERN` repeats the last backup between endpoints matching
  PATTERN, read from the cache.
- `zelta policy` also reads a JSON policy store (`policy.json`, or any
  `--config` ending in `.json`), which becomes the default when it exists.
  `--save PATTERN` turns a remembered backup into a job, `--rm PATTERN`
  removes one, and `--import FILE` converts a `zelta.conf`.
- `--remember` (`REMEMBER`) skips reading the target on the next sync and
  sends from the snapshots last synced, falling back to a full comparison
  when the receive refuses the stream.

## Command line

- Options may appear after operands (`zelta backup src tgt -n`). Zelta 1.2
  treated everything after the first operand as an operand.
- `--help`/`-h` print rich help to standard output and exit 0. Usage errors
  still print to standard error and exit 1. `zelta help [topic]` still opens
  the manual page.
- `-F` is passed to `zfs recv` (it was filed under the `zfs send` overrides).
- `--no-written`/`--nowritten` disable size listing in `zelta match`
  (they set an unused key).
- `--retry` and `--host-prefix` accept an optional value (`--retry=3`).
- `--dry-run` and `--snapshot-always` are accepted as documented aliases.
- Legacy command names work fully: `zmatch`, `zpull`, `zpush`, `zsync`,
  `zeport`, `zp`, and `zprune` (also `zelta zprune`).
- `zelta completion bash|zsh|fish|powershell` generates shell completions,
  including dataset, snapshot, and policy-object completion.
- `zelta ipc-env` quotes values as FreeBSD sh prints `set` output, which is
  what Zelta 1.2 printed there (on Linux its output followed dash and quoted
  every value). Values with quotes or control characters use plain single
  quotes rather than `$'...'`.

## Options and configuration

- `off` is false, like `0`, `no`, and `false`.
- `RECV_PIPE` (`--recv-pipe`) is honored; `RECEIVE_PREFIX` and `RECV_PREFIX`
  are accepted as legacy names for it.
- Every legacy variable is mapped, not only the first one found.
- Invalid `zelta.env` lines are skipped with a warning instead of aborting
  the evaluation of the whole file. `export KEY=value` lines are accepted.
- When `/usr/local/etc/zelta` does not exist but `/etc/zelta` does, the latter
  is used as the configuration directory.
- `ZELTA_HOSTNAME` may be set to override the local hostname used in logs.
- `LOG_LEVEL` in a policy file is honored.
- `SEND_NEW` is accepted but, as in Zelta 1.2, not applied to full sends.

## Behavior

### Replication

- **Next snapshot after a mid-run snapshot.** Zelta 1.2 had a missing-brace
  bug that reset every dataset's "next snapshot" to a snapshot taken during
  the run. Full intermediate sends of new datasets then skipped their older
  history, dry runs planned differently from real runs, and `zelta rotate`
  targeted the new snapshot instead of the next available one (as
  documented). This is fixed.
- Dry runs no longer create missing target parents; they print the
  `zfs create` command when the parent is actually missing.
- `zelta rebase` runs the rotate workflow in-process, so a dry run shows the
  real zfs commands, and its notices are printed normally.
- Four-endpoint `zelta clone` snapshots the new clone explicitly before
  backing it up, which also makes cloning from an older snapshot work.
- `zelta snapshot` exits 1 when the snapshot fails (it exited 0).
- A failed `zfs rename` stops the run even when zfs printed nothing.
- Default snapshot names have one-second resolution, so commands started
  within the same second (a backup followed by a failover, say) could
  collide, which since zelta#83 aborts `--snapshot` and failover. When the
  default name already exists, zelta-go waits for the next second and uses
  a fresh name. Names you choose (`--snap-name`, `SNAP_NAME`, `ds@name`)
  are never changed and still fail if they exist.
- Each `zelta policy` job gets its own default snapshot name from its start
  time, as each separate `zelta backup` process did in Zelta 1.2.
- The rotate summary reports the correct number of up-to-date datasets.
- `DEPTH=0` means unlimited, as `zelta.env` documents, for every command (it
  was rejected). Negative and non-numeric depths are still rejected.
- `SNAP_MODE` and `SYNC_DIRECTION` values are case-insensitive, and a
  configured `CLONE_DEFAULT` is honored (the default command is unchanged).
- The resume hint names the receiving dataset (`zfs receive -A <target>`).
- Displayed `zfs snapshot` commands quote the snapshot name like every other
  name, and "0 snapshots bookmarked" prints the zero.

### Other commands

- `zelta unlock` and `zelta failover` succeed on a zfs without
  `zfs mount -R` (OpenZFS 2.2, as on Ubuntu 24.04) by mounting each dataset.
  Zelta 1.2 does the same mounts but reports "failed recursive mount during
  unlock" and exits 1. Zelta (Awk) after 1.2 no longer runs `zfs mount -R`
  and only mounts each dataset top down (sorted by name), as zelta-go does
  after trying `-R`; dry runs differ by that one `zfs mount -R` line.
- `zelta match -p` prints exact byte counts in human-readable tables, as
  documented.
- `--include`/`--exclude` with intermediate backups skip filtered snapshots
  when replicating stepwise (Zelta 1.2 sent them anyway).
- An absolute `--include` of the top dataset also selects its snapshots.
- `--snap-time` treats bare numbers of at least 1000000000 as Unix epoch
  times and smaller bare numbers as seconds; all unit forms (including
  `1 year` and `2mo`) are relative to the current time.
- `zelta snapshot` honors `--snap-time` and `--snap-size` as documented.
- `zelta policy` with `JOBS` greater than 1 runs that many sites concurrently
  whenever more than one site is configured, as documented, and exits non-zero
  if any job still fails after retries.
- Policy option values are the rest of the line; Zelta 1.2 truncated values
  containing `": "` or `"- "`.
- A policy dataset with neither an explicit target nor a `BACKUP_ROOT` is
  skipped with the warning "no target defined for ..." (Zelta 1.2 synced it
  to a bogus `/leaf` target).
- `ADD_DATASET_PREFIX: -1` includes every parent label, as documented.
- `RETRY` set at site or host scope is honored, not only the global value.
- `BACKUP_COMMAND` replaces the command used for policy jobs, as documented.
- `zelta policy --dryrun --verbose` lists each job's `ZELTA_*` overrides
  sorted by name (Zelta 1.2 printed them in hash order).
- Policy import loops are detected even when paths are spelled differently
  (`a` and `./a`).
- As in Zelta 1.2, `--rotate` and `-R` given to `zelta policy` do not change
  the verb of its jobs, which always run as `zelta backup`.
- `zelta report` quotes dataset names containing spaces correctly.
- `--prune-num` must be a non-negative integer (Zelta 1.2 silently kept
  everything for non-numeric values).
- `zelta prune` only offers snapshots of datasets present in the source
  listing; an absolute `--exclude` can no longer leak candidates through the
  match endpoint.
- `--prune-guard` also accepts `no`, `false`, and `off` for `none`, in any case.
- zprune's per-dataset summary keeps the full names of datasets containing
  spaces.
- `SYSTIME` drives the clock of every command (backup timestamps, report
  thresholds, prune windows), not only JSON timestamps.

Some Zelta 1.2 behaviors that may look surprising are kept on purpose:
range candidates print newest range first (individual candidates print
oldest first), `zelta match` exits 0 after "datasets inaccessible or do not
exist", `--depth 0` is rejected, and zero counts are empty fields in `-H`
output.
- JSON output escapes quotes, backslashes, and control characters, and writes
  fields in a stable order.
- Messages containing backslashes are printed verbatim (the shell logger
  dropped them).
