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

1. Built-in defaults  
2. `zelta.env` (`KEY=value`; `ZELTA_` prefix optional inside file)  
3. `zelta.conf` (policy only; YAML-like `KEY: value`)  
4. Process env `ZELTA_*`  
5. CLI flags (highest)

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
- Command templates from `data/cmds.tsv` → argv slices, not shell strings when possible.  
- Comparison pure; execution behind `zfs.Executor`.

## Multi-impl lessons

Record only when something actually hurts porting. Goldens under `testdata/golden/` are the shared oracle format for Awk checks and future Ruby.
