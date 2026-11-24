---
name: modhub-ui-review
description: >-
  Audit and refine ModHub's Windows desktop UI, catalog 4-column grid, popover
  filters, profile tabs, topbar, accounts button, and loading state transitions.
---

# ModHub UI Review Skill

## When to Use
Use when reviewing or changing ModHub UX/UI layouts, cards, navigation, or Windows desktop appearance.

## Key Checkpoints
1. **Desktop Frame & Topbar**: `TitleBar.tsx` displays clean logo and Mica styling without duplicating the main row; `Topbar.tsx` balances controls cleanly with no `Ctrl+K` overlay clutter or redundant sidebar promo blocks.
2. **Catalog & Filters**: 4-column card grid (`VirtualizedResults.tsx`, `ModCard.tsx`), full-bleed card artwork without squishing, no duplicate installed-checkmark over button states, and popover filters triggered cleanly without shifting layout.
3. **Profile Tabs**: Tabs follow `Mods -> Resourcepacks -> Shaders -> Worlds -> Launch Settings (including Java selection) -> Console`, clearing stale tab content immediately on switch.
4. **Accounts & Downloads**: Prominent Accounts entry above Home, immediate auth state refresh, auto-clearing/clean finished downloads without noisy extra catalog/Java CTA clutter.
