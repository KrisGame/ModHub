# ModHub Architectural Decisions

1. **Hybrid Transport (`httpapi` + `desktop` Wails)**
   - Both Wails desktop IPC (`backend/internal/desktop/bridge.go`) and HTTP/chi (`backend/internal/httpapi/router.go`) delegate into `app.Container`.
   - Frontend calls go through `frontend/src/services/bridge.ts` so UI features work seamlessly in desktop WebView2 and `--web` mode.
2. **Encrypted Account Vault + PKCE Loopback OAuth**
   - Microsoft OAuth uses Authorization Code + PKCE (`S256`) with `localhost` loopback callback and encrypted persistence (`accounts/vault.go`, `security/secret_windows.go`). No Client Secret in desktop app.
3. **Persistence & Atomicity**
   - Metadata/indexes live in SQLite (`storage/db.go` via `modernc.org/sqlite`); JSON config/state writes use atomic write-rename semantics (`storage/atomic.go`).
4. **Hardened Networking & Archive Parsing**
   - Downloads and media fetches validate URLs (`netx/urlsecurity.go`) and enforce size limits (`netx/httpclient.go`). Modpack/ZIP extraction validates paths against Zip Slip traversal (`modpacks/security.go`).
5. **Skill-First Workflows**
   - Project workflows use modern `.agents/skills/<name>/SKILL.md` (giving `/slash-command` + semantic activation) instead of deprecated `.agents/workflows/*.md`.
