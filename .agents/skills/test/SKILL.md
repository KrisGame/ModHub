---
name: test
description: >-
  Slash-command workflow (/test): Run smart targeted checks or full backend Go
  tests and frontend TypeScript/Vite build verification for ModHub.
---

# /test

1. Determine scope via `node scripts/dev/change-summary.mjs`.
2. Fast targeted pass: `node scripts/dev/smart-check.mjs --build`.
3. Full verification (when core contracts changed or before release):
   - Backend: `cd backend && go test ./...`
   - Frontend: `cd frontend && npm run build`
4. Report exact commands executed, pass/fail counts, skipped checks, and any failures verbatim.
