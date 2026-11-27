# ModHub Multi-Agent Roles

Keep routine edits (1-3 files) single-agent; use these role specs only for parallel work or independent architecture, review, or QA.

- **OpenCode:** `.opencode/agents/` registers these four roles as subagents by file name. Ask OpenCode to use a role, for example `Use the qa-engineer subagent to verify this change`. `.opencode/commands/` registers the ten slash workflows from `.agents/skills/`; for example `/review`, `/plan-task <task>`, or `/test`. OpenCode discovers the original skills directly from `.agents/skills/`.
- **OpenCode automated pipeline:** `/modhub-task <task>` uses global primary agent `modhub-orchestrator` (**GPT-6 Luna**) for triage/planning and model-configured `architect`, `developer` (**GPT-6 Sol**), and `reviewer` subagents. Full Go/frontend verification runs once at the end; reviewers launch only for high-risk changes. No Astra.
- **Antigravity:** loads these role specs when spawning subagents via `define_subagent` + `invoke_subagent`.

| Role | File | Suggested Subagent Settings | Use When |
| :--- | :--- | :--- | :--- |
| **Architect** | `architect.md` | `EnableWriteTools: false` | Multi-package changes, schema/API design, refactoring `App.tsx`/`router.go` |
| **Developer** | `developer.md` | `EnableWriteTools: true` | Isolated backend or frontend track running alongside another track |
| **Reviewer** | `reviewer.md` | `EnableWriteTools: false` | Independent diff review, regression & security audit before completion |
| **QA Engineer** | `qa-engineer.md` | `EnableWriteTools: true` | Running checks, reproduction tests, desktop/web edge-case verification |
