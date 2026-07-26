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
| Useful APIs | Public packages at module root (`backup/`, `match/`, `zfs/`, …) |
| Verb shape | `backup.Run`, `match.Compare`, `prune.Run`, lineage/rotate |
| ZFS port | `zfs.Executor` + `Real` + `Fake` + `Remote` (`SSHConfig`, `CommandRemote`) |
| Sylve-critical | `SSHConfig`, `Request.OnLine`, `backup.ErrCode`, `JSONReport`, `Candidates()` |
| External proof | `sdk/sdk_test.go` imports public packages only |
| CLI | Thin: `opt.Parse` → Request → Run → print; `remoteFromEnv()` |
| Sylve today (still) | Embeds Awk zelta, shells out, scrapes lines/JSON — **Phase D out of repo** |
| Sylve SSH (still) | Backup client = OpenSSH argv; Awk path = `ZELTA_REMOTE_*` via thinner env |
| Sylve progress | Callback per line → `AppendBackupEventOutput` → map to `OnLine` |

**Phases A–C done** (promote + API + sdk smoke). **Phase D** = Sylve cutover PR.

**Sylve call sites** → library:

1. `backup --json --incremental --snapshot --snap-name N [--depth 1] SRC TGT` → `backup.Run`
2. Restore pull = same backup path with custom `recv` flags + no-snapshot
3. `prune --no-ranges --keep-snap-num=N SRC TGT` → `prune.Run` + `Candidates()`
4. Custom SSH: `Real{SSH: SSHConfig{IdentityFile, Port, Options: …}}`

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

### Transport decision (locked with Hayzam)

| Component | Transport | Notes |
|-----------|-----------|--------|
| **zelta-go client** | OpenSSH **binary** via `SSHConfig` / `CommandRemote` | No `golang.org/x/crypto/ssh` client. Keeps `~/.ssh/config`, agent, ProxyJump, ControlMaster. |
| **Sylve backup / ZFS client** | OpenSSH binary (`buildSSHArgs`) | On-disk keys `data/ssh/target-N_id`; BatchMode; StrictHostKeyChecking=accept-new; timeouts; **own** ControlMaster/Path/Persist under `/tmp/sylve-ssh-*.sock`; no ForwardAgent. |
| **Sylve → Awk zelta** | `buildZeltaEnv` → `ZELTA_REMOTE_*` | Thinner: BatchMode / hostkey / `-p` / `-i` only — **no** ControlMaster today. |
| **Sylve cluster peer exec** | `x/crypto/ssh` **server** only (`embedded_ssh.go`) | In-process listener, cluster ed25519 host key, pubkey from raft identities, `exec` → `/bin/sh -c`. Not used for backup streams. |

**OpenSSH config is not disabled** (`-F` unused). CLI/library `-o`/`-i`/`-p` override those knobs only; Host blocks, ProxyJump, agent, and `SSH_AUTH_SOCK` still apply when the caller does not force a private key.

Zero-value `SSHConfig{}` ≈ stock `ssh` + BatchMode + ConnectTimeout=30 — fine for operator CLI. Sylve should set explicit `IdentityFile`, `Port`, and `Options` (mux, hostkey policy) to match `buildSSHArgs`.

Suggested Sylve `SSHConfig` mapping from `buildSSHArgs`:

```go
zfs.SSHConfig{
    IdentityFile: keyPath, // data/ssh/target-N_id
    Port:         port,    // if non-default
    Options: []string{
        "StrictHostKeyChecking=accept-new",
        "ControlMaster=auto",
        "ControlPath=/tmp/sylve-ssh-" + id + ".sock",
        "ControlPersist=60",
        // plus whatever timeouts Sylve already sets
    },
}
// BatchMode=yes and ConnectTimeout=30 are defaults on SSHConfig
```

Bastion + agent-forward docs for human operators remain valid for CLI/`CommandRemote`; Sylve appliance path is keys+flags, not that model.

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

### Phase A — Mechanical promote — **DONE** (`5a9a88a`)

Packages at root; imports fixed; agents/README layout updated.

### Phase B — Sylve-critical API — **DONE** (`5a9a88a`, `8788f42`)

`SSHConfig` + `Remote`/`CommandRemote`, `OnLine`, `ErrCode`, godoc examples,
CLI `remoteFromEnv()`.

### Phase C — External consumer proof — **DONE** (`sdk/sdk_test.go`)

Public-import-only smoke for backup + match + prune surface.

### Phase D — Sylve integration (out of repo) — **PoC started**

Drop-in map:

| Sylve today | zelta-go SDK |
|-------------|--------------|
| `runZeltaWithEnvStreaming(…, backup …)` | `backup.Run` + `OnLine` |
| `buildSSHArgs` / `buildZeltaEnv` | `zfs.Real{SSH: SSHConfig{…}}` (see mapping above) |
| `PruneCandidatesWithTarget` | `prune.Run` + `Candidates()` |
| `classifyBackupOutput` | `Result.ErrCode` (+ text hints for legacy classifier) |
| `EnsureZeltaInstalled` embed | **keep for now** (flagged path only) |
| Regex size/stream scrapers | `report.BackupResult` fields |
| Cluster peer SSH server | stays `x/crypto/ssh` in Sylve — **not** zelta-go |

Sylve keeps: DB, queues, jail/VM fences, HA, destroy, manifests, embedded
cluster SSH server.

#### PoC status (2026-07-26, `devhost` / vault1) — **three boring wins done**

**Host layout**

| Path | Role |
|------|------|
| `/root/Sylve` | Sylve + PoC patches (not pushed upstream) |
| Gitea `djbell/zelta-go@294e570` | module source (`GOPRIVATE` + SSH `insteadOf`) |
| `apool/zelta-go-poc/src` | disposable source (**not** `apool/treetop`) |
| `rust07-scratch/zelta-go-poc/*` | disposable targets (cross-pool; same-pool blocked by Sylve) |
| `apool/treetop` | golden-like — **do not destroy** |
| `bpool` | not imported on this host |

**Sylve changes (gated, default off)**

1. `go.mod`: `require git.belltower.it/djbell/zelta-go v0.0.0-20260726214018-294e570afb73`  
   — **no `replace`**; needs  
   `git config --global url."git@git.belltower.it:".insteadOf "https://git.belltower.it/"`  
   and `GOPRIVATE=git.belltower.it`
2. `internal/services/zelta/zelta_go_backup.go` — `useZeltaGo()`,  
   `sshConfigFromTarget`, `backupWithZeltaGo` → `backup.Run` + JSON + `OnLine`
3. `backupWithEventProgressSnapshotNameRecursive` — `SYLVE_ZELTA_GO=1|true|yes|on` → Go; else Awk
4. Tests: `zelta_go_backup_test.go`; real-ZFS  
   `zelta_go_poc_integration_test.go` (`//go:build sylve_zelta_go_poc`)

**Verified**

| Check | Result |
|-------|--------|
| Unit `TestUseZeltaGo` / `TestErrCodeHint` | PASS |
| `TestPhaseD_RunBackupJob_ZeltaGo` (`runBackupJob` + flag) | PASS — event `success`, GUID match |
| `TestPhaseD_DualRun_AwkVsGo` | PASS — per-path GUIDs + content parity (mount noauto) |
| Module from Gitea without `replace` | PASS (re-ran Phase D tests) |

```sh
# on builder with Gitea SSH access:
export GOPRIVATE=git.belltower.it GONOSUMDB=git.belltower.it
git config --global url."git@git.belltower.it:".insteadOf "https://git.belltower.it/"
cd /root/Sylve
go test -tags=sylve_zelta_go_poc ./internal/services/zelta -run TestPhaseD -count=1 -v -timeout 10m
```

**Notes**

- Same-pool source/target refused by `validateBackupScopesDoNotOverlapTarget` — use apool→rust07-scratch.
- Awk fails if snap name already exists; dual-run uses separate snap names.
- Successful runs classify as `unknown` (no failure/up-to-date substrings) — expected; GUID is the oracle.
- **Not yet:** restore/prune cutover, delete embed, default-on flag, Hayzam PR.

**Enable in a running Sylve**

```sh
export SYLVE_ZELTA_GO=1   # process env for the Sylve daemon
```

### Optional polish (this repo, not blocking D)

- Godoc pass on every exported symbol if anything thin remains
- README polish deferred until after first Sylve link or Daniel asks
- Policy remains CLI-first (`internal/policy`); promote only if an integrator wants conf graphs

## Verification (stop conditions)

| Phase | Check |
|-------|-------|
| A–C | Done — unit/sdk tests + public packages |
| D | (not in this repo) Sylve cutover PR using `GOPRIVATE` + module path |

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
