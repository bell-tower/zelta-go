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

## Goldens

```
testdata/golden/match/<case>/
  meta.yaml       # args, env notes
  src.list        # canned zfs list -Hpr
  tgt.list
  expected.out
  expected.err
  expected.exit
```

Regenerate oracle from production Awk only in an intentional expensive session; commit results; iterate Go cheaply against fixtures.

## Related packages

`endpoint`, `zfs` (listparse + Fake), `report` (columns), `opt` (flags).
