# zelta-go

**Private / experimental.** Go library and single binary for [Zelta](https://zelta.space) — a data-driven ZFS composer.

Not public until documented. No man pages here; see `~/Code/zelta/doc/` (or installed Zelta docs).

## Status

Current checkpoint: match, read-only prune, and backup planning/execution are
implemented with oracle-driven tests. `zprune` and policy configuration remain
deferred.

## Library Status

Curated public packages for ZFS remote-action SDK - stable CLI as consumer:

| Package | Purpose |
|---------|---------|
| `endpoint` | Parse/user@host:dataset[@snap] |
| `zfs` | Executor, Real (SSH), Fake, pipe support |
| `match` | Compare two dataset trees |
| `backup` | Plan and run ZFS send/recv |
| `prune` | Read-only retention analysis |
| `lineage` | Clone/revert planning |
| `rotate` | Rotate lifecycle for divergent targets |
| `report` | JSON output schema, col expansion |

Import path: `git.belltower.it/djbell/zelta-go/<package>`. Production:
`&zfs.Real{ZFS: "zfs", SSH: zfs.SSHConfig{...}}`; tests: `&zfs.Fake{}`.

See `agents/16-sdk.md` for the full plan and Sylve drop-in guide.

## Build

```sh
make build
./bin/zelta version
make test
```

## Agents

Read `AGENTS.md` first. Local persona: `AGENTS-Persona.md` (gitignored).
