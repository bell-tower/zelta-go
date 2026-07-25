# Match goldens

Each case directory:

```
meta.yaml
src.list
tgt.list
expected.out
expected.err
expected.exit
```

Populate by recording production `zelta match` against known pools (expensive session), then iterate Go against fixtures.

## Cases

| Dir | Shape |
|-----|--------|
| `basic-*`, `incremental-H`, `diverged-H`, … | Hand-small Fake fixtures |
| `live-inc-H` / `live-inc-human` | debian `apool/treetop`→`bpool/bleetop` (spaces, incremental) |
| `live-uptodate-H` | debian `apool/beetop`→`bpool/bleetop2` |
| `live-mixed-H` | debian `apool/hmm`→`bpool/bleetop` (blocked + tgt-only) |
| `live-cols-H` | `-o ds_suffix,match,info` |
| `live-depth2-H` | `-d 2` |
| `live-xfer-H` / `live-xfer-human` | xfer_num / xfer_size / num_matches |
| `live-synonym-H` | `-o dssuffix,last,info` synonym expansion |
| `live-exclude-vol1-H` / `minus` / `lift` | `-X` DS globs |
| `live-include-minus-H` | `--include /minus` (+ cascade) |
| `live-snap-exclude-H` | `-X @zelta_2026*` (source snaps) |
| `live-exclude-exact-H` | exact source path → tgt-only + ghost src_last |
| `written-block-H` | match==tgt_last but tgt written ≠ 0 |
| `bookmark-match-H` | source `#` matches tgt `@` → match string diverges |
| `bookmark-uptodate-H` | snap preferred over bookmark for match/GUID |
| `nowritten-H` | `--no-written` (name,guid list only) |

Optional `meta.yaml` keys: `cols`, `depth`, `parsable`, `include`, `exclude`, `nowritten`, `time`.
Live dumps: oracle Zelta 1.2.0 (or host zelta at capture); lists are `zfs list -Hpr -t all -S createtxg -o name,guid,written,creation,used`.
