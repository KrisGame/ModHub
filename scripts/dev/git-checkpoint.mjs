import fs from "node:fs";
import path from "node:path";
import {
  CHECKPOINTS_DIR,
  ROOT_DIR,
  ensureDir,
  formatBytes,
  hasGit,
  runCmd,
  sha256File,
  walkFiles,
} from "./_lib.mjs";

const args = process.argv.slice(2);

if (args.includes("--list")) {
  if (!fs.existsSync(CHECKPOINTS_DIR)) {
    console.log("No local snapshots found yet.");
    process.exit(0);
  }
  const dirs = fs
    .readdirSync(CHECKPOINTS_DIR, { withFileTypes: true })
    .filter((d) => d.isDirectory())
    .map((d) => d.name)
    .sort()
    .reverse();
  console.log(`Local checkpoints (${dirs.length}) in .modhub-dev/checkpoints/:`);
  for (const d of dirs.slice(0, 15)) {
    const mPath = path.join(CHECKPOINTS_DIR, d, "manifest.json");
    if (fs.existsSync(mPath)) {
      const m = JSON.parse(fs.readFileSync(mPath, "utf8"));
      console.log(`  - ${d} | ${m.fileCount} files | label="${m.label}"`);
    }
  }
  process.exit(0);
}

const label = args.filter((a) => !a.startsWith("--")).join(" ").trim() || "manual";
const safeSlug = label.toLowerCase().replace(/[^a-z0-9_-]+/g, "-").replace(/^-|-$/g, "").slice(0, 32) || "snap";

if (hasGit(ROOT_DIR)) {
  const status = runCmd("git", ["status", "--porcelain"], ROOT_DIR);
  const tagName = `checkpoint-${new Date().toISOString().replace(/[:.]/g, "-")}-${safeSlug}`;
  console.log(`[Git detected] Creating non-destructive stash snapshot / status record (${tagName})...`);
  const createRes = runCmd("git", ["stash", "create", label], ROOT_DIR);
  const sha = (createRes.stdout || "").trim();
  if (sha) {
    runCmd("git", ["stash", "store", "-m", `modhub-checkpoint: ${label}`, sha], ROOT_DIR);
    console.log(`Git stash checkpoint stored: ${sha} ("${label}")`);
  } else {
    console.log(`Git working tree clean (${status.stdout.trim() ? "tracked clean" : "no tracked diffs"}); creating filesystem manifest anyway.`);
  }
}

const stamp = new Date().toISOString().replace(/[:.]/g, "-").slice(0, 19);
const id = `${stamp}_${safeSlug}`;
const snapDir = path.join(CHECKPOINTS_DIR, id);
const filesDir = path.join(snapDir, "files");
ensureDir(filesDir);

const files = walkFiles(ROOT_DIR, { skipBinary: true, skipSecrets: true });
const manifestFiles = {};
let totalBytes = 0;

for (const f of files) {
  const digest = sha256File(f.fullPath);
  manifestFiles[f.relPath] = { size: f.size, mtimeMs: Math.round(f.mtimeMs), sha256: digest };
  totalBytes += f.size;
  const backupPath = path.join(filesDir, f.relPath);
  ensureDir(path.dirname(backupPath));
  fs.copyFileSync(f.fullPath, backupPath);
}

const manifest = {
  id,
  label,
  createdAt: new Date().toISOString(),
  mode: hasGit(ROOT_DIR) ? "git+snapshot" : "snapshot-fallback (no git repo)",
  fileCount: files.length,
  totalBytes,
  files: manifestFiles,
};

fs.writeFileSync(path.join(snapDir, "manifest.json"), JSON.stringify(manifest, null, 2));
fs.writeFileSync(path.join(CHECKPOINTS_DIR, "latest.json"), JSON.stringify({ id, createdAt: manifest.createdAt, label }, null, 2));

console.log(`[OK] Checkpoint created: .modhub-dev/checkpoints/${id}`);
console.log(`     Mode: ${manifest.mode} | Files: ${files.length} | Size: ${formatBytes(totalBytes)}`);
