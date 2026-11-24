import fs from "node:fs";
import path from "node:path";
import { ROOT_DIR, estimateTokens, formatBytes, toPosix, walkFiles } from "./_lib.mjs";

const argv = process.argv.slice(2);
const listOnly = argv.includes("--list");
const withSnippets = argv.includes("--with-snippets");

function getArgValue(flag, fallback) {
  const idx = argv.indexOf(flag);
  if (idx !== -1 && idx + 1 < argv.length) return argv[idx + 1];
  return fallback;
}

const taskQuery = getArgValue("--task", "").trim().toLowerCase();
const maxBytes = Number.parseInt(getArgValue("--max-bytes", "10000"), 10) || 10000;

const manifestPath = path.join(ROOT_DIR, ".agents", "memory", "context-manifest.json");
if (!fs.existsSync(manifestPath)) {
  console.error("Missing .agents/memory/context-manifest.json");
  process.exit(1);
}

const manifest = JSON.parse(fs.readFileSync(manifestPath, "utf8"));
const domains = manifest.domains || {};

if (listOnly || !taskQuery) {
  console.log("Available context domains:");
  for (const [k, files] of Object.entries(domains)) {
    console.log(`  --task ${k.padEnd(14)} (${files.length} curated files)`);
  }
  console.log('Example: node scripts/dev/context-pack.mjs --task mods --with-snippets');
  process.exit(listOnly ? 0 : 1);
}

let matchedFiles = [];
const matchedDomains = [];
for (const [domain, files] of Object.entries(domains)) {
  if (domain === taskQuery || domain.includes(taskQuery) || taskQuery.includes(domain)) {
    matchedDomains.push(domain);
    matchedFiles.push(...files);
  }
}

if (matchedFiles.length === 0) {
  const allFiles = walkFiles(ROOT_DIR, { skipBinary: true });
  matchedFiles = allFiles
    .filter((f) => f.relPath.toLowerCase().includes(taskQuery))
    .slice(0, 12)
    .map((f) => f.relPath);
}

matchedFiles = [...new Set(matchedFiles)];

function extractOutline(relPath) {
  const fullPath = path.join(ROOT_DIR, relPath);
  if (!fs.existsSync(fullPath)) return { exists: false, lines: 0, bytes: 0, symbols: [] };
  const text = fs.readFileSync(fullPath, "utf8");
  const lines = text.split(/\r?\n/);
  const symbols = [];
  const ext = path.extname(relPath);
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    if (ext === ".go" && /^(func|type)\s+/.test(line)) {
      symbols.push(`L${i + 1}: ${line.replace(/\{.*$/, "").trim()}`);
    } else if ((ext === ".ts" || ext === ".tsx") && /^export\s+(async\s+)?(function|const|type|interface|class)\s+/.test(line)) {
      symbols.push(`L${i + 1}: ${line.replace(/\{.*$/, "").trim()}`);
    }
    if (symbols.length >= 16) break;
  }
  return {
    exists: true,
    lines: lines.length,
    bytes: Buffer.byteLength(text, "utf8"),
    symbols,
    head: withSnippets ? lines.slice(0, 28).join("\n") : "",
  };
}

let out = [];
out.push(`=== ModHub Context Pack: "${taskQuery}" ===`);
if (matchedDomains.length) out.push(`Matched domains: ${matchedDomains.join(", ")}`);
out.push(`Memory pointers: ${(manifest.alwaysReadMemory || []).join(", ")}`);
out.push("");

let totalOutlineBytes = 0;
for (const rel of matchedFiles) {
  const info = extractOutline(toPosix(rel));
  if (!info.exists) {
    out.push(`[MISSING] ${rel}`);
    continue;
  }
  const block = [
    `FILE: ${rel} (${info.lines} lines, ${formatBytes(info.bytes)})`,
    ...(info.symbols.length ? info.symbols.map((s) => `  ${s}`) : ["  (no top-level Go/TS exports detected)"]),
    ...(info.head ? ["  --- head preview ---", ...info.head.split("\n").map((l) => `  ${l}`)] : []),
    "",
  ].join("\n");

  if (totalOutlineBytes + Buffer.byteLength(block, "utf8") > maxBytes) {
    out.push(`...truncated at --max-bytes=${maxBytes}. Remaining files: ${matchedFiles.slice(matchedFiles.indexOf(rel)).join(", ")}`);
    break;
  }
  totalOutlineBytes += Buffer.byteLength(block, "utf8");
  out.push(block);
}

const finalStr = out.join("\n");
console.log(finalStr);
console.log(`Estimated pack tokens: ~${estimateTokens(Buffer.byteLength(finalStr, "utf8"))}`);
