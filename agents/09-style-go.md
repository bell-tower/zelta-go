# Go style (this repo)

Daniel is not a Go expert — keep code boring and readable.

## Conventions

- `internal/` private library code; `cmd/` thin  
- Interfaces at consumer (`zfs.Executor`)  
- Errors: wrap with `%w`; map exit codes at CLI boundary  
- Tabs for Go (gofmt)  
- No comments unless non-obvious WHY  
- No new deps without a clear win; stdlib first  
- Name types after Zelta vocabulary (`Endpoint`, `Pair`, `Config`) where it helps cross-impl reading  

## File size

Split before 400 LOC. Prefer many small files over god packages.

## Embed

Data tables embedded at build; tests may read `data/` from repo root via relative path or embed test helper.
