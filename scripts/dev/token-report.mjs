import fs from "node:fs";
import path from "node:path";
import { ROOT_DIR, estimateTokens, formatBytes, toPosix, walkFiles } from "./_lib.mjs";

const RULE_FILE_LIMIT = 24000;
const ALWAYS_RULES_BUDGET_TOKENS = 20000;

function measureFile(relPath) {
  const full = path.join(ROOT_DIR, relPath);
  if (!fs.existsSync(full)) return null;
  const content = fs.readFileSync(full, "utf8");
  const bytes = Buffer.byteLength(content, "utf8");
  return { relPath: toPosix(relPath), bytes, tokens: estimateTokens(bytes), lines: content.split(/\r?\n/).length };
}

const rulePaths = ["AGENTS.md", "backend/AGENTS.md", "frontend/AGENTS.md", "GEMINI.md"]
  .map(measureFile)
  .filter(Boolean);

const memoryFiles = walkFiles(path.join(ROOT_DIR, ".agents", "memory"), { baseDir: ROOT_DIR, skipSecrets: false })
  .map((f) => measureFile(f.relPath))
  .filter(Boolean);

const agentFiles = walkFiles(path.join(ROOT_DIR, ".agents", "agents"), { baseDir: ROOT_DIR, skipSecrets: false })
  .map((f) => measureFile(f.relPath))
  .filter(Boolean);

const skillsRoot = path.join(ROOT_DIR, ".agents", "skills");
const skillFiles = [];
let skillFrontmatterBytes = 0;
let skillTotalBytes = 0;

if (fs.existsSync(skillsRoot)) {
  for (const dir of fs.readdirSync(skillsRoot).sort()) {
    const p = path.join(skillsRoot, dir, "SKILL.md");
    if (!fs.existsSync(p)) continue;
    const raw = fs.readFileSync(p, "utf8");
    const bytes = Buffer.byteLength(raw, "utf8");
    const fmMatch = raw.match(/^---\r?\n[\s\S]*?\r?\n---/);
    const fmBytes = fmMatch ? Buffer.byteLength(fmMatch[0], "utf8") : bytes;
    skillFrontmatterBytes += fmBytes;
    skillTotalBytes += bytes;
    skillFiles.push({
      name: dir,
      bytes,
      tokens: estimateTokens(bytes),
      fmTokens: estimateTokens(fmBytes),
    });
  }
}

const alwaysRuleBytes = rulePaths.reduce((acc, r) => acc + r.bytes, 0);
const alwaysRuleTokens = estimateTokens(alwaysRuleBytes);
const memoryTokens = memoryFiles.reduce((acc, m) => acc + m.tokens, 0);

console.log("=== ModHub Antigravity Token & Context Report ===");
console.log(`Always-loaded directory rules (${rulePaths.length} files): ${formatBytes(alwaysRuleBytes)} (~${alwaysRuleTokens} tokens / ${ALWAYS_RULES_BUDGET_TOKENS} Antigravity budget)`);
for (const r of rulePaths) {
  const flag = r.bytes > RULE_FILE_LIMIT ? " [OVER 24KB LIMIT]" : r.bytes > 4000 ? " [CONSIDER TRIMMING]" : "";
  console.log(`  - ${r.relPath}: ${formatBytes(r.bytes)} (~${r.tokens} tok, ${r.lines} lines)${flag}`);
}

console.log(`\nSkills progressive disclosure (${skillFiles.length} skills):`);
console.log(`  - Frontmatter index cost (always visible): ${formatBytes(skillFrontmatterBytes)} (~${estimateTokens(skillFrontmatterBytes)} tokens)`);
console.log(`  - Full bodies combined (on-demand only):  ${formatBytes(skillTotalBytes)} (~${estimateTokens(skillTotalBytes)} tokens)`);
const largeSkills = skillFiles.filter((s) => s.bytes > 6000);
if (largeSkills.length) {
  console.log(`  - Large SKILL.md (>6KB): ${largeSkills.map((s) => `${s.name} (${formatBytes(s.bytes)})`).join(", ")}`);
}

console.log(`\nPersistent memory files (${memoryFiles.length}): ~${memoryTokens} tokens (read selectively via context-pack)`);
console.log(`Specialized role definitions (${agentFiles.length}): ${formatBytes(agentFiles.reduce((a, x) => a + x.bytes, 0))}`);
console.log(`\nStatus: ${(alwaysRuleTokens + estimateTokens(skillFrontmatterBytes)) < 3500 ? "HEALTHY (Low baseline context overhead)" : "REVIEW RECOMMENDED"}`);
