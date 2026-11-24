---
name: modhub-qa
description: >-
  Execute ModHub verification across Go packages, TypeScript/Vite builds,
  launcher preflight/crash diagnostics, and desktop/web runtime parity checks.
---

# ModHub QA Verification Skill

## When to Use
Use when verifying ModHub changes, testing profile creation/launch flows, checking crash diagnosis, or validating release readiness.

## Verification Sequence
1. Inspect delta: `node scripts/dev/change-summary.mjs`.
2. Targeted test & typecheck: `node scripts/dev/smart-check.mjs --build`.
3. Full backend verification when touching `container.go`, `storage/`, or shared `domain/`:
   `cd backend && go test ./...`
4. Full frontend verification:
   `cd frontend && npm run build`
5. Manual desktop/web smoke checks for touched flows:
   - **Instances**: Create profile -> verify version/loader install -> mods/resourcepacks/shaders/worlds/settings/console tabs.
   - **Launcher & Java**: Preflight check, JVM runtime selection in profile settings, smart crash diagnosis output (`launcher/crash_analyzer.go`).
   - **Accounts & Downloads**: OAuth state updates UI immediately; finished downloads clean up reliably.
