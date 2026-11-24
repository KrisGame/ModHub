---
name: modhub-api-integrations
description: >-
  Maintain ModHub's external integrations: Modrinth API, CurseForge API, Mojang
  manifests, modloaders (Fabric/Forge/NeoForge/Quilt), and Microsoft OAuth PKCE.
---

# ModHub External API Integrations Skill

## When to Use
Use when modifying external HTTP clients, catalog fetchers, Minecraft/modloader manifests, Java runtime downloads, or Microsoft/Xbox/Minecraft authentication.

## Context Command
Run `node scripts/dev/context-pack.mjs --task accounts` or `--task catalog`.

## Critical Requirements
1. **Microsoft OAuth2 PKCE**: Implemented in `backend/internal/accounts/oauth.go` and `xbox_mc.go`. Always keep `http://localhost:<port>` redirect URI consistent between authorize and token requests, `S256` PKCE, `state` verification, no Client Secret, and default public Client ID handling in `settings`/`accounts`.
2. **CurseForge & Modrinth**: Use `netx.HTTPClient` (respects `ProxyURL` and rate/size caps). Never leak `CurseForgeAPIKey` into logs, errors, or cache snapshots.
3. **Version & Loader Manifests**: `mcversion/manifest.go` and `modloader/` (`fabric.go`, `forge.go`, `neoforge.go`, `quilt.go`) must cancel cleanly with `ctx` and support profile creation auto-provisioning without blocking non-Java downloads.
