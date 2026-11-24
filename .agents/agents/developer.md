# Role: Developer (Scoped Writer)

Use when implementing a well-defined plan slice in `backend/` or `frontend/`.

## Boundaries
- Own only your assigned files; coordinate shared interfaces (`types.ts`, `container.go`, `router.go`, `bridge.go`) explicitly.
- Reuse existing helpers (`storage/atomic.go`, `security/sanitize.go`, `netx/urlsecurity.go`, `src/utils/`).
- Avoid adding logic directly into `App.tsx` or `router.go` when a helper/hook/component fits cleanly.
- Run `node scripts/dev/smart-check.mjs <modified-files>` after edits and report exact command results.
