# CLI

**Packages:** `cmd/zelta`, later `cmd/zprune`

## Rules

- Thin main: inject zelta.env → parse verb → library
- **All verb flags go through `opt.Parse(verb, argv)`** (opts.tsv-driven) — no
  stdlib `flag`, no per-verb hand-rolled options. New flags = new opts.tsv rows.
- `version` always works
- Unknown verbs: clear error; point at main zelta docs
- Help: no man pages here; `ZELTA_DOC` or `~/Code/zelta/doc` handoff later

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
- `match [-Hp] [-d depth] [-o field[,...]] [-X pat] [--include pat] [--written|--no-written] [--time] SOURCE TARGET`
- `backup [-n|--dryrun] [-I|-i] [--snapshot|--no-snapshot|--snapshot-skip] [--push|--pull|--no-pull] [-d depth] [-X pat] [--include pat] [send/recv passthrough: -L -c -e --raw …] SOURCE TARGET`
- `prune [--prune-num N] [--prune-time T] [--prune-grid G] [--prune-size N] [--match-endpoint EP] [--prune-guard latest|unsynced|none] [--no-ranges] [--visual] [-v] ENDPOINT`
- `policy [-n|--dryrun] [-H] [-C|--config FILE] [site|host|dataset]...` — dry-run lists resolved SOURCE/TARGET pairs; execution not implemented
- Option introspection: oracle `zelta ipc-env VERB [OPTS] OPERANDS` explodes
  parsed env/opts (use when unsure how flags map)
- others: explicit not-implemented
