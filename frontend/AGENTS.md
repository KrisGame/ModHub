# ModHub Frontend Rules (React 18 + TS + Vite)

- **Transport**: Use `src/services/bridge.ts` and `src/api.ts` for backend calls (supports both Wails IPC and HTTP `/api` mode). Keep API response types aligned in `src/types.ts`.
- **UI & Styling**: Reuse design tokens from `src/styles/variables.css`, `layout.css`, `components.css`, and `desktop-shell.css`. Preserve Mica/Windows desktop shell polish and accessible focus/keyboard support.
- **Hotspots**: `src/App.tsx` (~2165 lines) is large — read targeted ranges, and extract new non-trivial logic into `src/hooks/`, `src/components/`, or `src/utils/`.
- **Security & Perf**: Sanitize any rendered HTML (`dompurify`), avoid unbounded re-renders on virtualized lists (`@tanstack/react-virtual`), and verify with `npm run build` (`tsc --noEmit && vite build`).
