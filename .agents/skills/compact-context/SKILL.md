---
name: compact-context
description: >-
  Slash-command workflow (/compact-context): Audit token usage, trim stale
  session notes, update memory files safely without secrets, and suggest /compact.
---

# /compact-context

1. Run `node scripts/dev/token-report.mjs` and `node scripts/dev/cleanup.mjs` (dry-run only).
2. Update `.agents/memory/session-state.md` with concise bullet points of current progress, verified facts, and next steps (strictly zero keys/tokens/credentials).
3. Update `.agents/memory/decisions.md` or `known-issues.md` only if new permanent invariants emerged.
4. Recommend `/compact` to the user if the active chat conversation has grown long.
