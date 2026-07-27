# Tests

## Unit / golden (Go)

```sh
make test
```

## POSIX smoke (no ZFS)

```sh
make shelltest
```

## ShellSpec (Richard’s suite, real ZFS)

Ported from [zelta](https://github.com/bell-tower/zelta) AWK lineage. Same
tags, helpers, and scenario layout; install check adapted for the Go binary
(no `share/zelta/*.awk` required).

### Prerequisites

- [ShellSpec](https://shellspec.info/) (`shellspec` on `PATH`)
- `sudo` ZFS permissions (see AWK `test/README.md` sudoers notes)
- Optional: `jq` for JSON assertions

### Scratch paths

Sandboxes live under **repo-local `./tmp/`** (not `/tmp/`). Override base with
`SANDBOX_ZELTA_TMP_BASE` if needed.

### Quick start (local)

```sh
# no-op only (install + CLI smoke + cleanup) — no pools required
export SANDBOX_ZELTA_TMP_SUFFIX="$LOGNAME"
shellspec --tag install,cleanup

# full standard scenario (file-backed pools under ./tmp/)
export SANDBOX_ZELTA_SRC_POOL=apool
export SANDBOX_ZELTA_TGT_POOL=bpool
export SANDBOX_ZELTA_SRC_DS=apool/treetop
export SANDBOX_ZELTA_TGT_DS=bpool/backups
./test/run_tests.sh standard
```

Or: `make shellspec` / `make shellspec-standard`.
