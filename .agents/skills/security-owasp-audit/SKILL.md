---
name: security-owasp-audit
description: >-
  Audit ModHub authentication, OAuth PKCE, secrets vaulting, SSRF/URL checks,
  archive extraction, and XSS sanitization against OWASP desktop/web risks.
---

# Security & OWASP Audit Skill

## When to Use
Use when touching `accounts/`, `security/`, `netx/`, `modpacks/`, `httpapi/middleware.go`, `packio/`, or frontend HTML rendering.

## Checklist
1. **Microsoft OAuth2 PKCE**: Keep `localhost` loopback redirect matching exactly between auth URL and token exchange, `S256` code challenge, strict `state` validation, and zero Client Secret in desktop code.
2. **Secret Hygiene**: Tokens stay encrypted via `accounts/vault.go` + `security/secret_windows.go`; API responses and logs must never expose access/refresh tokens or API keys.
3. **SSRF & Download Limits**: Validate remote URLs with `netx/urlsecurity.go` and enforce byte limits (`netx/httpclient.go`).
4. **Path Traversal / Zip Slip**: Validate instance names, mod filenames, and modpack archive entries with `security/sanitize.go` and `modpacks/security.go` before writing to disk.
5. **Web & Wails Surfaces**: Keep localhost origin/security checks intact in `httpapi/middleware.go`; sanitize third-party markdown/HTML with `dompurify`.
