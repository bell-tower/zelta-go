# Endpoint package

**Package:** `internal/endpoint`

Parse and format Zelta endpoints without shell.

## Forms

- `pool/dataset`
- `pool/dataset@snap`
- `host:pool/dataset`
- `user@host:pool/dataset`
- `user@[ipv6]:pool/dataset`

## Responsibilities

- Split user, host, dataset, snapshot
- `ds_suffix` relative to a tree root
- Round-trip string ↔ struct for tests

## Tests

Table-driven only; no ZFS. Edge cases: IPv6, missing parts, depth limits deferred to callers.
