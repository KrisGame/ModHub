---
name: modhub-mod-engine
description: >-
  Work on ModHub's mod dependency resolution, CurseForge/Modrinth cross-matching
  and merging, installed hash checks, profile mod installation, and modpacks.
---

# ModHub Mod & Pack Engine Skill

## When to Use
Use when modifying mod search, merger/overrides, dependency resolution, profile mod installs, or `.mrpack`/CurseForge modpack import/export.

## Context Command
Run `node scripts/dev/context-pack.mjs --task mods` or `--task modpacks` before editing.

## Domain Rules
1. **Cross-Platform Merger**: `backend/internal/catalog/merger.go` and `crossmatcher.go` unify Modrinth + CurseForge results using `overrides.json` merge/split rules; preserve source attribution (`platform`, `source_key`).
2. **Profile Compatibility**: Direct install-to-profile flows (`deps/quickdownload.go`, `profileModInstall.ts`) must validate Minecraft version, loader compatibility, and already-installed files/hashes (`catalog/installed.go`, `instances/mods.go`) to prevent duplicates.
3. **Dependencies**: Resolve required dependencies via `deps/engine.go` and `jarmanifest.go`; respect user `AutoInstallMediumDeps` settings.
4. **Modpack Safety**: Validate all archive entries (`modpacks/security.go`) and export URLs (`packio/packbuilder.go`) before downloading or writing files.
