# zelta-go

[![Go](https://github.com/bell-tower/zelta-go/actions/workflows/go.yml/badge.svg)](https://github.com/bell-tower/zelta-go/actions/workflows/go.yml)
[![Shell](https://github.com/bell-tower/zelta-go/actions/workflows/shell.yml/badge.svg)](https://github.com/bell-tower/zelta-go/actions/workflows/shell.yml)
[![ShellSpec](https://github.com/bell-tower/zelta-go/actions/workflows/shellspec.yml/badge.svg)](https://github.com/bell-tower/zelta-go/actions/workflows/shellspec.yml)

Go library and CLI for [Zelta](https://zelta.space) — a data-driven ZFS composer.

**Docs:** [zelta.space](https://zelta.space)

## Status

Match, backup, read-only prune, clone/revert, rotate, policy dry-run/exec, and
`zprune` are implemented with deterministic unit and golden tests. Several
paths also have disposable real-ZFS evidence. Public SDK packages are
importable for in-process use (e.g. orchestration platforms).

## Install / build

```sh
git clone https://github.com/bell-tower/zelta-go.git
cd zelta-go
make build
./bin/zelta version
```

```sh
make                      # build → bin/zelta, bin/zprune
make test                 # go test ./...
make clean                # remove bin/zelta, bin/zprune
make shelltest            # binary smoke (no ZFS)
make shellspec            # ShellSpec install + no-op + cleanup
make shellspec-standard   # full Richard suite (needs sudo ZFS)
```

## Library

Import path: `github.com/bell-tower/zelta-go/<package>`

| Package | Purpose |
|---------|---------|
| `endpoint` | Parse `user@host:dataset[@snap]` |
| `zfs` | Executor, Real (SSH), Fake, pipe support |
| `match` | Compare two dataset trees |
| `backup` | Plan and run ZFS send/recv |
| `prune` | Read-only retention analysis |
| `lineage` | Clone/revert planning |
| `rotate` | Rotate lifecycle for divergent targets |
| `report` | JSON output schema, column expansion |

```go
// Structured SSH (library consumers):
&zfs.Real{SSH: zfs.SSHConfig{IdentityFile: key, Port: 22}}

// Raw remote prefixes (mbuffer, socat, custom ssh, …):
&zfs.Real{Remote: zfs.CommandRemote{Command: "ssh -p 2202"}}

// Tests:
&zfs.Fake{}
```

Runnable samples: `go test ./backup -run Example`.

## CLI

```sh
./bin/zelta match …
./bin/zelta backup …
./bin/zelta policy …
./bin/zprune …
```

`./bin/zelta --help` and `./bin/zelta help <topic>` use embedded man pages.

## License

See [LICENSE](LICENSE).
