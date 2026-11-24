import fs from "node:fs";
import path from "node:path";
import { ROOT_DIR, DEV_STATE_DIR, ensureDir, walkFiles, toPosix } from "./_lib.mjs";

const args = new Set(process.argv.slice(2));
const compact = args.has("--compact");
const writeFile = args.has("--write");

function listDirFiles(relDir) {
  const full = path.join(ROOT_DIR, relDir);
  if (!fs.existsSync(full)) return [];
  return fs.readdirSync(full).sort();
}

function scanBackendPackages() {
  const internalDir = path.join(ROOT_DIR, "backend", "internal");
  if (!fs.existsSync(internalDir)) return [];
  const pkgs = [];
  for (const name of fs.readdirSync(internalDir).sort()) {
    const pkgDir = path.join(internalDir, name);
    if (!fs.statSync(pkgDir).isDirectory()) continue;
    const files = fs.readdirSync(pkgDir).filter((f) => f.endsWith(".go"));
    const testFiles = files.filter((f) => f.endsWith("_test.go"));
    const imports = new Set();
    for (const f of files) {
      const content = fs.readFileSync(path.join(pkgDir, f), "utf8");
      const matches = content.matchAll(/"modhub\/internal\/([^"]+)"/g);
      for (const m of matches) imports.add(m[1]);
    }
    pkgs.push({
      name,
      goFiles: files.length - testFiles.length,
      testFiles: testFiles.length,
      deps: [...imports].sort(),
    });
  }
  return pkgs;
}

function scanFrontendSummary() {
  const srcDir = path.join(ROOT_DIR, "frontend", "src");
  const groups = ["pages", "hooks", "components", "services", "utils", "styles"];
  const map = {};
  for (const g of groups) {
    const full = path.join(srcDir, g);
    map[g] = fs.existsSync(full) ? fs.readdirSync(full).sort() : [];
  }
  return map;
}

function scanAgentToolkit() {
  const skillsDir = path.join(ROOT_DIR, ".agents", "skills");
  const memoryDir = path.join(ROOT_DIR, ".agents", "memory");
  const agentsDir = path.join(ROOT_DIR, ".agents", "agents");
  return {
    skills: fs.existsSync(skillsDir) ? fs.readdirSync(skillsDir).sort() : [],
    memory: fs.existsSync(memoryDir) ? fs.readdirSync(memoryDir).sort() : [],
    agents: fs.existsSync(agentsDir) ? fs.readdirSync(agentsDir).sort() : [],
    scripts: listDirFiles("scripts/dev").filter((f) => f.endsWith(".mjs")),
  };
}

const backendPkgs = scanBackendPackages();
const frontend = scanFrontendSummary();
const toolkit = scanAgentToolkit();
const totalFiles = walkFiles(ROOT_DIR, { skipBinary: true }).length;

const report = {
  generatedAt: new Date().toISOString(),
  root: toPosix(ROOT_DIR),
  trackedSourceFiles: totalFiles,
  backendPackages: backendPkgs,
  frontend,
  toolkit,
};

if (writeFile) {
  ensureDir(DEV_STATE_DIR);
  const outPath = path.join(DEV_STATE_DIR, "project-map.json");
  fs.writeFileSync(outPath, JSON.stringify(report, null, 2));
}

console.log(`ModHub Project Map (${totalFiles} non-binary source/config files)`);
console.log(`Backend internal packages (${backendPkgs.length}):`);
for (const p of backendPkgs) {
  const depStr = compact ? "" : ` -> [${p.deps.join(", ")}]`;
  console.log(`  - ${p.name}: ${p.goFiles} go, ${p.testFiles} test${depStr}`);
}
console.log("Frontend src:");
for (const [group, items] of Object.entries(frontend)) {
  console.log(`  - ${group} (${items.length}): ${compact ? items.slice(0, 6).join(", ") + (items.length > 6 ? ", ..." : "") : items.join(", ")}`);
}
console.log(`Agent Toolkit: ${toolkit.skills.length} skills, ${toolkit.memory.length} memory files, ${toolkit.agents.length} roles, ${toolkit.scripts.length} dev scripts`);
if (writeFile) {
  console.log(`Saved map JSON -> .modhub-dev/project-map.json`);
}
