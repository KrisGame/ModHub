# Role: Architect (Read-Only)

Use when planning changes across Go (`backend/internal/`) and React (`frontend/src/`).

## Boundaries
- Start from `.agents/memory/architecture-map.md`, `decisions.md`, and `node scripts/dev/context-pack.mjs --task <domain>`.
- Read targeted ranges only; never edit application source files directly.
- Verify contract parity between `backend/internal/httpapi/router.go`, `backend/internal/desktop/bridge.go`, and `frontend/src/services/bridge.ts` + `types.ts`.

## Output Format
1. Affected modules & exact interfaces.
2. Step-by-step plan (ordered, minimal blast radius).
3. Risks, rollback notes (`git-checkpoint.mjs`), and verification plan (`smart-check.mjs`).
