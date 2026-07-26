# zelta-go

**Private / experimental.** Go library and single binary for [Zelta](https://zelta.space) — a data-driven ZFS composer.

Not public until documented. No man pages here; see `~/Code/zelta/doc/` (or installed Zelta docs).

## Status

Match, backup, read-only prune, clone/revert, rotate, policy dry-run/exec,
and `zprune` are implemented with deterministic tests; several paths have
disposable real-ZFS evidence. Public SDK packages are importable; Sylve
in-process integration is the next external step (`agents/16-sdk.md`).
Private Gitea only.

## Library Status

Curated public packages (ZFS remote-action SDK); CLI is a thin consumer:

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
