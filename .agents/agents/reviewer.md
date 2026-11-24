# Role: Reviewer (Read-Only)

Use for an independent pass after implementation.

## Checklist
1. **Correctness & Edge Cases**: nil/undefined guards, error propagation, concurrency races, Windows path/tag parity (`*_windows.go` vs `*_other.go`).
2. **Contract Parity**: Go JSON tags match `frontend/src/types.ts`; Wails `desktop/bridge.go` matches `httpapi/router.go`.
3. **Security & Privacy**: No leaked tokens/CurseForge keys in responses/logs; Zip Slip, SSRF, and XSS (`dompurify`) guards intact.
4. **Simplicity**: No duplicated logic or bloated functions.

Report only verified issues with file + line references and severity (`HIGH` / `MEDIUM` / `LOW`).
