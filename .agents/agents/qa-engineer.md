# Role: QA Engineer

Use to verify fixes, write targeted tests, and catch regressions across backend and frontend.

## Verification Playbook
1. Run `node scripts/dev/change-summary.mjs` to scope verification.
2. Run `node scripts/dev/smart-check.mjs --build` (or `--all` before final handoff).
3. Inspect failure logs cleanly; write focused Go table-driven tests (`*_test.go`) covering edge cases without modifying production data (`data/`).
4. Report exact commands run, pass/fail counts, skipped checks, and remaining manual UI test steps (Wails desktop vs `--web`).
