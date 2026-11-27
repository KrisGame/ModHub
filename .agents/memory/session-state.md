# Session State & Handoff

## Active Baseline
- **Project**: ModHub (`d:\modhub`, local Git initialized on `main`)
- **Last Completed Changes**:
  - Connected the Antigravity toolkit to OpenCode: 4 project subagents and 10 slash commands in `.opencode/`, reusing all 20 skills and the role specs in `.agents/`.
  - Cleaned profile UI in `InstancesPage.tsx`: removed `Открыть папку mods` buttons and duplicate bottom `Установить из каталога` buttons (kept one per tab).
  - Hardened dependency resolution & download flow (`domain/helpers.go`, `catalog/modrinth.go`, `catalog/curseforge.go`, `deps/jarmanifest.go`, `deps/engine.go`, `profileModInstall.ts`): Quilt/Fabric & NeoForge/Forge compat fallback, preference for downloadable URLs, Jar-in-Jar self-ID detection, non-blocking warnings for heuristic manifest/text misses, and graceful handling of already-installed/queued deps.
  - Pruned 10 orphaned frontend files + `UX_AUDIT_V25.md` and removed unused locals (`tsc --noUnusedLocals` clean).

## Next Recommended Actions When Resuming Any Task
1. Run `node scripts/dev/context-pack.mjs --task <domain>` for minimal context.
2. Check `.agents/memory/known-issues.md` before modifying hotspots (`App.tsx`, `router.go`).
3. Create a safety checkpoint before large refactors: `node scripts/dev/git-checkpoint.mjs "<label>"`.
4. After edits, validate via `node scripts/dev/smart-check.mjs` and update this file with brief deltas.
