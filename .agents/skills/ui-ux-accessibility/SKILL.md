---
name: ui-ux-accessibility
description: >-
  Design and audit ModHub UI components for desktop Mica shell polish, layout
  stability, responsive grids, keyboard accessibility, and theme contrast.
---

# UI/UX & Accessibility Standards

## When to Use
Use when modifying layouts, modals, cards, filters, titlebars, or styles in `frontend/src/styles/` and `frontend/src/components/`.

## Rules
1. **Design Tokens**: Use CSS variables from `src/styles/variables.css` (`--bg-*`, `--text-*`, `--border-*`, `--accent-*`) so all theme presets (`dark`/`light` + accent/bg presets) stay consistent.
2. **Layout Stability**: Prevent scrollbar-induced horizontal jumps (`scrollbar-gutter` / reserved padding) around close/action buttons and avoid flash of stale tab content on view switches.
3. **Desktop Shell Polish**: Keep `TitleBar.tsx` and `desktop-shell.css` clean, non-duplicated, and correctly aligned with window controls and app logo.
4. **Media & Cards**: Mod/pack thumbnails should fill card media containers cleanly (`object-fit: cover`, explicit aspect ratio) without squishing; avoid redundant status badges when action buttons already convey state.
5. **Keyboard & A11y**: Support `Escape` to close popovers/modals, visible `:focus-visible` outlines, semantic `<button>` elements, and ARIA labels on icon-only controls.
