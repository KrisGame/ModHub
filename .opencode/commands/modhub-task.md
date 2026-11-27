---
description: Run ModHub task pipeline: GPT-6 Luna plans, GPT-6 Sol implements, automatic checks, risk-based review.
agent: modhub-orchestrator
subagent: false
---

Execute the complete ModHub development pipeline for this request: $ARGUMENTS

## Pipeline

1. **Triage (Luna, current agent).** Treat `$ARGUMENTS` as the full user request. Restate the concrete outcome and non-goals internally. Classify scope as `small` (one to three files within one module), `cross-module` (multiple packages, frontend and backend, shared API/bridge/types, or database schema), `security` (OAuth, secrets, SSRF/network URL validation, archive extraction/path traversal, sanitization, or localhost API protection), or `analysis-only` (question, explanation, assessment, or plan without an explicit request to change files). Do not ask the user to write a prompt for the developer.
2. **Minimal context (Luna).** Run `node scripts/dev/context-pack.mjs --task <best matching domain>`, using a domain from the script's list. Read `.agents/memory/known-issues.md` when touching hotspots, and read only relevant ranges of listed files. For an unlisted topic, use the closest domain or `architecture`; do not dump the full repository.
3. **Plan (Luna).** For `analysis-only`, answer directly from the scoped context; do not checkpoint, edit, delegate implementation, test, commit, or push. For `small`, produce an internal 2–4-bullet plan; do not spawn architect. For `cross-module` or `security`, invoke the `architect` subagent once with the concise request, context-pack output and limits; request affected files/interfaces, plan, risks, and checks. Continue directly if its result is unavailable and report any uncertainty.
4. **Checkpoint.** Before implementation, run `node scripts/dev/git-checkpoint.mjs "before-modhub-task"`. Never reset, revert or overwrite existing user changes. If the checkpoint fails, inspect status and continue safely without destructive commands.
5. **Implement (Sol).** Invoke exactly one `developer` subagent, using the approved plan, concise relevant context, user constraints, and exact writable scope. Request the minimum implementation, targeted tests, and `node scripts/dev/smart-check.mjs <changed-files>`; developer must report changed paths, checks, and risks. Do not start multiple developers or delegate decomposition back to the developer. For a small task, still use the same developer to keep coding on Sol.
6. **Verify (Luna, shell).** Independently inspect `git status --short` and `git diff --check`. Run `node scripts/dev/smart-check.mjs` and, once before final handoff, run `node scripts/dev/smart-check.mjs --all` (full Go suite and frontend production build). Never claim tests passed unless their commands finish successfully. Preserve generated/runtime files excluded by repository rules; never commit build output, secrets or local data.
7. **Repair if needed.** On a failed check, send only its concise error and affected scope to the same `developer` child; retry the relevant check. Do not repeat exploration/planning unless failure reveals a real contract or scope gap. Stop after two repair attempts and report the remaining failure honestly.
8. **Independent review (Luna, on demand).** Invoke `reviewer` only for `security`, cross-module/API/schema changes, or a consequential feature. Give it the changed-file list, concise diff and verification results; request verified findings only. Skip reviewer for ordinary small fixes. Fix verified findings through the existing Sol developer, then rerun relevant checks.
9. **Finish.** Update `.agents/memory/session-state.md` for meaningful state/architecture changes, without secrets. Never stage pre-existing, untracked, ignored, generated, runtime, secret, or unrelated user changes. Follow project Git rules only when the intended diff is isolated and all relevant final checks pass: run `git add` on the explicit files you changed (never `git add -A`), inspect the staged diff, then commit with a concise Conventional Commit message and push to `origin/main`. If any check fails or any user changes overlap / cannot be confidently excluded, do not commit/push; report why and leave changes intact. Summarize outcome, changed scope, checks, review decision, and commit/push result briefly.

## Budget rules

- No Astra. Luna is the default/triage/architecture/review model; Sol is the only coding model in this pipeline.
- One architect at most, one developer at a time, and zero reviewers for a small change.
- Never send the whole conversation, full context pack, full build log, or repository dump to a subagent; include only the smallest useful excerpts and paths.
- Do not run full Go/frontend verification during planning, per-file edits, or repair iterations; run it once after final changes.
- If the request is analysis/planning only, do not write or launch the implementation pipeline unless the user explicitly asks for code changes. Treat regular chat messages the same way as this command; slash-command invocation is never required for an implementation request.
