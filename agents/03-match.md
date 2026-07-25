# Match — Phase 1 focus

**Package:** `internal/match`  
**CLI:** `zelta match [flags] SRC TGT`

## Goal

Feature-bound toward Awk `zelta match` output (human + `-H` scripting). Pixel-tight for release goldens; debug chrome may lag.

## Pipeline (split files; keep each small)

1. Plan `zfs list` props from column needs  
2. Fetch via `zfs.Executor` (Fake in unit tests)  
3. Parse rows → source/target trees  
4. Pair by `ds_suffix`; GUID (and ivset when needed) match  
5. Derive info, xfer sizes, src_next / srclast / tgtlast, …  
6. Render with `cols.tsv` (`-o`, `-H`, `-p`)

## Do not

- Call out to another zelta process  
- Implement full prune UI here (extract contracts to `05-prune.md` when touching prune-in-match logic)  
- Load backup.awk

## Contract notes (extracted)

- List props: `name,guid,written,creation,used` · `-t all -S createtxg` (newest snaps first)
- Default cols: `dssuffix,match,last,info` → `ds_suffix,match,src_last,tgt_last,info`
- Root `ds_suffix` = `""`; human `[LEAF]` (source last path component); `-H` keeps empty
- Savepoints keep `@`/`#`; GUID match; first match newest→oldest = latest common
- Info: `up-to-date` · `syncable (full|incremental)` · `blocked sync: …` · `no source (target only)`
- Blocked: no target snapshots · match ≠ tgt_last · target written
- Bookmarks (`#`): listed with snaps; GUID index prefers snapshot; match requires **target** snapshot; match string is **source** savepoint (so `#x` vs `@x` → diverged)

## Goldens

```
testdata/golden/match/<case>/
  meta.yaml       # source, target, scripting: true|false
  src.list        # canned zfs list -Hpr
  tgt.list
  expected.out
  expected.err
  expected.exit
```

Cases: `basic-*`, `incremental-H`, `diverged-H`, `tgt-only-H`, `no-tgt-snaps-H`,
live dumps `live-inc-*`, `live-uptodate-H`, `live-mixed-H`, plus flag cases
`live-cols-H`, `live-depth2-H`, `live-xfer-H`, `live-xfer-human`, `live-synonym-H`.
Regenerate oracle from production Awk only in an intentional expensive session; commit results; iterate Go cheaply against fixtures.

## Flags (phase 1)

- `-H` scripting · `-p` parsable sizes · `-d N` depth (zfs list `-d` + pair filter) · `-o` cols via `report.ExpandProplist` (`cols.tsv` synonyms; default `dssuffix,match,last,info`)
- `-X`/`--exclude`, `--include` — see `filter.go` (comma lists / repeated flags; DS globs cascade via `(/.*)?`; `@` snap filters = **source only**; DS filters = dataset rows only)
- `--written` (default on) / `--no-written` — list props `name,guid[,written,creation,used]`; `-p` without written/size cols also skips written props (oracle `add_written`)
- `--time` — append `SOURCE_LIST_TIME` / `TARGET_LIST_TIME` (seconds) after table (stdout)

## Related packages

`endpoint`, `zfs` (listparse + Fake + Real local/ssh), `report` (columns), `opt` (flags).

## Live smoke (lab)

`Real` sshes for `user@host:ds` (not localhost). Example:

```sh
./bin/zelta match -H 'root@debian:apool/treetop' 'root@debian:bpool/bleetop'
```

Prefer `debian` for scratch; `vault1.djb0` is production (apool/cpool only).
