# CLI

**Packages:** `cmd/zelta`, later `cmd/zprune`

## Rules

- Thin main: inject zelta.env → parse verb → library
- **All verb flags go through `opt.Parse(verb, argv)`** (opts.tsv-driven) — no
  stdlib `flag`, no per-verb hand-rolled options. New flags = new opts.tsv rows.
- `version` always works
- Unknown verbs: clear error; point at main zelta docs
- `zelta help [topic]` routes to man pages via `commandHelp()` (upstream `zelta_man` parity):
  - no topic → `man 8 zelta`
  - `options` → `man 7 zelta-options`
  - verb → `man 8 zelta-VERB`
  - Uses `ZELTA_DOC` when set/directory, otherwise system `man` path
- `--help`, `-h`, `-?` show usage() (quick reference, not man page)

## Resolution chain (oracle parity)

1. `cmd/zelta/main.go` `injectEnvFile`: `conf.EnvPath()` →
   `conf.LoadEnvFile` → `os.Setenv(ZELTA_K)` only when unset-or-empty
   (oracle `:=`; empty export counts as unset; rejected: ZELTA_AWK/ETC/ENV).
2. `opt.Parse` seeds: built-in defaults (`opt/defaults.go`) → process env
   `ZELTA_*` (non-empty, `no/false`→`0`) → legacy alias latch → applies CLI.
3. Verbs read `Parsed.Env` (Get/Bool/List) + `Operands`; `Parsed.Warnings`
   printed as `warning: …`; parse/depth errors print `error: …` (oracle stop()).

Paths (oracle): `ZELTA_ETC` = env → `~/.config/zelta` (if dir) →
`/usr/local/etc/zelta`; `ZELTA_ENV` = env → `$ZELTA_ETC/zelta.env`.

## Verbs

- `version`
- `help [topic]` — man page routing (topic → `man 8 zelta-VERB`; `options` → `man 7 zelta-options`)
- `match [-Hp] [-d depth] [-o field[,...]] [-X pat] [--include pat] [--written|--no-written] [--time] SOURCE TARGET`
- `backup [-n|--dryrun] [-I|-i] [--snapshot|--no-snapshot|--snapshot-skip] [--push|--pull|--no-pull] [-d depth] [-X pat] [--include pat] [send/recv passthrough: -L -c -e --raw …] SOURCE TARGET`
- `prune [--prune-num N] [--prune-time T] [--prune-grid G] [--prune-size N] [--match-endpoint EP] [--prune-guard latest|unsynced|none] [--no-ranges] [--visual] [-v] ENDPOINT`
- `policy [-n|--dryrun] [-H] [-C|--config FILE] [site|host|dataset]...` — list or run backup jobs from zelta.conf
- `clone, revert, rotate, lock, unlock, failover, propsync` — additional verbs listed in usage()
- Option introspection: oracle `zelta ipc-env VERB [OPTS] OPERANDS` explodes
  parsed env/opts (use when unsure how flags map)
- others: explicit not-implemented
