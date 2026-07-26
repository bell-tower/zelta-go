# SDK — Curated ZFS remote-action Go packages

## Goal

Make `git.belltower.it/djbell/zelta-go` importable as a **curated multi-package
SDK**. Sylve (and other integrators) call library verbs in-process;
`cmd/zelta` is a thin consumer. First cut also closes **Sylve-critical gaps**:
structured SSH config, typed results/errors, progress hooks.

## Principles

1. **Request structs > process env.** Core behavior driven by fields; no
   required `ZELTA_*` for library use.
2. **Inject `zfs.Executor`.** Production: `&zfs.Real{SSH: …}`; tests: `Fake`.
3. **Typed outcomes.** Prefer `Result` + errors over scraping human text.
4. **Stability before feature.** Export only what integrators need; internal
   helpers stay unexported inside packages.
5. **No Sylve types** in this repo (no GORM, no cluster models, no queues).

## Current state

| Aspect | Today |
|--------|-------|
| Useful APIs | All under `internal/` — **not importable** outside this module |
| Verb shape | Already library-shaped: `backup.Run(ctx, exec, Request)`, `match.Compare`, `prune.Run` |
| ZFS port | `zfs.Executor` + `Real` + `Fake` implemented |
| CLI | Thin: `opt.Parse` → Request → Run → print |
| Sylve today | Embeds Awk zelta, shells out via `runZeltaWithEnvStreaming`, scrapes lines/JSON |
| Sylve SSH | `buildZeltaEnv` → `ZELTA_REMOTE_*` env strings (key/port/BatchMode overrides) |
| Sylve progress | Callback per line → `AppendBackupEventOutput` |

**Sylve call sites** that must become library calls:

1. `backup --json --incremental --snapshot --snap-name N [--depth 1] SRC TGT`
2. Restore pull = same backup path with custom `RECV` flags + no-snapshot
3. `prune --no-ranges --keep-snap-num=N SRC TGT` → `Result.Candidates()`
4. Custom SSH: key, port, host-key policy (Go `Real` already has
   `BatchMode=yes`/`ConnectTimeout=30` but no `-i`/`-p` port yet)

## Target layout

```
zelta-go/
  endpoint/           # Parse, Endpoint, DSSuffix helpers
  zfs/                # Executor, Real, Fake, ListRow, SSHConfig
  match/              # Compare, Request/Result, Pair, filters
  backup/             # Run, Plan, Request/Result, lifecycle
  prune/              # Run, Request/Result, Candidates
  lineage/            # clone/revert planning/execution
  rotate/             # rotate planning/execution
  report/             # BackupResult JSON, col expansion, byte fmt
  internal/
    cmdbuild/         # data/cmds.tsv → argv (implementation detail)
    opt/              # CLI flag/env parse; opt.SendRecv exported for library
    conf/             # zelta.env path resolution (CLI)
    policy/           # zelta.conf job graphs (CLI-first; promote later)
    data embed        # TSVs stay via internal/ or data/
  cmd/zelta/          # consumer only
  sdk/                # external-module smoke test
```

**Promote, don't re-export stubs.** Move packages out of `internal/` and fix
imports. Today's type names (`backup.Request`, `zfs.Executor`) stay; CLI churn
is path-only.

**Stay internal** (not part of v1 SDK contract):

- `opt` — CLI/env hierarchy; integrators set `Request` fields directly
- `conf` — file path resolution
- `cmdbuild` — TSV argv machinery behind `zfs`/`backup`
- `policy` — Sylve has its own job model; promote later if wanted

## Sylve-critical API gaps (same batch as promote)

### 1. Remote transport on `zfs.Real` (done)

```go
type Remote interface {
    Argv(host, remoteCmd string, role Role) ([]string, error)
    Shell(host, remoteCmd string, role Role) (string, error)
}

// Structured OpenSSH (Sylve):
type SSHConfig struct { Bin, Port, IdentityFile, Options … }

// Awk REMOTE_* strings / mbuffer|socat (CLI + power users):
type CommandRemote struct { Command, Default, Send, Recv string }

type Real struct {
    ZFS    string
    SSH    SSHConfig // used when Remote == nil
    Remote Remote    // optional override
}
```

CLI: `cmd/zelta/remote.go` `remoteFromEnv()` → `CommandRemote` when
`ZELTA_REMOTE_*` is non-default, else `SSHConfig{}`.

Sylve drop-in: set `Real.SSH` (or `Remote: SSHConfig{…}`) — no env strings.

### 2. Progress / log callback

Add optional hook on backup (and prune if cheap):

```go
type Request struct {
    // ...
    OnLine func(line string)   // mid-run output/status lines
}
```

Implement by teeing pipe command output through `OnLine` when set. Final
`JSONReport` still returned. Sylve maps this to `AppendBackupEventOutput`.

### 3. Structured backup outcome

- Keep/populate `report.BackupResult` (JSON schema parity).
- Add typed `ErrCode` or `Status` field on `backup.Result` so Sylve can drop
  `classifyBackupOutput` string-scraping. Cases:
  - `ErrCodeSuccess`
  - `ErrCodeUpToDate` (no send needed)
  - `ErrCodeNoSource`
  - `ErrCodeNoSourceSnapshot`
  - `ErrCodeDiverged`
  - `ErrCodeTargetLocalWrites`
- Ensure skipped/up-to-date runs fill `JSONReport` when `req.JSON == true`.

### 4. Prune candidates API

Already have `Result.Candidates()` — export and document as the SDK prune
surface. No destroy in library (Sylve destroys itself; `zprune` stays CLI).

### 5. ZFS custom binary path

`zfs.Real{ZFS: "/usr/local/bin/zfs"}` already works via `rewriteBin`.
Document this — Sylve may use a custom zfs path in test/dev environments.

## CLI as consumer

After promote, `cmd/zelta/*` changes import paths only. If desired, wire
CLI env → Remote via `remoteFromEnv()` (done).

```
opt.Parse → build Request → backup.Run(ctx, &zfs.Real{}, req) → print
```

## What we explicitly do not do

- No god-object `Service` mirroring Sylve's 2.5k-line orchestrator
- No promoting `opt`/`conf`/`policy` in v1
- No public remote until Daniel says otherwise (private Gitea module +
  `GOPRIVATE` is fine for Sylve)
- No S3/blob
- No silent destructive prune in library
- No behavioural rewrite during `git mv` — promote first, then gaps

## Migration phases

### Phase A — Mechanical promote

1. `git mv` each package: `endpoint`, `zfs`, `match`, `backup`, `prune`,
   `lineage`, `rotate`, `report` out of `internal/`.
2. Update all import paths across the module.
3. `make test vet build shelltest` green.
4. Update `AGENTS.md`, `agents/09-style-go.md`, `agents/11-roadmap.md`,
   `agents/14-maturity.md`, `README.md`.

### Phase B — Sylve-critical API

1. `zfs.SSHConfig` + unit tests for argv shape.
2. `Request.OnLine` on backup + test.
3. Typed backup status (`ErrCode`) + test aligned with Sylve's
   `backup_outcome.go` cases.
4. Doc comments on every exported type/func (godoc is the contract).
5. Ensure `JSONReport` populated in all paths (including skipped/up-to-date).

### Phase C — External consumer proof

1. `sdk/example_test.go` as package `sdk_test` importing **only** public
   packages (fails if something needs `internal`).
2. Minimal example exercising backup + match + prune.

### Phase D — Sylve integration (out of repo, this doc guides)

Drop-in map for Sylve:

| Sylve today | zelta-go SDK |
|-------------|--------------|
| `runZeltaWithEnvStreaming(…, backup …)` | `backup.Run` + `OnLine` |
| `buildZeltaEnv` → `REMOTE_*` | `zfs.Real{SSH: …}` |
| `PruneCandidatesWithTarget` | `prune.Run` + `Candidates()` |
| `classifyBackupOutput` | typed `Result.ErrCode` |
| `EnsureZeltaInstalled` embed | **delete** (link library) |
| Regex size/stream scrapers | `report.BackupResult` fields |

Sylve keeps: DB, queues, jail/VM fences, HA, destroy, manifests.

## Verification (stop conditions)

| Phase | Check |
|-------|-------|
| A | `make test vet build shelltest` green; no imports of moved pkgs from wrong path |
| B | Package tests for SSH argv, OnLine, typed errors; JSONReport in skip path |
| C | `sdk/example_test.go` builds with only public imports |
| D | (not in this repo) Sylve cutover PR |

## Doc updates

- `agents/11-roadmap.md`: current phase → SDK; library boundary item marked
  in progress/complete
- `agents/14-maturity.md`: Public Go library → `IMPLEMENTED` then
  `FAKE-VERIFIED` with sdk tests
- `agents/10-deviations.md`: SSH structured config vs Awk REMOTE_* strings;
  progress hooks as Go-where-it-wins
- `agents/00-contracts.md`: library vs CLI note
- `README.md`: Library Status section updated
- `agents/09-style-go.md`: package boundary change (`internal/` private →
  curated public)

## Risk notes

- **Import graph:** `report` ↔ `endpoint`, `backup` → `match`/`zfs` — keep
  acyclic; `cmdbuild` stays internal so public pkgs may import
  `internal/cmdbuild` (allowed: public → internal of same module).
- **API freeze anxiety:** start conservative; unexport anything only CLI needs
  after audit.
- **Progress fidelity:** first cut tees command output lines; pixel-perfect
  Awk progress is not required for Sylve's append-log use case.
