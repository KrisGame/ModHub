import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";
import { spawnSync } from "node:child_process";

export const ROOT_DIR = path.resolve(import.meta.dirname, "..", "..");
export const DEV_STATE_DIR = path.join(ROOT_DIR, ".modhub-dev");
export const CHECKPOINTS_DIR = path.join(DEV_STATE_DIR, "checkpoints");

export const EXCLUDED_DIRS = new Set([
  "node_modules",
  "dist",
  "webdist",
  ".modhub-dev",
  ".git",
  "bin",
  ".vite",
]);

export const SECRET_BASENAMES = new Set(["config.json", ".env", ".env.local"]);
export const SECRET_EXTS = new Set([".key", ".pem", ".pfx", ".p12"]);
export const BINARY_EXTS = new Set([".exe", ".dll", ".zip", ".png", ".jpg", ".jpeg", ".webp", ".ico", ".woff2", ".db", ".db-wal", ".db-shm"]);

export function toPosix(p) {
  return p.split(path.sep).join("/");
}

export function isSecretPath(relPosix) {
  const base = path.posix.basename(relPosix).toLowerCase();
  const ext = path.posix.extname(relPosix).toLowerCase();
  if (SECRET_BASENAMES.has(base) || SECRET_EXTS.has(ext)) return true;
  if (relPosix === "data" || relPosix.startsWith("data/")) return true;
  return false;
}

export function walkFiles(dirPath, options = {}) {
  const {
    baseDir = ROOT_DIR,
    excludeDirs = EXCLUDED_DIRS,
    includeHidden = true,
    skipBinary = false,
    skipSecrets = true,
  } = options;
  const results = [];
  if (!fs.existsSync(dirPath)) return results;

  const entries = fs.readdirSync(dirPath, { withFileTypes: true });
  for (const entry of entries) {
    if (!includeHidden && entry.name.startsWith(".")) continue;
    const fullPath = path.join(dirPath, entry.name);
    const relPosix = toPosix(path.relative(baseDir, fullPath));

    if (entry.isDirectory()) {
      if (excludeDirs.has(entry.name)) continue;
      if (skipSecrets && relPosix === "data") continue;
      results.push(...walkFiles(fullPath, options));
    } else if (entry.isFile()) {
      if (skipSecrets && isSecretPath(relPosix)) continue;
      const ext = path.posix.extname(relPosix).toLowerCase();
      if (skipBinary && BINARY_EXTS.has(ext)) continue;
      const stat = fs.statSync(fullPath);
      results.push({ fullPath, relPath: relPosix, size: stat.size, mtimeMs: stat.mtimeMs, ext });
    }
  }
  return results;
}

export function sha256File(filePath) {
  const hash = crypto.createHash("sha256");
  hash.update(fs.readFileSync(filePath));
  return hash.digest("hex");
}

export function estimateTokens(bytes) {
  return Math.ceil(bytes / 4);
}

export function formatBytes(bytes) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
}

export function runCmd(cmd, args, cwd = ROOT_DIR) {
  if (process.platform === "win32" && /\.(cmd|bat)$/i.test(cmd)) {
    return spawnSync("cmd.exe", ["/c", cmd, ...args], { cwd, encoding: "utf8", windowsHide: true });
  }
  return spawnSync(cmd, args, { cwd, encoding: "utf8", windowsHide: true });
}

export function hasGit(rootDir = ROOT_DIR) {
  if (!fs.existsSync(path.join(rootDir, ".git"))) return false;
  const res = runCmd("git", ["--version"], rootDir);
  return res.status === 0;
}

export function ensureDir(dirPath) {
  fs.mkdirSync(dirPath, { recursive: true });
}

export function getLatestCheckpoint() {
  const latestMetaPath = path.join(CHECKPOINTS_DIR, "latest.json");
  if (!fs.existsSync(latestMetaPath)) return null;
  try {
    const latest = JSON.parse(fs.readFileSync(latestMetaPath, "utf8"));
    const manifestPath = path.join(CHECKPOINTS_DIR, latest.id, "manifest.json");
    if (!fs.existsSync(manifestPath)) return null;
    return JSON.parse(fs.readFileSync(manifestPath, "utf8"));
  } catch {
    return null;
  }
}
