# Zelta Go — Agent Guide

**Private.** Not public until fully documented. No man pages in this repo.

Zelta Go is a **data-driven ZFS composer**: library + single binary, feature-bound toward parity with Zelta 1.2 (Bourne+AWK) and zprune. Parallel experiment — goldens from current Awk are the oracle; Ruby may adopt contracts later.

Sibling reference tree (read only when extracting a contract): `~/Code/zelta/`.

---

## Before You Do Anything

1. Read **this file** (router only).
2. Read **`AGENTS-Persona.md`** if present (gitignored local persona + human context).
3. Query **Memory Palace MCP** for cross-repo / business / session intelligence (see Persona).
4. Open **one** `agents/NN-*.md` for the package you will touch.
5. Prefer `testdata/golden/**` + contracts over reading AWK source.

---

## Token Budget Rules (non-negotiable)

1. **One package per session** when possible. Soft max **~400 LOC/file**.
2. Never load full `~/Code/zelta/share/zelta/*.awk` into context.
3. Behavior truth = `testdata/golden/**` + `agents/00-contracts.md`, not AWK.
4. When porting: extract **one** contract note into the relevant `agents/` file, then implement from that.
5. Cheap loop: `go test ./internal/<pkg>/...` — no ZFS, no sudo.
6. Expensive loop (OK with frontier models): golden regen from real `zelta match`, integration tags.
7. Do not invent S3/blob here; that waits for Ruby product line.

---

## Product Role

| Line | Role |
|------|------|
| Bourne+AWK (`~/Code/zelta`) | Zelta Portable lineage; production today |
| Ruby (planned) | Future authoritative contracts + blob |
| **Go (this repo)** | Single binary + library; rescue media; integrators |

Goals: library, feature-equivalent CLI, pixel-tight match/zprune efficiency for releases, Go-where-it-wins experiments, private Gitea until docs are real.

Acceptable deviations: YAML policy → standardize; no recursive self-calls; embed all data except man pages (use `~/Code/zelta/doc` / `ZELTA_DOC`); shellspec scenario-level parity after install checks.

---

## Agent Doc Map

| File | Open when… |
|------|------------|
| `agents/00-contracts.md` | Safety, terminology, env hierarchy, multi-impl notes |
| `agents/01-data.md` | Editing `data/*.tsv` |
| `agents/02-endpoint.md` | Endpoint parse / ds_suffix |
| `agents/03-match.md` | **Phase 1 focus** — compare engine + goldens |
| `agents/04-backup.md` | Send/recv plan (phase 2+) |
| `agents/05-prune.md` | Prune analysis / zprune (later) |
| `agents/06-policy.md` | Policy conf (later) |
| `agents/07-cli.md` | `cmd/zelta`, `cmd/zprune` |
| `agents/08-testing.md` | Unit / golden / integration |
| `agents/09-style-go.md` | Go conventions for this repo |

---

## Layout

```
data/                 # Owned TSV (Go source of truth); //go:embed into binary
internal/endpoint/    # user@host:pool/ds@snap
internal/opt/         # opts.tsv + env hierarchy
internal/cmdbuild/    # cmds.tsv → argv (no shell recursion)
internal/zfs/         # Executor, Real, Fake, list parse
internal/match/       # Phase 1
internal/report/      # cols.tsv + json.tsv render
internal/conf/        # zelta.env paths + KEY=value
internal/backup|prune|policy/  # stubs until needed
cmd/zelta/            # thin dispatcher
cmd/zprune/           # later
testdata/golden/      # oracle fixtures
agents/               # split agent docs
```

External docs (do not copy wholesale): `~/Code/zelta/doc/` especially `zelta-options.7.md`, `wiki/conf/env.md`.

---

## Commands

```sh
make test          # unit + golden
make build         # bin/zelta
go test ./internal/match/...
go test -tags=integration ./...   # real ZFS; rare
```

---

## Safety (always)

- Read-only backups by default
- No destructive `--force` in core zelta verbs
- Divergent datasets renamed/preserved, never overwritten
- Fail safe; no dangerous assumptions
- zprune is the destructive wrapper; keep guard semantics

---

## Privacy / Git

- Private Gitea only until Daniel says otherwise
- Do not publish, open-source, or add public remotes
- `AGENTS-Persona.md` is gitignored — never commit it
