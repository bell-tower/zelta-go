# Zelta Go — Agent Guide

**Private.** Not public until fully documented. No man pages in this repo.

Zelta Go is a **private, experimentally feature-rich data-driven ZFS composer**: library-in-progress + single binary, feature-bound toward parity with Zelta 1.2 (Bourne+AWK) and zprune. It is not production-ready, feature-equivalent, or a public Go library yet. Goldens from current Awk are the compatibility oracle; Ruby may adopt contracts later.

Sibling reference tree (read only when extracting a contract): `~/Code/zelta/`.

---

## Before You Do Anything

1. Read **this file** (router only).
2. Read **`AGENTS-Persona.md`** if present (gitignored local persona + human context).
3. Query **Memory Palace MCP** for cross-repo / business / session intelligence (see Persona).
4. Read `agents/11-roadmap.md` for current strategy and maturity boundaries.
5. Open **one** package/operation-specific `agents/NN-*.md` file.
6. Prefer `testdata/golden/**` + contracts over reading AWK source.

---

## Token Budget Rules (non-negotiable)

1. **One package per session** when possible. Soft max **~400 LOC/file**.
2. Never load full `~/Code/zelta/share/zelta/*.awk` into context.
3. Behavior truth = `testdata/golden/**` + `agents/00-contracts.md`, not AWK.
4. When porting: extract **one** contract note into the relevant `agents/` file, then implement from that.
5. Cheap loop: `go test ./internal/<pkg>/...` — no ZFS, no sudo.
6. Expensive loop (OK with frontier models): golden regen from real `zelta match`, integration tags.
7. Do not invent S3/blob here; that waits for Ruby product line.
8. Passing tests does not authorize adjacent feature work.
9. Never start work beyond the explicitly specified bounded loops.

### Model and task fit

- **Tight-loop model:** one package, one contract, one failing test or golden mismatch.
- **High-context model:** bounded architecture review, contract extraction, risk review, or test-matrix design. It must return a short decision/contract before implementation.
- No model may ingest the whole sibling Awk tree and then autonomously broaden scope.
- Every implementation session has explicit stop conditions for each specified bounded loop: focused test, relevant full test, vet/build as appropriate, diff review, status review, report, stop.

### Subagents (prefer when compound context gets expensive)

Use Task/subagents **proactively** when work would otherwise stuff the main thread with parallel reads, oracle slices, or multi-file greps. Compound token interest shows up fast once match/backup/prune workflows deepen — clarity, speed, and $ all suffer if one context holds every parse.

| Prefer subagent when… | Keep on main thread when… |
|-----------------------|---------------------------|
| Extracting a contract from AWK/doc (bounded slice → note) | Single-file edit you already have open |
| Building/regen a golden case (fake zfs + oracle capture) | Tight `go test` fix loop on known failure |
| Exploring sibling `~/Code/zelta` without loading it here | Implementing from an already-written contract |
| Parallel research (cols + shellspec + list props) | Trivial one-shot answers |

**How:** narrow prompt; demand a short return (contract bullets, file paths, expected I/O) — not a dump of source. Main agent implements from that summary. Explore for search; general for multi-step extract+fixture. Do not fan out subagents for work that fits in one cheap test cycle.

---

## Product Role

| Line | Role |
|------|------|
| Bourne+AWK (`~/Code/zelta`) | Zelta Portable lineage; production today |
| Ruby (planned) | Future authoritative contracts + blob |
| **Go (this repo)** | Single binary + library; rescue media; integrators |

Goals: library, feature-equivalent CLI, pixel-tight match/zprune efficiency for releases, Go-where-it-wins experiments, private Gitea until docs are real.

Current posture: match, backup, read-only prune, clone, revert, rotate, and lineage code exist with deterministic tests. Real-system lifecycle verification and the public library facade remain incomplete. See `agents/14-maturity.md`.

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
| `agents/10-deviations.md` | **Intentional** Awk→Go deviations (full log; see also `00-contracts`) |
| `agents/11-roadmap.md` | Current strategy, ordered backlog, explicit non-goals |
| `agents/12-rotate.md` | Rotate and clone-origin safety contract |
| `agents/13-lineage.md` | Clone, revert, and four-endpoint lineage contract |
| `agents/14-maturity.md` | Capability states and release/readiness matrix |
| `agents/15-session-protocol.md` | Model selection, bounded loops, stop conditions |

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
internal/backup/         # replication planning/execution; lifecycle gaps remain
internal/prune/          # read-only retention analysis
internal/lineage/        # clone/revert lineage operations
internal/rotate/         # rotate planning/execution; recovery gaps remain
internal/policy/         # intentionally deferred
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

Do not run integration tests, golden regeneration, or real ZFS commands unless the
task explicitly calls for them. Prefer the smallest package test first.

## Session stop protocol

Before reporting completion, the editor must:

1. Run the smallest relevant test.
2. Run broader tests/vet/build only when relevant.
3. Review `git diff` and `git status`.
4. Record a contract/deviation only if behavior actually changed.
5. Report verified behavior and unverified assumptions.
6. Stop after the specified bounded loops. Do not select additional roadmap work automatically.

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
