# zelta-go

**Private / experimental.** Go library and single binary for [Zelta](https://zelta.space) — a data-driven ZFS composer.

Not public until documented. No man pages here; see `~/Code/zelta/doc/` (or installed Zelta docs).

## Status

Current checkpoint: match, read-only prune, and backup planning/execution are
implemented with oracle-driven tests. `zprune` and policy configuration remain
deferred.

## Library Status

The current implementation is a command-oriented prototype. Useful exported
APIs exist in `internal/`, including `match.Compare`, `backup.Run`,
`prune.Run`, `zfs.Executor`/`Real`/`Fake`, command construction, endpoint
parsing, and option parsing. Because these packages are below Go's
`internal/` barrier, they are not importable by external modules yet.

Before publishing a library contract, choose whether to promote a curated API
to top-level packages or keep the module CLI-only. Do not move individual
packages opportunistically; the package boundary should follow the documented
contract in `agents/11-roadmap.md`.

## Build

```sh
make build
./bin/zelta version
make test
```

## Agents

Read `AGENTS.md` first. Local persona: `AGENTS-Persona.md` (gitignored).
