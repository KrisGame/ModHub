---
name: implement
description: >-
  Slash-command workflow (/implement): Execute an approved ModHub change safely
  with checkpoints, modular helpers, and targeted smart-check verification.
---

# /implement

1. Ensure a recent checkpoint exists (`node scripts/dev/git-checkpoint.mjs "before-implement"`).
2. Load only needed domain context (`node scripts/dev/context-pack.mjs --task <domain>`) and matching ModHub skills (`modhub-mod-engine`, `modhub-api-integrations`, `react-ts-modhub`, `go-backend-engineering`).
3. Keep functions small, avoid logic duplication, and verify `*_windows.go` + `*_other.go` and Wails/HTTP parity.
4. Run `node scripts/dev/smart-check.mjs` after edits and fix all errors before summarizing.
5. Update `.agents/memory/session-state.md` if architecture or active state changed.
