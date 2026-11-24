import fs from "node:fs";
import path from "node:path";
import { ROOT_DIR, getLatestCheckpoint, hasGit, runCmd, sha256File, walkFiles } from "./_lib.mjs";

const rawArgs = process.argv.slice(2);
const runAll = rawArgs.includes("--all");
const runBuild = rawArgs.includes("--build");
const fileArgs = rawArgs.filter((a) => !a.startsWith("--"));

function detectChangedFiles() {
  if (fileArgs.length > 0) return fileArgs.map((f) => f.replaceAll("\\", "/"));

  if (hasGit(ROOT_DIR)) {
    const res = runCmd("git", ["status", "--porcelain"], ROOT_DIR);
    if (res.status === 0) {
      return res.stdout
        .split(/\r?\n/)
        .map((l) => l.slice(3).trim().replaceAll("\\", "/"))
        .filter(Boolean);
    }
  }

  const latest = getLatestCheckpoint();
  if (latest && latest.files) {
    const current = walkFiles(ROOT_DIR, { skipBinary: true });
    const changed = [];
    for (const f of current) {
      const prev = latest.files[f.relPath];
      if (!prev || prev.size !== f.size || prev.sha256 !== sha256File(f.fullPath)) {
        changed.push(f.relPath);
      }
    }
    return changed;
  }
  return [];
}

function validateAgentsToolkit() {
  let ok = true;
  const skillsDir = path.join(ROOT_DIR, ".agents", "skills");
  if (fs.existsSync(skillsDir)) {
    for (const entry of fs.readdirSync(skillsDir)) {
      const skillFile = path.join(skillsDir, entry, "SKILL.md");
      if (!fs.existsSync(skillFile)) continue;
      const content = fs.readFileSync(skillFile, "utf8");
      const fm = content.match(/^---\r?\n([\s\S]*?)\r?\n---/);
      if (!fm || !/^name:\s*\S+/m.test(fm[1]) || !/^description:\s*\S+/m.test(fm[1])) {
        console.error(`[FAIL] Invalid SKILL.md frontmatter: ${skillFile}`);
        ok = false;
      }
    }
  }
  const scriptsDir = path.join(ROOT_DIR, "scripts", "dev");
  if (fs.existsSync(scriptsDir)) {
    for (const f of fs.readdirSync(scriptsDir).filter((x) => x.endsWith(".mjs"))) {
      const res = runCmd("node", ["--check", path.join(scriptsDir, f)], ROOT_DIR);
      if (res.status !== 0) {
        console.error(`[FAIL] Syntax error in scripts/dev/${f}: ${res.stderr}`);
        ok = false;
      }
    }
  }
  if (ok) console.log("[PASS] Toolkit validation (.agents/skills + scripts/dev/*.mjs)");
  return ok;
}

const changed = detectChangedFiles();
console.log(`Smart Check Scope (${runAll ? "ALL" : changed.length ? `${changed.length} changed file(s)` : "toolkit + baseline"}):`);
if (changed.length > 0 && changed.length <= 20) {
  for (const f of changed) console.log(`  - ${f}`);
}

const goPkgs = new Set();
let needFrontend = runAll;
let needToolkit = runAll || changed.length === 0;

for (const f of changed) {
  if (f.startsWith("backend/internal/")) {
    const parts = f.split("/");
    if (parts[2]) goPkgs.add(`./internal/${parts[2]}`);
  } else if (f.startsWith("backend/")) {
    goPkgs.add("./...");
  } else if (f.startsWith("frontend/")) {
    needFrontend = true;
  } else if (f.startsWith(".agents/") || f.startsWith("scripts/dev/") || f.endsWith("AGENTS.md")) {
    needToolkit = true;
  }
}

let allPassed = true;

if (needToolkit) {
  if (!validateAgentsToolkit()) allPassed = false;
}

if (runAll || goPkgs.size > 0) {
  const targetPkgs = runAll ? ["./..."] : [...goPkgs];
  console.log(`Running Go tests: go test ${targetPkgs.join(" ")}`);
  const res = runCmd("go", ["test", ...targetPkgs], path.join(ROOT_DIR, "backend"));
  if (res.stdout) process.stdout.write(res.stdout);
  if (res.stderr) process.stderr.write(res.stderr);
  if (res.status !== 0) allPassed = false;
} else {
  console.log("[SKIP] Go tests (no backend changes detected; pass --all to force)");
}

if (needFrontend) {
  const npmCmd = process.platform === "win32" ? "npm.cmd" : "npm";
  const npxCmd = process.platform === "win32" ? "npx.cmd" : "npx";
  if (runBuild || runAll) {
    console.log("Running frontend build (tsc --noEmit && vite build)...");
    const res = runCmd(npmCmd, ["run", "build"], path.join(ROOT_DIR, "frontend"));
    if (res.error) console.error(res.error.message);
    if (res.stdout) process.stdout.write(res.stdout);
    if (res.stderr) process.stderr.write(res.stderr);
    if (res.status !== 0) allPassed = false;
  } else {
    console.log("Running frontend typecheck (npx tsc --noEmit)...");
    const res = runCmd(npxCmd, ["tsc", "--noEmit"], path.join(ROOT_DIR, "frontend"));
    if (res.error) console.error(res.error.message);
    if (res.stdout) process.stdout.write(res.stdout);
    if (res.stderr) process.stderr.write(res.stderr);
    if (res.status !== 0) allPassed = false;
  }
} else {
  console.log("[SKIP] Frontend check (no frontend changes detected; pass --all to force)");
}

process.exit(allPassed ? 0 : 1);
