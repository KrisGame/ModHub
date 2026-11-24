---
name: plan-task
description: >-
  Slash-command workflow (/plan-task): Create a focused, token-efficient
  implementation plan with checkpoints, exact file targets, and verification steps.
---

# /plan-task

1. Gather domain context with `node scripts/dev/context-pack.mjs --task <domain>`.
2. Identify exact files and line ranges; avoid loading full hotspots (`App.tsx`, `router.go`).
3. Create a pre-work safety snapshot: `node scripts/dev/git-checkpoint.mjs "pre-plan"`.
4. Output a short ordered plan:
   - Scope & non-goals
   - Exact backend/frontend files to touch
   - Contract parity & edge-case checks
   - Exact `smart-check.mjs` / `go test` / `npm run build` commands to verify.
