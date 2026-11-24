---
name: react-ts-modhub
description: >-
  Apply React 18, strict TypeScript, and Vite performance patterns tailored to
  ModHub frontend components, hooks, virtualized lists, and bridge API calls.
---

# React & TypeScript Best Practices for ModHub

## When to Use
Use when creating or refactoring React components, custom hooks, state flows, or API bindings in `frontend/src/`.

## Rules
1. **Strict Types**: Align DTOs with Go JSON payloads in [types.ts](file:///d:/modhub/frontend/src/types.ts); avoid `any` and unsafe casts.
2. **Transport Layer**: Call backend via [bridge.ts](file:///d:/modhub/frontend/src/services/bridge.ts) or [api.ts](file:///d:/modhub/frontend/src/api.ts) so both Wails desktop IPC and HTTP `/api` work identically.
3. **Component Boundaries**: Keep `App.tsx` slim — extract domain state to `src/hooks/` and presentation pieces to `src/components/`.
4. **Async & Race Safety**: Guard `useEffect` async fetches with `AbortController` or request sequence tokens when switching tabs/profiles so stale results never flash over new views.
5. **List Performance & XSS**: Use `@tanstack/react-virtual` for long grids/logs (`VirtualizedResults.tsx`, `InstanceConsoleTab.tsx`) and `dompurify` before rendering external descriptions/changelogs.

## Verify
Run `npx tsc --noEmit` (or `npm run build`) inside `frontend/`.
