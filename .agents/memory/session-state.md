# Session State & Handoff

## Active Baseline
- **Project**: ModHub (`d:\modhub`, local Git initialized on `main`)
- **Last Completed Changes**:
  - Audit remediation: staged modpack updates with rollback and atomic SQLite metadata; browser archive upload and retryable same-origin exports; bounded world export; accessible import/export dialogs, web profile progress, and browser section history. Index status and CurseForge key handling now distinguish errors from successful results.
  - Fixed the export dialog opening from profile details via a body portal. Verified in Chromium that it opens over the profile, fits a 380px-high area with internal scrolling and visible actions, closes on Escape, and does not appear in the profile list.
  - Fixed audit findings: ModHub/world exports now download in web mode through constrained same-origin endpoints; temporary files removed after transfer, export formats report/omit unsupported components, and world ZIP generation is bounded and atomic.
  - Added non-Windows AES-GCM config-secret protection under per-data-dir vault, strict save failure on broken ciphertext, and one-time export download tests. Added Settings UI for index sync and modpack update checks/actions.
  - Verification: `go test ./...` (85 passed), `go vet ./...`, targeted `smart-check` (Go suites + TS typecheck), and `npm run build` passed. One full frontend build preceded final backend-only secret edge hardening; no frontend code changed after it.
  - Connected the Antigravity toolkit to OpenCode: 4 project subagents and 10 slash commands in `.opencode/`, reusing all 20 skills and the role specs in `.agents/`.
  - Added the `/modhub-task` pipeline: global GPT-6 Luna orchestrator plans and triages; scoped GPT-6 Sol developer implements; project architect/reviewer are Luna, checks are risk-based; no Astra. Global OpenCode config defaults to the ModHub Luna orchestrator.
  - `/modhub-task` is optional: new ModHub sessions use the global Luna orchestrator automatically; pipeline instructions also require ordinary chat requests to follow the same workflow and classify advice-only questions as analysis-only.
  - Cleaned profile UI in `InstancesPage.tsx`: removed `Открыть папку mods` buttons and duplicate bottom `Установить из каталога` buttons (kept one per tab).
  - Hardened dependency resolution & download flow (`domain/helpers.go`, `catalog/modrinth.go`, `catalog/curseforge.go`, `deps/jarmanifest.go`, `deps/engine.go`, `profileModInstall.ts`): Quilt/Fabric & NeoForge/Forge compat fallback, preference for downloadable URLs, Jar-in-Jar self-ID detection, non-blocking warnings for heuristic manifest/text misses, and graceful handling of already-installed/queued deps.
  - Pruned 10 orphaned frontend files + `UX_AUDIT_V25.md` and removed unused locals (`tsc --noUnusedLocals` clean).

## Next Recommended Actions When Resuming Any Task
1. Run `node scripts/dev/context-pack.mjs --task <domain>` for minimal context.
2. Check `.agents/memory/known-issues.md` before modifying hotspots (`App.tsx`, `router.go`).
3. Create a safety checkpoint before large refactors: `node scripts/dev/git-checkpoint.mjs "<label>"`.
4. After edits, validate via `node scripts/dev/smart-check.mjs` and update this file with brief deltas.
