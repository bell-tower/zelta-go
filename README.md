# zelta-go

The Go implementation of [Zelta](https://zelta.space): safe, recursive ZFS
replication, recovery, and retention for local and remote dataset trees.
Remote hosts need only standard ZFS tools and SSH access.

zelta-go is a drop-in replacement for the original Awk implementation. It
accepts the same commands, flags, `ZELTA_*` environment variables,
`zelta.env`, and `zelta.conf`, prints the same output, and returns the same
exit codes. The few intentional differences are listed in
[doc/COMPATIBILITY.md](doc/COMPATIBILITY.md).

## Install

Requires Go 1.26 or newer to build.

```sh
git clone https://github.com/bell-tower/zelta-go.git
cd zelta-go
sudo ./install.sh          # or: make && sudo make install PREFIX=/usr/local
```

`install.sh` honors the same `ZELTA_BIN`, `ZELTA_SHARE`, `ZELTA_ETC`, and
`ZELTA_DOC` variables as the original installer and installs into your home
directory when run as a regular user. It never overwrites an existing
`zelta.env` or `zelta.conf`.

## Usage

```sh
zelta match   tank/data backup:tank/Backups/data   # compare trees
zelta backup  tank/data backup:tank/Backups/data   # replicate
zelta rotate  tank/data backup:tank/Backups/data   # recover a diverged replica
zelta revert  tank/data@yesterday                  # rewind without destroying
zelta prune   --match-endpoint=backup:tank/Backups/data tank/data
zprune        --match-endpoint=backup:tank/Backups/data tank/data
zelta policy                                       # run zelta.conf jobs
```

Run `zelta --help` for the command list, `zelta COMMAND --help` for a
command's options, and `zelta help [TOPIC]` for the manual pages (embedded in
the binary, so they work without installation).

### Shell completion

```sh
# bash
source <(zelta completion bash)
# zsh
zelta completion zsh > "${fpath[1]}/_zelta"
# fish
zelta completion fish | source
```

Completion covers commands, flags and their values, local and remote
datasets and snapshots (`host:pool/da<TAB>`), ssh hosts, `zelta match -o`
columns, policy sites and hosts, and help topics.

### Large trees

Each `zfs receive` waits for ZFS to commit, so trees with many datasets
spend most of a run waiting rather than sending. `--parallel N` syncs up to
N datasets at once (new datasets still wait for their parents):

```sh
zelta backup --parallel 8 tank/data backup:tank/Backups/data
```

On a 221-dataset tree replicated over ssh, a daily incremental took 142 s
serially and 29 s with `--parallel 8`. Commands to a remote host already
share one ssh connection (see `REMOTE_MULTIPLEX` in `zelta help options`).

## Using Zelta from Go

The [`zelta`](zelta) package runs the same engine in-process, with typed
options and results instead of flags and text output:

```go
import "github.com/bell-tower/zelta-go/zelta"

c, err := zelta.New(zelta.WithLogger(slog.Default()))
if err != nil {
	return err
}
res, err := c.Backup(ctx, "tank/home", "backup@vault:pool/backups/home", &zelta.BackupOptions{
	Snapshot: zelta.SnapshotPolicy{Mode: zelta.SnapshotIfNeeded, MinInterval: time.Hour},
	Parallel: 4,
})
for _, d := range res.Datasets {
	fmt.Println(d.Target, d.UpToDate, d.Notice)
}
```

It covers match, backup (and replicate), snapshot, prune planning and
guarded destruction, clone, revert, rotate, rebase, lock, unlock, failover,
propsync, and policy runs. A client uses only built-in defaults and its own
settings unless created with `zelta.WithEnvironment()`, which reads
`ZELTA_*` variables and `zelta.env` the way the command does. Failures are
`*zelta.Error` values carrying the command's exit status and logged messages;
nothing is printed. Run `go doc github.com/bell-tower/zelta-go/zelta` for the
full API.

## Development

```sh
make            # build bin/zelta and bin/zprune
make test       # unit tests (race detector)
make cover      # coverage report
make lint       # gofmt, go vet, staticcheck, gopls
sudo make integration   # end-to-end tests on temporary file-backed pools
sudo make shellspec     # the Zelta ShellSpec compatibility suite
```

Integration and ShellSpec tests create their own file-backed pools and never
touch existing ones. Optional switches:

- `ZELTA_AWK_ROOT=<zelta checkout>` compares this implementation's output
  with the Awk implementation, command by command.
- `ZELTA_TEST_SSHD=1` starts a temporary loopback sshd to test remote
  replication and connection reuse with real OpenSSH.
- `ZELTA_TEST_BINARY=<path>` (integration) and `ZELTA_BINARY=<path>`
  (`test/run_shellspec.sh`) test a prebuilt binary instead of building one.

See [doc/ARCHITECTURE.md](doc/ARCHITECTURE.md) for the package layout and the
conventions every command follows.

## License

See [LICENSE](LICENSE).
