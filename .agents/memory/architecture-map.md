# ModHub Architecture Map

> Auto-curated reference for fast onboarding without scanning the repo. Update via `node scripts/dev/project-map.mjs`.

## High-Level Stack
- **Backend (`backend/`)**: Go 1.22, Wails v2.9.2 (`desktop`), chi v5 (`httpapi`), `modernc.org/sqlite` (`storage`).
- **Frontend (`frontend/`)**: React 18.3, TypeScript 5.7, Vite 6, `@tanstack/react-virtual`, `dompurify`.
- **Entrypoints**:
  - `backend/main.go` — resolves data dir, serves desktop Wails mode or `--web` HTTP server (`httpapi.NewServer`).
  - `backend/internal/app/container.go` — DI container wiring DB, HTTPClient, Events, Icons, Downloads, Java, Instances, Mods, Content, MCVersions, Modloaders, Accounts, Launcher, Modpacks.
  - `frontend/src/main.tsx` -> `frontend/src/App.tsx` — main shell (`TitleBar`, `AppNavRail`, `Sidebar`, lazy `pages/*` & modals).

## Backend Package Map (`backend/internal/`)
| Package | Responsibility | Key Files |
| :--- | :--- | :--- |
| `app` | Core `Container`, config, caches, search/details orchestration | `container.go`, `services.go` |
| `httpapi` | `/api/*` HTTP routes, static UI fallback, localhost middleware | `router.go`, `middleware.go`, `static.go` |
| `desktop` | Wails App bindings, dir picker, Windows memory trim | `bridge.go`, `datadir.go`, `open_dir.go`, `trim_windows.go` |
| `domain` | Shared domain models, constants, normalization helpers | `models.go`, `constants.go`, `helpers.go` |
| `storage` | SQLite DB, migrations, atomic file writes, stores, LRU cache | `db.go`, `migrations.go`, `atomic.go`, `*_store.go` |
| `catalog` | Modrinth + CurseForge clients, merger, crossmatcher, icons, installed hash check | `searchengine.go`, `merger.go`, `modrinth.go`, `curseforge.go`, `installed.go` |
| `deps` | Dependency tree, quick-download resolution, jar manifest inspection | `engine.go`, `quickdownload.go`, `jarmanifest.go`, `trace.go` |
| `downloads` | Download queue, pause/resume/cancel/retry, diskspace checks | `manager.go`, `diskspace_windows.go` |
| `instances` | Profiles/instances, mods, resourcepacks, shaders, worlds CRUD | `manager.go`, `mods.go`, `content.go`, `paths.go` |
| `mcversion` | Minecraft version manifest, library rules, vanilla installer | `manifest.go`, `installer.go`, `rules.go` |
| `modloader` | Fabric, Forge, NeoForge, Quilt installers & verification | `manager.go`, `fabric.go`, `forge.go`, `neoforge.go`, `quilt.go` |
| `accounts` | Microsoft OAuth2 PKCE + Xbox/XSTS/MC auth, offline accounts, encrypted vault | `manager.go`, `oauth.go`, `xbox_mc.go`, `vault.go`, `offline.go` |
| `launcher` | Preflight, JVM/game args, natives, process lifecycle, logs, smart crash analyzer | `manager.go`, `preflight` logic, `crash_analyzer.go`, `logs.go` |
| `modpacks` | Modpack import/export/update diff + archive traversal guards | `manager.go`, `formats.go`, `export.go`, `security.go` |
| `packio` | Streamed pack ZIP export builder & URL validation | `packbuilder.go` |
| `runtime` | Java runtime detection, compatibility rules, Mojang JRE manifest install | `manager.go`, `compat.go`, `detector_windows.go`, `mojang_manifest.go` |
| `netx` | Hardened HTTP client, Windows proxy detection, URL/SSRF validation | `httpclient.go`, `urlsecurity.go`, `proxy_windows.go` |
| `security` | Path/name sanitization, OS secret encryption (`secret_windows.go`) | `sanitize.go`, `secret_windows.go` |
| `settings` | `config.json` & overrides load/save | `store.go` |
| `events` | In-process pub/sub event bus (`bus.go`) pushed to Wails/SSE | `bus.go` |

## Frontend Map (`frontend/src/`)
- **Pages (`pages/`)**: `HomePage.tsx`, `InstancesPage.tsx`, `DownloadsPage.tsx`, `AccountsPage.tsx`, `SettingsPage.tsx`
- **Services & Hooks**: `services/bridge.ts`, `api.ts`, `types.ts`, `hooks/useAccounts.ts`, `useDownloads.ts`, `useInstances.ts`, `useLauncher.ts`, `useDependencyTree.ts`, `useThemeSettings.ts`
- **Core UI Components (`components/`)**: `TitleBar.tsx`, `AppNavRail.tsx`, `Topbar.tsx`, `Sidebar.tsx`, `VirtualizedResults.tsx`, `ModCard.tsx`, `ModModal.tsx` (`modmodal/*`), `InstallToProfileModal.tsx`, `InstallVersionWizard.tsx`, `InstanceConsoleTab.tsx`, `JavaSettingsSection.tsx`, `ImportModpackModal.tsx`
- **Styles (`styles/`)**: `variables.css`, `layout.css`, `components.css`, `desktop-shell.css`, `index.css`
