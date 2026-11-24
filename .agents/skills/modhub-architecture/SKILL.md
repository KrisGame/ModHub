---
name: modhub-architecture
description: >-
  Navigate ModHub's Go backend container, Wails desktop bridge, Chi HTTP API,
  SQLite storage, and React frontend without redundant codebase scanning.
---

# ModHub Architecture Skill

## When to Use
Use when orienting on ModHub architecture, adding new features spanning backend and frontend, or reviewing module boundaries.

## Key Reference
Read [architecture-map.md](file:///d:/modhub/.agents/memory/architecture-map.md) and run `node scripts/dev/project-map.mjs --compact` if structure changed.

## Core Invariants
1. **Single Container Hub**: [container.go](file:///d:/modhub/backend/internal/app/container.go) owns all domain managers (`Accounts`, `Downloads`, `Instances`, `Mods`, `Content`, `MCVersions`, `Modloaders`, `Launcher`, `Modpacks`, `Java`).
2. **Dual Transport Parity**: Any user-facing backend capability exposed via [router.go](file:///d:/modhub/backend/internal/httpapi/router.go) must stay aligned with [bridge.go](file:///d:/modhub/backend/internal/desktop/bridge.go) and [bridge.ts](file:///d:/modhub/frontend/src/services/bridge.ts) when desktop Wails bindings exist.
3. **Events**: Backend real-time state updates publish through `events.Bus` and subscribe in frontend via `subscribeBackendEvents`.
4. **Data Directory**: Runtime user data (`data/`) is strictly separated from source code and excluded from context packs.
