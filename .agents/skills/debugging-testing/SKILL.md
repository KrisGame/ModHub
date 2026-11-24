---
name: debugging-testing
description: >-
  Investigate bugs systematically with root-cause tracing, minimal repros, and
  targeted Go and TypeScript verification before full builds.
---

# Debugging & Testing Discipline

## When to Use
Use when diagnosing runtime crashes, OAuth errors, mod install bugs, download failures, or UI regressions.

## Workflow
1. **Scope First**: Run `node scripts/dev/context-pack.mjs --task <domain>` to locate exact files instead of guessing.
2. **Reproduce**: Trace backend -> transport (`bridge.ts` / `router.go`) -> frontend state (`hooks/`) before changing code.
3. **Add Regression Tests**: Write focused Go `*_test.go` cases (table-driven, temporary dirs via `t.TempDir()`, no side effects on `d:\modhub\data`).
4. **Targeted Check**: Run `node scripts/dev/smart-check.mjs` first; run full `go test ./...` and `npm run build` before closing non-trivial changes.
5. **Cleanup**: Never leave temporary logs or `.tmp` artifacts behind (`node scripts/dev/cleanup.mjs`).
