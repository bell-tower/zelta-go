# Backup (phase 2+)

**Package:** `internal/backup` — stub until match goldens are solid.

## Intent

In-process: `match.Compare` → send/recv plan from `cmds.tsv` → `Executor`. No `ipc-run match`.

## Defer

Send-check feature fallback, resume tokens, bookmark, rotate/clone composers — contract notes only until phase 2.
