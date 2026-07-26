# Data tables (`data/`)

Go **owns** these copies. Schema may clean up vs `~/Code/zelta/share/zelta/`. Note deviations here.

| File | Role |
|------|------|
| `opts.tsv` | Flag → env key → type (true/false/set/list/incr/…) |
| `cmds.tsv` | Action → remote role → zfs/zelta template |
| `cols.tsv` | match column names, synonyms, types |
| `json.tsv` | JSON field map for backup-related summary |

## Edit rules

- Prefer editing TSV over hardcoding flag/column lists in Go.
- Keep parsers dumb; put behavior in packages that consume loaded tables.
- After schema change: update loader tests + any golden that depends on columns.
- Embed via `//go:embed` from a single `data` package or cmd-level embed passed in.

## Location

Editable + embedded source of truth: repo-root `data/*.tsv` (`package data`, `//go:embed`).

## Loader home

- `internal/opt` — opts  
- `internal/cmdbuild` — cmds  
- `internal/report` — cols + json  

## cmds.tsv usage (Go)

| Layer | Uses templates |
|-------|----------------|
| `cmdbuild.Build` | argv only: `COMMAND` + `ARGS` + `VARS` |
| `cmdbuild.RemoteRole` | column 2 → `SEND` / `RECV` / `DEFAULT` / `""` |
| `cmdbuild.StdinNull` | `RECV` → stdin open; else `ssh -n` (oracle REMOTE_*) |
| `zfs.Real` | `LIST` / `SNAP` via cmdbuild; pipe sides use SEND/RECV roles |
| `backup` | `SEND` / `RECV` / `SNAP` via cmdbuild |

Still **not** shell-string builders (no recursive `ipc-run` in argv). Remote wrapping stays in `zfs` (`Remote` / `SSHConfig` / `CommandRemote`). CLI maps `ZELTA_REMOTE_*` via `cmd/zelta/remote.go`.
