---
name: security-audit
description: >-
  Slash-command workflow (/security-audit): Audit ModHub OAuth PKCE, secret
  vault, SSRF URL guards, archive Zip Slip safety, and XSS sanitization.
---

# /security-audit

1. Load `security-owasp-audit` and run `node scripts/dev/context-pack.mjs --task security`.
2. Verify all 5 trust boundaries:
   - `accounts/oauth.go` & `vault.go`: PKCE `S256`, `localhost` loopback exact URI match, `state` verification, no plaintext secrets in API/logs.
   - `netx/urlsecurity.go` & `httpclient.go`: download URL validation & size limits.
   - `modpacks/security.go` & `security/sanitize.go`: Zip Slip & path traversal checks.
   - `httpapi/middleware.go`: localhost API protections.
   - `frontend/`: `dompurify` around any external HTML.
3. Run `cd backend && go test ./internal/security ./internal/accounts ./internal/modpacks ./internal/netx ./internal/httpapi` and report findings.
