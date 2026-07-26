# Policy

**Package:** `internal/policy`
**CLI:** `zelta policy`

YAML-like `zelta.conf` job graphs. Go keeps common-layout parity with Awk
(including repeated `options:`/`datasets:` fan-out and textual `import:`) while
following the documented option hierarchy in `00-contracts.md`.

## MVP scope (current)

- Load config (`-C` / `ZELTA_CONFIG` / `$ZELTA_ETC/zelta.conf`)
- Expand `import:` fragments (relative paths, indent splice, depth ≤ 8, loop guard)
- Resolve jobs: site → host → datasets → source/target endpoints
- Dry-run table: `-n` / `--dryrun`, scripting `-H`
- Operand filter (OR): site | host | source_remote | source ds | target | `host:ds` | source leaf

**Defer:** job execution, `JOBS`/`RETRY`, `--backup-command`,
full backup-flag forwarding into live runs, `ARCHIVE_ROOT`.

## Config shape

```yaml
BACKUP_ROOT: vault:tank/Backups
ADD_HOST_PREFIX: 1        # legacy: HOST_PREFIX
ADD_DATASET_PREFIX: 0     # legacy: PREFIX; 0=leaf, N=last N+1 labels
# SEND_INTR / INTERMEDIATE, JOBS / THREADS, SNAP_MODE / SNAPSHOT, …

SITE_NAME:
  host.example:
  - pool/ds
  - pool/ds: user@other:tank/path
  host2:
    options:
      import: targets/vault.yaml
      import: rules/hostbackup.yaml
    datasets:
      import: sources/host2.yaml
    options:                 # repeat block = multi-target fan-out
      BACKUP_ROOT: other:tank/B
    datasets:
      - pool/ds
```

## Target resolution

1. Explicit `: target` on the dataset line wins (colon+space split keeps `host:path`).
2. Else `BACKUP_ROOT` required (else warn + skip).
3. If `ADD_HOST_PREFIX` truthy → append `/` + host (user@ stripped).
4. Append source path labels from index `n - ADD_DATASET_PREFIX` through `n`
   (AWK numeric; `PREFIX=-1` appends nothing — doc says full path, code wins).

Source endpoint is always `{source_remote}:{dataset}` (including `localhost:`).

## Precedence (Go / contracts)

1. Built-in defaults
2. `zelta.env`
3. conf scopes: global < site < host (`options:`)
4. Process `ZELTA_*`
5. CLI flags

**Deviation:** Awk policy skips exporting defaults/`zelta.env` into AWK `Opt`, so
conf effectively beats env-file there. Go follows `00-contracts` instead.

## Dry-run output

- Default: header `SOURCE`/`TARGET`; width = max(6, longest source); two spaces.
- `-H`: no header; single space.
- No ZFS/SSH.

## Known option keys

Any `opts.tsv` key whose VERBS column contains `policy` or `all`. Unknown conf
keys error (`unknown option: NAME`). Legacy aliases from KEY_ALIAS column.
