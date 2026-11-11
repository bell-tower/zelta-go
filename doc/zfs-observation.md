# ZFS Observation Substrate

Zelta's ZFS observation model follows the public concepts used by OpenZFS,
without depending on cgo or private `zfs_handle_t` layout.

## Shape

OpenZFS presents a dataset as an identified object with a dataset type and a
property set. Each property has a value and provenance (`default`, `local`,
`inherited`, `temporary`, `received`, or `none`). `zfs get -j` expresses this as
datasets containing nested property records.

`zfs.Dataset` and `zfs.Property` mirror those concepts. Property values remain
raw JSON in the observation layer. Typed access is an explicit operation such
as `Property.String` or `Property.Uint64`, rather than a lossy conversion while
reading the provider output.

## Portability

The default provider remains the OpenZFS command interface over local or SSH
execution. It works without ZFS headers, libraries, or cgo on the controller.
The observation types are deliberately independent of that provider.

A future libzfs provider may translate `zfs_get_all_props`,
`zfs_get_recvd_props`, and iterator callbacks into the same observations. It
must not expose `zfs_handle_t` or require consumers to link against a specific
OpenZFS ABI.

This avoids cgo portability costs:

- OpenZFS is not available on every build platform.
- libzfs ABI and headers vary by OpenZFS/FreeBSD/Linux release.
- cgo complicates cross-compilation and static distribution.
- A linked libzfs provider would be local-host-only while Zelta's normal model
  is remote-capable through SSH.

## Migration Boundary

`ListRow` and `DatasetContext` remain compatibility/parser structures during the
first migration. Match and backup should move to observations only after the
provider model has fixture coverage for both tabular output and `zfs get -j`.

## Provider Decision

Zelta uses a provider-neutral ZFS object model with replaceable observation and
operation providers. The default provider is command-based and remains capable
of local and SSH-backed operation. An optional libzfs provider may be added for
local systems later.

The default provider is not a fake libzfs implementation. It is a
`ShellProvider` that produces the same observations and implements the same
capabilities using structured ZFS commands. This keeps the model familiar to
OpenZFS developers without pretending that command execution has libzfs handle
semantics.

Use capability interfaces rather than one all-purpose provider interface:

```go
type Observer interface {
    Dataset(ctx context.Context, name string, opts ReadOptions) (*Dataset, error)
    Tree(ctx context.Context, root string, opts ReadOptions) (*DatasetTree, error)
}

type LocalMutator interface {
    Create(ctx context.Context, req CreateRequest) error
    Snapshot(ctx context.Context, req SnapshotRequest) error
    Rename(ctx context.Context, req RenameRequest) error
    Clone(ctx context.Context, req CloneRequest) error
}
```

Streaming remains a separate capability. A local libzfs provider is not
required to implement the remote stream transport used by backup.

The initial routing is:

```text
local endpoint  -> ShellProvider
remote endpoint -> ShellProvider
```

The later optional routing may become:

```text
local endpoint  -> LibZFSProvider
remote endpoint -> ShellProvider
```

The public model and interfaces remain pure Go. Any libzfs implementation must
be isolated in an optional package or build-tagged implementation and must not
expose `zfs_handle_t`, `nvlist_t`, or a specific OpenZFS ABI to callers. Normal
Zelta builds must not require OpenZFS headers, libraries, or cgo.

## Cache Authority

An observation cache is an optimization, not a safety authority. It may serve
dashboard data, repeated read-only queries, and property metadata. Safety
decisions must use a fresh observation or an explicitly validated freshness
token. Cached state must not authorize send/receive, divergence handling,
rotate/revert, prune execution, or other destructive operations.

This preserves match-before-send and fail-safe behavior regardless of which
provider supplied the observation.
