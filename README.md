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

Import path: `git.belltower.it/djbell/zelta-go/<package>`.

```go
// Structured SSH (library / Sylve):
&zfs.Real{SSH: zfs.SSHConfig{IdentityFile: key, Port: 22}}

// Raw remote prefixes (Awk REMOTE_*, mbuffer, socat, …):
&zfs.Real{Remote: zfs.CommandRemote{Command: "ssh -p 2202"}}

// Tests:
&zfs.Fake{}
```

See `agents/16-sdk.md`. Runnable samples: `go test ./backup -run Example`.

## Build

```sh
make build
./bin/zelta version
make test
```

## Agents

Read `AGENTS.md` first. Local persona: `AGENTS-Persona.md` (gitignored).
