---
name: review
description: >-
  Slash-command workflow (/review): Perform an independent code & regression
  review using .agents/agents/reviewer.md and change-summary.mjs.
---

# /review

1. Run `node scripts/dev/change-summary.mjs` to list every modified/added/deleted file and modified high-risk hotspots.
2. Inspect changed files against `.agents/agents/reviewer.md`:
   - Go/TS contract & JSON tag alignment
   - Error propagation & context cancellation
   - Secret/security invariants (`oauth.go`, `vault.go`, `sanitize.go`, `urlsecurity.go`)
   - Duplicate code or bloated additions to `App.tsx` / `router.go`
3. Run `node scripts/dev/smart-check.mjs --build` and report findings strictly by severity (`HIGH` / `MEDIUM` / `LOW`).
