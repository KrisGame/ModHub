# ModHub Multi-Agent Roles

Antigravity spawns specialized subagents at runtime via `define_subagent` + `invoke_subagent` (not auto-loaded `.agents/agents/*.md`). Keep routine edits (1-3 files) single-agent to save tokens; load these role specs only when parallelizing or isolating review/QA:

| Role | File | Suggested Subagent Settings | Use When |
| :--- | :--- | :--- | :--- |
| **Architect** | `architect.md` | `EnableWriteTools: false` | Multi-package changes, schema/API design, refactoring `App.tsx`/`router.go` |
| **Developer** | `developer.md` | `EnableWriteTools: true` | Isolated backend or frontend track running alongside another track |
| **Reviewer** | `reviewer.md` | `EnableWriteTools: false` | Independent diff review, regression & security audit before completion |
| **QA Engineer** | `qa-engineer.md` | `EnableWriteTools: true` | Running checks, reproduction tests, desktop/web edge-case verification |
