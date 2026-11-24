# ModHub Backend Rules (Go 1.22)

- **Layering**: `internal/httpapi` and `internal/desktop` are thin transport adapters delegating to `internal/app.Container`. Keep domain/business logic in dedicated `internal/*` packages.
- **Persistence**: SQLite lives in `internal/storage` (`modernc.org/sqlite`); JSON config/state writes must go through atomic write helpers (`storage/atomic.go`).
- **Security**: Keep tokens encrypted in `accounts/vault.go` / `security/`; never return secrets, OAuth tokens, or CurseForge keys to HTTP responses, Wails events, or logs. Validate URLs via `netx/urlsecurity.go` and paths via `security/sanitize.go`.
- **Verification**: Prefer targeted package tests first (`go test ./internal/<pkg>`), then `go test ./...` when cross-package contracts change.
