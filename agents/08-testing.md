# Testing

| Tier | Command | When |
|------|---------|------|
| Unit | `go test ./internal/<pkg>/...` | every tweak |
| Golden | `go test ./internal/match/ -run Golden` | output contracts |
| Build | `make build` | smoke |
| Integration | `go test -tags=integration ./...` | rare / pre-release |

## Rules

- Fake ZFS for unit/golden — no sudo  
- Table-driven tests  
- Shellspec from main zelta is optional cross-check later; scenario equivalence > identical install chrome  
- Never commit secrets or live SSH configs in fixtures  

## Shell tests

`make shelltest` runs `test/shell/basic_test.sh` — POSIX shell smoke tests that
verify the compiled binary on any platform (macOS, FreeBSD, Linux, etc.).
Tests cover version, usage, help/manpage routing, and unknown-verb errors.
Uses `ZELTA_BIN` env var (default `./bin/zelta`). No ZFS or root required.

## Cross-platform / sandbox testing

Use these env vars when running tests against a remote disposable host:

| Variable | Purpose | Example |
|----------|---------|---------|
| `SANDBOX_HOST` | SSH hostname for remote ZFS tests | `dev2` or `debian` |
| `SANDBOX_USER` | SSH user (default: root) | `djbell` |
| `SANDBOX_POOL` | Pool for file-backed test datasets | `apool` |
| `SANDBOX_BIN` | Path to pre-deployed binary on host | `/tmp/zelta` |
| `SANDBOX_SSH` | SSH command and options | `ssh -o StrictHostKeyChecking=no` |

The disposable lab hosts (`dev2`, `debian`, `netbsd`) are authorized for
destructive ZFS operations (see AGENTS.md).  To run the shell test suite
against a remote binary:

```sh
scp bin/zelta $SANDBOX_HOST:/tmp/
SANDBOX_HOST=dev2 SANDBOX_BIN=/tmp/zelta sh test/shell/basic_test.sh
```

For integration tests (requires ZFS on the remote host):

```sh
go test -tags=integration -run Backup ./internal/backup/
```

Note: current `-tags=integration` tests run locally and expect `apool`/`bpool`;
remote execution requires additional setup (use `go test -run` with a
specialized test file).
