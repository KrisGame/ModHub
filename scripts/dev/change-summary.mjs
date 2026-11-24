import { ROOT_DIR, getLatestCheckpoint, hasGit, runCmd, sha256File, walkFiles } from "./_lib.mjs";

const HIGH_RISK = new Set([
  "frontend/src/App.tsx",
  "backend/internal/httpapi/router.go",
  "backend/internal/accounts/oauth.go",
  "backend/internal/accounts/vault.go",
  "backend/internal/security/sanitize.go",
  "backend/internal/netx/urlsecurity.go",
]);

if (hasGit(ROOT_DIR)) {
  const st = runCmd("git", ["status", "--short"], ROOT_DIR);
  const diff = runCmd("git", ["diff", "--stat"], ROOT_DIR);
  console.log("=== Git Change Summary ===");
  console.log(st.stdout.trim() || "(clean working tree)");
  if (diff.stdout.trim()) console.log("\nDiff stats:\n" + diff.stdout.trim());
  process.exit(0);
}

const latest = getLatestCheckpoint();
if (!latest || !latest.files) {
  console.log("No Git repo and no baseline checkpoint found.");
  console.log('Run: node scripts/dev/git-checkpoint.mjs "baseline" first.');
  process.exit(0);
}

const currentFiles = walkFiles(ROOT_DIR, { skipBinary: true, skipSecrets: true });
const currentMap = new Map(currentFiles.map((f) => [f.relPath, f]));

const added = [];
const modified = [];
const deleted = [];
const riskHits = [];

for (const [rel, f] of currentMap.entries()) {
  const prev = latest.files[rel];
  if (!prev) {
    added.push(rel);
  } else if (prev.size !== f.size || prev.sha256 !== sha256File(f.fullPath)) {
    modified.push(rel);
    if (HIGH_RISK.has(rel)) riskHits.push(rel);
  }
}

for (const rel of Object.keys(latest.files)) {
  if (!currentMap.has(rel)) deleted.push(rel);
}

console.log(`=== Change Summary vs Checkpoint ${latest.id} ("${latest.label}") ===`);
console.log(`Added: ${added.length} | Modified: ${modified.length} | Deleted: ${deleted.length}`);
if (added.length) console.log("Added:\n" + added.map((x) => `  + ${x}`).join("\n"));
if (modified.length) console.log("Modified:\n" + modified.map((x) => `  ~ ${x}`).join("\n"));
if (deleted.length) console.log("Deleted:\n" + deleted.map((x) => `  - ${x}`).join("\n"));
if (riskHits.length) console.log(`High-risk hotspots modified: ${riskHits.join(", ")}`);
if (!added.length && !modified.length && !deleted.length) {
  console.log("No changes since latest checkpoint.");
}
