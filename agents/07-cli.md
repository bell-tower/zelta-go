# CLI

**Packages:** `cmd/zelta`, later `cmd/zprune`

## Rules

- Thin main: parse verb → library  
- Stdlib `flag` + small dispatch first; avoid heavy CLI frameworks unless pain is real  
- `version` always works  
- Unknown verbs: clear error; point at main zelta docs  
- Help: no man pages here; if `ZELTA_DOC` or `~/Code/zelta/doc` exists, optional handoff later  

## Phase 1 verbs

- `version`  
- `match` (growing)  
- others: explicit not-implemented  
