# Contracts — shared behavior truth

Open this for safety, terminology, env hierarchy, and multi-impl notes. Keep short; link goldens for pixel detail.

## Terminology

| Term | Meaning |
|------|---------|
| endpoint (ep) | `user@host:pool/dataset[@snap]` (IPv6 host in `[]`) |
| ds_suffix | Relative child path with leading `/` (root → `""` or `/` per impl — freeze in goldens) |
| dataset tree | Dataset + recursive children |
| backup | User-facing term for replication |
| three axes | Snapshot, Backup, Prune/Retention — independent |

## Option hierarchy (support; see `~/Code/zelta/doc/`)

1. Built-in defaults (`opt/defaults.go`)
2. `zelta.env` (`KEY=value`; `ZELTA_` prefix optional inside file) — **`conf` + main injection, done**
3. `zelta.conf` (policy only; YAML-like `KEY: value`) — *not implemented*
4. Process env `ZELTA_*` (empty export = unset, oracle `:=`)
5. CLI flags (highest) — **`opt.Parse` from `data/opts.tsv`, done**

Mechanics: file never overrides process env; CLI `incr/decr` seed from the
current value; legacy env aliases (KEY_ALIAS column) use a global first-set
latch and overwrite the primary key (oracle quirks, kept).

Startup paths (must work before/without full env file): `ZELTA_ETC`, `ZELTA_ENV`, `ZELTA_CONFIG`, `ZELTA_SHARE` (Go: optional override of embedded data), `ZELTA_DOC` (man pages live in main zelta tree).

Go binary embeds `data/*`; does not require `ZELTA_SHARE` at runtime.

## Safety

- Read-only backups by default  
- No destructive force in core verbs  
- Divergence → rename/preserve, never overwrite  
- Child mountpoints reset to avoid overlays  
- Fail safe  

## Architecture shift vs Awk

- **No recursive `zelta ipc-run`.** Match is a library call.  
- **Command templates from `data/cmds.tsv` → argv slices, not shell strings when possible.
- **Comparison pure; execution behind `zfs.Executor`.

## Intentional deviations (full log)

**Open `agents/10-deviations.md`** when translating Awk→Go, explaining a pixel diff, or changing shared behavior.

- That file is the **full log** (bullet-only, with enough context/examples to find the code path).
- Package docs keep short contracts + `## Defer` (work not done).
- Goldens under `testdata/golden/` remain pixel truth — do not duplicate tables into the log.
- Add a log entry when a deviation is **intentional** or a **known gap with a reason**; fix bugs in code, not in the log.

## Multi-impl lessons

Record porting pain in `10-deviations.md` (or the relevant `0N-*.md` contract note). Goldens are the shared oracle format for Awk checks and future Ruby.
