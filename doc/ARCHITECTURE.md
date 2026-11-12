# zelta-go architecture

zelta-go is the Go successor to the Awk implementation of Zelta. It is a
drop-in replacement: the same verbs, flags, `ZELTA_*` variables, `zelta.env`,
`zelta.conf`, output formats, and exit codes. The Awk implementation (its
`main` branch) and its manual pages are the specification; deliberate
deviations are recorded in [COMPATIBILITY.md](COMPATIBILITY.md).

## Layout

```
cmd/zelta/            main: argv[0] dispatch (zelta, zprune, zmatch, zpull, ...)
zelta/                public Go API: Client, typed options and results
internal/cli/         cobra command tree, flags generated from the option registry,
                      help, usage errors, completions, ipc-* verbs
internal/config/      option registry (port of zelta-opts.tsv), layered resolution:
                      defaults < zelta.env < policy < environment < command line
internal/app/         Runtime (options, logger, executor, streams, clock), exit
                      errors, Build/Finish (logger + JSON document lifecycle)
internal/command/     Cmd/Arg/Remote/Pipeline model; renders dry-run shell text
                      and the argv used for execution (local or over ssh)
internal/execx/       OS executor (direct exec, Go-level pipelines) and Fake
internal/endpoint/    [user@]host:pool/ds@snap parsing
internal/logging/     leveled output matching `zelta ipc-log` routing
internal/jsonout/     JSON summary documents
internal/units/       sizes, durations, Awk-compatible number formatting
internal/zfs/         zfs command builder bound to an endpoint; feedback classifier
internal/match/       tree comparison engine (Compare), `zelta match` output
internal/prune/       `zelta prune` planner and `zprune` destroyer
internal/backup/      backup, sync, replicate, rotate, clone, revert, rebase, snapshot
internal/failover/    lock, unlock, propsync, failover
internal/policy/      zelta.conf parser and job runner
internal/report/      zeport / zelta report
doc/                  manual pages (roff and Markdown) embedded for `zelta help`;
                      these design notes and COMPATIBILITY.md
```

## Public API

The `zelta` package is the public API. (`doc` is importable as well, only
because `go:embed` cannot reach files outside its directory.) A `Client`
resolves options per operation with the same loader as the command line
(built-in defaults, then `ZELTA_*` and `zelta.env` only with
`WithEnvironment`, then client and operation settings applied above every
other source), builds an `app.Runtime` whose logger forwards to `slog`
through `Runtime.LogHandler` (nested runtimes such as failover's final
backup inherit it), and calls the same engines the CLI does. Engines that
the CLI only needs to print expose a result-returning variant for it:
`backup.Execute`, `backup.TakeSnapshot`, `prune.Build` with `prune.Destroy`,
and `match.Compare`. Typed fields map to option keys in `zelta/options.go`;
anything else is reachable through `Settings`. Typed snapshot names are
validated so they never reach the shell expansion `SNAP_NAME` performs.

## Engine contract

Every command is implemented as

```go
func Run(ctx context.Context, rt *app.Runtime, args []string) error
```

where `args` are the positional operands left after flag parsing.

- **Options** come from `rt.Opts` (keys without the `ZELTA_` prefix):
  `Get`, `Bool` (Zelta truthiness: "", 0, no, false, off are false), `Int`.
  The effective verb is `rt.Opts.Get("VERB")` (flags such as `--rotate` and
  `-R` change it).
- **Never call `os.Exit`.** Return `app.Fail(msg)` / `app.Exit(code, msg)` for
  fatal errors (the message is logged once as `error: msg` by `app.Finish`),
  `app.Usage(msg)` for usage errors (the CLI prints the message and usage and
  exits 1), or `&app.ExitError{Code: n}` when messages were already logged.
- **Logging** goes through `rt.Log` (`Errorf`, `Warnf`, `Noticef`, `Infof`,
  `Debugf`). Message texts match the Awk implementation exactly; users and
  tests grep them. Notices are the normal human output (stdout on a terminal).
- **Commands** are built with `zfs.Command(rt, ep, kind, args...)` and
  `command.*` helpers. Use `command.Name` for dataset/snapshot names (they are
  always quoted in displayed commands), `command.Fragment` for user option
  strings like `SEND_DEFAULT`, and remote kinds `zfs.RemoteDefault`,
  `zfs.RemoteSend`, `zfs.RemoteRecv`.
- **Execution** uses `rt.Run(ctx, cmd)` (logs the command at debug level as
  `` `cmd` ``) or `rt.Exec.Pipe` for send/receive pipelines. In dry-run mode,
  print `+ <cmd.String()>` at notice level instead of executing.
- **Output lines** from commands are tagged stdout/stderr; strip carriage
  returns with `app.SanitizeLine`. Unexpected lines go through `zfs.Feedback`,
  which logs ssh/connection failures as errors and other noise as info.
- **JSON** summary fields are set with `rt.Report(key, value)`; arrays via
  `rt.JSON` when non-nil. `app.Finish` writes the document.
- **Time** comes from `rt.Now()`; never call `time.Now()` in engines.

## Testing

- Unit tests use `app.NewTest(verb, opts)`, which wires a `*execx.Fake`
  (scripted by rendered command string), buffered log streams (`Out`, `Err`),
  raw streams (`RawOut`, `RawErr`), and a fixed clock (`app.TestNow`).
- Assert on exact log output and on `tr.Fake.Calls` (the rendered commands,
  which double as dry-run golden output).
- Integration tests (`-tags integration`) run against real file-backed pools;
  never against existing pools.
- The ShellSpec suite from the Awk implementation runs against the Go binary
  as a compatibility check (`make shellspec`).
