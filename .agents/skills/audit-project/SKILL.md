---
name: audit-project
description: >-
  Slash-command workflow (/audit-project): Refresh ModHub's project map, token
  report, change summary, and known-issues health check with zero blind reads.
---

# /audit-project

1. Run `node scripts/dev/project-map.mjs --compact --write` to refresh `.modhub-dev/project-map.json`.
2. Run `node scripts/dev/token-report.mjs` and `node scripts/dev/change-summary.mjs`.
3. Read `.agents/memory/architecture-map.md`, `.agents/memory/known-issues.md`, and `.agents/memory/session-state.md`.
4. Summarize concisely: current architecture health, modified modules, hotspot warnings, and recommended next actions.
