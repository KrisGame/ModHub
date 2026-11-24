# ModHub Launcher (Wails v2 + Go + SQLite FTS5 + React + TypeScript + Vite)

Полноценный настольный Minecraft Launcher и менеджер модификаций **Modrinth** + **CurseForge**:
- **Единый каталог и поиск** (Modrinth + CurseForge, склейка дублей, FTS5-индекс, трёхуровневый движок зависимостей, add-on discovery).
- **Download Manager** (очередь, пауза/резюмирование, ретраи, SHA-1/SHA-256, проверка свободного места).
- **Java Runtime Manager** (автопоиск, проверка совместимости, установка официальных рантаймов Mojang).
- **Instance Manager** (изолированные профили Minecraft, верификация и восстановление).
- **Minecraft Version Manager & Modloader Manager** (Vanilla, Fabric, Quilt, Forge, NeoForge).
- **Microsoft Account Manager** (OAuth 2.0 PKCE S256 → Xbox Live → XSTS → Minecraft Services, Windows DPAPI / AES-256-GCM vault).
- **Game Launcher** (сборка аргументов по реальным метаданным, natives с защитой от Zip Slip, мониторинг PID, живая консоль, crash-reports).
- **Modpack Manager** (импорт, экспорт и обновление `.mrpack`, CurseForge `.zip`, `.modhub`, Drag & Drop).
- **Инженерная модернизация (Principal Pass):** SQLite v12 (`foreign_keys=ON`, `PRAGMA optimize`, покрывающие индексы, устранение N+1), усиление HTTP/HTML-заголовков безопасности (`CSP`, `X-Frame-Options`, `Referrer-Policy`, `Permissions-Policy`, санитизация `writeErr`), TTL-очистка кэша, `React.lazy` code-splitting всех страниц и модалок, `ErrorBoundary` с автосбросом по разделам, полная клавиатурная и ARIA-доступность (`Escape`, `role="tab"`, `prefers-reduced-motion`).

## Установка, проверка и сборка (Windows)
```cmd
cd backend && go mod tidy && go vet ./... && go test ./...
cd ..\frontend && npm install && npm run build
cd .. && build.bat && start.bat
```
