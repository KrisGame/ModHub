---
name: checkpoint
description: >-
  Slash-command workflow (/checkpoint): Create a safe workspace snapshot (Git
  or SHA-256 fallback in .modhub-dev/) and print a concise change summary.
---

# /checkpoint

1. Run `node scripts/dev/git-checkpoint.mjs "<short-label>"`.
2. Run `node scripts/dev/change-summary.mjs` to inspect the delta since the prior checkpoint.
3. Confirm `.modhub-dev/checkpoints/latest.json` was updated without touching `data/` runtime secrets.
