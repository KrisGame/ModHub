import fs from "node:fs";
import path from "node:path";
import { CHECKPOINTS_DIR, ROOT_DIR, formatBytes, toPosix } from "./_lib.mjs";

const args = new Set(process.argv.slice(2));
const confirm = args.has("--confirm");
const includeOldCheckpoints = args.has("--prune-checkpoints");
const KEEP_CHECKPOINTS = 5;

const SAFE_TEMP_EXTS = new Set([".mhpart", ".tmp", ".bak", ".orig"]);
const SKIP_TOP = new Set(["node_modules", ".git", "data", ".agents", "scripts"]);

const candidates = [];

function scanTemp(dirPath) {
  if (!fs.existsSync(dirPath)) return;
  for (const entry of fs.readdirSync(dirPath, { withFileTypes: true })) {
    const full = path.join(dirPath, entry.name);
    const rel = toPosix(path.relative(ROOT_DIR, full));
    const top = rel.split("/")[0];
    if (entry.isDirectory()) {
      if (SKIP_TOP.has(top) || entry.name === "node_modules" || entry.name === ".modhub-dev") continue;
      scanTemp(full);
    } else if (entry.isFile()) {
      const ext = path.extname(entry.name).toLowerCase();
      if (SAFE_TEMP_EXTS.has(ext)) {
        candidates.push({ type: "temp-file", rel, full, size: fs.statSync(full).size });
      }
    }
  }
}

scanTemp(ROOT_DIR);

if (includeOldCheckpoints && fs.existsSync(CHECKPOINTS_DIR)) {
  const dirs = fs
    .readdirSync(CHECKPOINTS_DIR, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name)
    .sort()
    .reverse();
  for (const old of dirs.slice(KEEP_CHECKPOINTS)) {
    const full = path.join(CHECKPOINTS_DIR, old);
    candidates.push({ type: "old-checkpoint", rel: `.modhub-dev/checkpoints/${old}`, full, size: 0 });
  }
}

console.log(`=== ModHub Cleanup (${confirm ? "APPLY MODE" : "DRY-RUN"}) ===`);
if (candidates.length === 0) {
  console.log("Nothing to clean. Workspace is tidy.");
  process.exit(0);
}

let totalBytes = 0;
for (const c of candidates) {
  totalBytes += c.size;
  console.log(`  [${c.type}] ${c.rel} (${formatBytes(c.size)})`);
}

if (!confirm) {
  console.log(`\nFound ${candidates.length} candidate(s) (~${formatBytes(totalBytes)}).`);
  console.log("Dry-run only: nothing was deleted. Re-run with --confirm to delete.");
  process.exit(0);
}

let removed = 0;
for (const c of candidates) {
  fs.rmSync(c.full, { recursive: true, force: true });
  removed++;
}
console.log(`\nRemoved ${removed} item(s).`);
