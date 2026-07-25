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
