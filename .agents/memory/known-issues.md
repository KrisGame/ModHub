# Known Issues & Hotspots

## Code Hotspots (Navigate by Symbol, Avoid Full Reads)
- `frontend/src/App.tsx` (~2165 lines): Main state hub & modal wiring. Always locate functions/hooks before reading; prefer extracting new UI logic into `src/components/` or `src/hooks/`.
- `backend/internal/httpapi/router.go` (~1664 lines): All `/api/*` handlers in one file. Search endpoint path first with `findstr` before reading a handler slice.
- `frontend/src/types.ts` (~1254 lines): Shared DTOs; grep specific interface names instead of reading the whole file.
- `backend/internal/app/container.go` (~668 lines): Central container methods; keep changes backward-compatible with `desktop/bridge.go` and `httpapi/router.go`.

## Environment Constraints
- **Git Initialized**: Local Git repo (`D:/modhub/.git`, branch `main`) is active via `git version 2.56.1.windows.1`; `scripts/dev/git-checkpoint.mjs` and `change-summary.mjs` use native Git alongside optional `.modhub-dev/checkpoints/` backups.
- **No `agy` CLI / `gh` CLI on `PATH`**: Skills and rules live directly in workspace `.agents/` and `AGENTS.md` so Antigravity loads them natively.
- **Windows-Specific Files**: `*_windows.go` files (`detector_windows.go`, `secret_windows.go`, `trim_windows.go`, `proxy_windows.go`, `process_windows.go`, `diskspace_windows.go`, `sysinfo_windows.go`) carry Windows build tags; inspect counterpart `*_other.go` when altering signatures.
