# zelta-go

**Private / experimental.** Go library and single binary for [Zelta](https://zelta.space) — a data-driven ZFS composer.

Not public until documented. No man pages here; see `~/Code/zelta/doc/` (or installed Zelta docs).

## Status

Current checkpoint: match, read-only prune, and backup planning/execution are
implemented with oracle-driven tests. `zprune` and policy configuration remain
deferred.

## Build

```sh
make build
./bin/zelta version
make test
```

## Agents

Read `AGENTS.md` first. Local persona: `AGENTS-Persona.md` (gitignored).
