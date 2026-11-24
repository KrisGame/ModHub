# Session State & Handoff

## Active Baseline
- **Project**: ModHub (`d:\modhub`, local Git initialized on `main`)
- **Toolkit Installed**:
  - Directory rules: `AGENTS.md`, `backend/AGENTS.md`, `frontend/AGENTS.md`
  - Memory & maps: `.agents/memory/architecture-map.md`, `context-manifest.json`, `decisions.md`, `known-issues.md`, `session-state.md`
  - Role specs: `.agents/agents/{architect,developer,reviewer,qa-engineer}.md` + `README.md`
  - Skills (20 total in `.agents/skills/`): 5 general engineering, 5 ModHub domain, 10 `/slash-command` workflows
  - QOL scripts: `scripts/dev/{_lib,project-map,context-pack,smart-check,git-checkpoint,change-summary,token-report,cleanup}.mjs`

## Next Recommended Actions When Resuming Any Task
1. Run `node scripts/dev/context-pack.mjs --task <domain>` for minimal context.
2. Check `.agents/memory/known-issues.md` before modifying hotspots (`App.tsx`, `router.go`).
3. Create a safety checkpoint before large refactors: `node scripts/dev/git-checkpoint.mjs "<label>"`.
4. After edits, validate via `node scripts/dev/smart-check.mjs` and update this file with brief deltas.
