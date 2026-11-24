# ModHub Workspace Rules

ModHub is a hybrid desktop & web Minecraft mod/pack/instance launcher (`backend/` Go 1.22 + Wails v2 + chi + SQLite; `frontend/` React 18 + TypeScript 5.7 + Vite 6).

## Token & Context Discipline
- **Map before reading**: Read `.agents/memory/architecture-map.md` or run `node scripts/dev/context-pack.mjs --task <domain>` before searching directories.
- **No blind full-file reads**: Locate symbols first (`findstr` / `rg`) and read bounded line ranges (`<=150` lines) unless a file is tiny.
- **Never ingest heavy/secret paths**: Skip `node_modules/`, `backend/webdist/`, `frontend/dist/`, `.modhub-dev/`, `modhub.exe`, and `data/config.json` (contains runtime settings/keys).
- **Compact output**: Give concise engineering answers and actionable diffs. Do not repeat file contents back to the user.
- **Selective verification**: Run `node scripts/dev/smart-check.mjs` after edits for targeted package/type checks; run full tests/builds once before finishing.
- **Subagent discipline**: Stay single-agent for routine edits (1-3 files). Spawn specialized roles (`.agents/agents/`) only for genuine parallel/isolated tracks (architecture, parallel implementation, independent review/QA).
- **Session continuity**: At task completion or major milestone, update `.agents/memory/session-state.md` (and `decisions.md` / `known-issues.md` if applicable). Never record API keys, tokens, Client IDs, or credentials in memory/docs.

## QOL Dev Scripts (`scripts/dev/`)
- `node scripts/dev/project-map.mjs` — refresh/print compact repo structure & dependency map
- `node scripts/dev/context-pack.mjs --task <topic>` — emit minimal token-bounded file context
- `node scripts/dev/smart-check.mjs [files...]` — run targeted Go + TS checks for changed scope
- `node scripts/dev/git-checkpoint.mjs [label]` — create safe checkpoint (Git or `.modhub-dev/checkpoints/` snapshot)
- `node scripts/dev/change-summary.mjs` — diff summary since last checkpoint
- `node scripts/dev/token-report.mjs` — audit `.agents/` context/token budget
- `node scripts/dev/cleanup.mjs` — dry-run scan for temp artifacts (`--confirm` required to delete)
