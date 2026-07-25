# Backup (phase 2)

**Package:** `internal/backup`
**CLI:** `zelta backup [-n] [-Ii] [--snap|--no-snap] [-d depth] [-X pat] SRC TGT`

## Pipeline

1. `match.Compare` (in-process)
2. **Parent CREATE** if tgt missing: `zfs create -vupo canmount=noauto PARENT` (default on; runs even on `-n`; retry on OpenZFS readonly `-up` bug)
3. **Snap IF_NEEDED** (default): source written or missing snaps → `zfs snapshot -r SRC@zelta_…`
4. `PlanFromMatch` + optional `ApplySourceSnap` (predicted on dry-run)
5. `cmdbuild` SEND/RECV flags from `opt.Resolve()` (SEND_DEFAULT, RECV_TOP/FS/VOL/PARTIAL, RESUME)
6. Dry-run: `would sync N` + snap + first-pass `PipeShell` (same-host ssh hairpin)
7. Execute: `Snapshot` then `RunPipe`; intermediate full does second-pass incr

## Plan rules

| Info | Kind | Send |
|------|------|------|
| `syncable (full)` + `-I` | Full | first: `src@src_next` (earliest); execute 2nd: `-I` → `src_last` |
| `syncable (full)` + `-i` | Full | `send … src@src_last` once |
| `syncable (incremental)` | Incr | `send … -I src@match src@end` (default `-I`) |
| `up-to-date` | Skip → Incr if snap | after snap: match→new |
| `blocked…` / tgt-only | Blocked | no force |

## Recv flags (oracle defaults)

| Case | Flags |
|------|--------|
| Full root FS | `-o readonly=on` + `-u -x mountpoint -o canmount=noauto` + `-s` |
| Full child FS | `-u -x mountpoint -o canmount=noauto` + `-s` |
| Full volume | RECV_VOL (default empty) + `-s` |
| Incr | `-s` only (RESUME default on) |

## Sync direction (dual-remote)

| Direction | Shape |
|-----------|-------|
| PULL (default) | `ssh -n TGT "ssh -n SRC send | recv"` |
| PUSH (`--push`) | `ssh -n SRC "send | ssh TGT recv"` |
| `--no-pull` / `ZELTA_SYNC_DIRECTION=0` | controller `{ ssh -n SRC send|ssh TGT recv ; }` + one warning |
| same remote host | hairpin `ssh -n HOST "{ send | recv ; }"` regardless |

## Snap modes

- `IF_NEEDED` (default): written ≠ 0 or no src snaps
- `ALWAYS` (`--snap`): always snapshot root
- `NEVER` (`--no-snap`): never

## Defer

Send-check feature drop, resume tokens, rotate/clone, origin-match. Bookmark
dry-run/failure parity remains follow-up work.

Intentional Awk≠Go notes (dry-run first-pass, quoting, RECV constants, …): **`agents/10-deviations.md`**.

## Safety

- No force-overwrite of divergent targets
- Recv `-o readonly=on`
- Execute is real — prefer `-n` first; scratch targets on debian only
