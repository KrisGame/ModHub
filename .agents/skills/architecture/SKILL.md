---
name: architecture
description: >-
  Slash-command workflow (/architecture): Analyze ModHub module boundaries,
  Wails/HTTP API contracts, and architectural impact before major changes.
---

# /architecture

1. Read `.agents/memory/architecture-map.md`, `.agents/memory/decisions.md`, and `.agents/agents/architect.md`.
2. Load targeted domain files via `node scripts/dev/context-pack.mjs --task <domain>`.
3. Map affected Go packages (`backend/internal/*`), transport bindings (`httpapi/router.go` & `desktop/bridge.go`), and TypeScript callers (`bridge.ts`, `types.ts`, `hooks/`).
4. Produce a concise boundary & data-flow diagram + contract impact summary before any edits.
