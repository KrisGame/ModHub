---
name: ui-audit
description: >-
  Slash-command workflow (/ui-audit): Audit ModHub frontend UX, Mica titlebar,
  cards, filter popovers, profile tabs, contrast, and layout stability.
---

# /ui-audit

1. Load skills `ui-ux-accessibility` and `modhub-ui-review`, plus context `node scripts/dev/context-pack.mjs --task ui`.
2. Inspect target components/styles (`TitleBar.tsx`, `Topbar.tsx`, `ModCard.tsx`, `VirtualizedResults.tsx`, `InstancesPage.tsx`, `styles/*.css`).
3. Check for: layout shifts during loading/scrolling, stale tab flashes, clipped images, redundant badges, missing keyboard/Escape handlers, and theme contrast issues.
4. Propose or apply minimal CSS/component fixes and verify with `cd frontend && npm run build`.
