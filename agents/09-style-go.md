# Go style (this repo)

Daniel is not a Go expert — keep code boring and readable.

## Conventions

- Public SDK packages at module root (`backup`, `match`, `zfs`, …); `internal/` for CLI/implementation detail (`opt`, `cmdbuild`, `conf`, `policy`); `cmd/` thin consumer only. See `agents/16-sdk.md`.
- Interfaces: prefer small ports; OK on provider (`zfs.Executor`) when multiple consumers share them — don’t churn just to match the proverb
- Errors: wrap with `%w`; map exit codes at CLI boundary  
- Tabs for Go (gofmt)  
- No comments unless non-obvious WHY  
- No new deps without a clear win; stdlib first  
- Name types after Zelta vocabulary (`Endpoint`, `Pair`, `Config`) where it helps cross-impl reading  
- Data/column keys stay snake_case (`ds_suffix`); Go identifiers stay CamelCase
- Homegrown key:value `meta.yaml` in goldens is fine until real YAML earns a dep
- Domain helpers may echo AWK shape (`isTruthyWritten`) when private and clearer for porting

**Decision (keep):** no extra linters/style RFCs yet. Oracle/golden fidelity beats Go-shaped internals. Mild prefs (`[]*Pair`, provider-side Executor) stay until they hurt.

## File size

Split before 400 LOC. Prefer many small files over god packages.

## Embed

Data tables embedded at build; tests may read `data/` from repo root via relative path or embed test helper.
