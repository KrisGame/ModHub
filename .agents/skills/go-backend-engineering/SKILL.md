---
name: go-backend-engineering
description: >-
  Apply idiomatic Go 1.22 backend architecture, error handling, concurrency,
  SQLite/atomic storage, and Windows compatibility patterns in backend/.
---

# Go Backend Engineering for ModHub

## When to Use
Use when editing or designing packages in `backend/internal/` or `backend/main.go`.

## Rules
1. **Thin Adapters**: Keep `httpapi` and `desktop` focused on request binding, status codes, and delegating to `app.Container` and `internal/*` managers.
2. **Context & Timeouts**: Thread `ctx context.Context` into outbound HTTP (`netx`), downloads, OAuth flows, and DB operations; handle `context.Canceled` cleanly without noisy errors.
3. **Safe Persistence**: Persist DB metadata via `storage` (`modernc.org/sqlite` parameterized queries) and JSON config/state via `storage/atomic.go`.
4. **Concurrency**: Protect shared state with explicit `sync.Mutex` / `sync.RWMutex` without holding locks across network I/O; drain/cancel background goroutines on container shutdown.
5. **OS Build Tags**: When editing `*_windows.go` files, verify counterpart `*_other.go` files keep matching exported symbols.

## Verify
Run `go test ./internal/<package>` first, then `go test ./...` when contracts change.
