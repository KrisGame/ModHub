import { api } from "../api";
import type { CartItem, InstallInstanceModInput, ProjectCard, VersionFile } from "../types";

export interface InstallProfileModOptions {
  target: { id: string; name: string };
  project: ProjectCard;
  file: VersionFile;
  mcVersion: string;
  loader: string;
  body?: string;
}

export interface InstallProfileModResult {
  queued: number;
  dependencies: number;
  alreadyInstalled: number;
}

function sourceKeys(file: VersionFile): string[] {
  return [...new Set([file.source_key, `${file.platform}:${file.mod_id || ""}`, file.mod_id || ""].filter(Boolean))];
}

interface InstallableFile {
  title?: string;
  name?: string;
  platform?: string;
  folder?: string;
  filename: string;
  download_url: string;
  fallback_url?: string;
  file_size: number;
  mod_id?: string;
  version_id?: string;
}

function installInput(
  targetID: string,
  item: InstallableFile,
  groupID: string,
  titleFallback: string,
  category = "mod"
): InstallInstanceModInput | null {
  const low = item.filename?.toLowerCase() || "";
  if (category === "resourcepack" || category === "shader") {
    if (!low.endsWith(".zip") || !item.download_url) return null;
  } else {
    if (!low.endsWith(".jar") || !item.download_url) return null;
  }
  return {
    id: targetID,
    title: item.name || item.title || titleFallback,
    filename: item.filename,
    url: item.download_url,
    fallback_url: item.fallback_url,
    size: item.file_size,
    category,
    project_id: item.mod_id,
    version_id: item.version_id,
    group_id: groupID,
  };
}

function cartItemToInstallableFile(item: CartItem): InstallableFile {
  const sourceKey = item.source_key || "";
  const separator = sourceKey.indexOf(":");
  return {
    name: item.title,
    platform: item.platform,
    folder: item.folder,
    filename: item.filename,
    download_url: item.download_url,
    file_size: item.file_size,
    mod_id: separator >= 0 ? sourceKey.slice(separator + 1) : sourceKey,
  };
}

/** Determines whether a mod file's loader matches the target instance's loader. */
export function isLoaderCompatible(fileLoader: string, instanceType: string): boolean {
  const f = fileLoader.toLowerCase().trim();
  const inst = instanceType.toLowerCase().trim();
  if (!f || !inst) return false;
  if (f === inst) return true;
  // Quilt is backward compatible with Fabric mods
  if (inst === "quilt" && f === "fabric") return true;
  // NeoForge 1.20.1 and early builds can run Forge mods
  if (inst === "neoforge" && f === "forge") return true;
  return false;
}

/** Determines whether the version file supports the given Minecraft version. */
export function isVersionCompatible(gameVersions: string[], mcVersion: string): boolean {
  if (!gameVersions || gameVersions.length === 0) return true;
  const target = mcVersion.trim();
  if (!target) return true;
  return gameVersions.some((v) => {
    const vt = v.trim();
    if (vt === target) return true;
    if (vt.endsWith(".x") && target.startsWith(vt.slice(0, -2))) return true;
    return false;
  });
}

/**
 * CurseForge Launcher algorithm: Finds the latest, most stable release matching
 * the target Minecraft version and loader without requiring manual version selection.
 */
export function findBestCompatibleVersion(
  versions: VersionFile[],
  mcVersion: string,
  loader: string,
  contentType = "mod"
): VersionFile | null {
  if (!versions || versions.length === 0) return null;
  const isMod = contentType === "mod";
  const validFiles = versions.filter((v) => {
    const fn = (v.filename || "").toLowerCase();
    const isJar = fn.endsWith(".jar");
    const isZip = fn.endsWith(".zip");
    if (isMod && !isJar) return false;
    if (!isMod && !isZip && !isJar) return false;
    if (!v.download_url) return false;

    if (mcVersion && !isVersionCompatible(v.game_versions || [], mcVersion)) {
      return false;
    }

    if (isMod && loader && loader !== "vanilla") {
      const fileLoaders = (v.loaders || []).map((l) => l.toLowerCase());
      const hasCompatLoader = fileLoaders.some((fl) => isLoaderCompatible(fl, loader));
      if (!hasCompatLoader) return false;
    }

    return true;
  });

  if (validFiles.length === 0) return null;

  const releaseScore = (type: string) => {
    const t = (type || "").toLowerCase();
    if (t === "release") return 3;
    if (t === "beta") return 2;
    if (t === "alpha") return 1;
    return 0;
  };

  return [...validFiles].sort((a, b) => {
    const relDiff = releaseScore(b.release_type) - releaseScore(a.release_type);
    if (relDiff !== 0) return relDiff;

    if (isMod && loader && loader !== "vanilla") {
      const aExact = (a.loaders || []).map((l) => l.toLowerCase()).includes(loader.toLowerCase()) ? 1 : 0;
      const bExact = (b.loaders || []).map((l) => l.toLowerCase()).includes(loader.toLowerCase()) ? 1 : 0;
      if (bExact !== aExact) return bExact - aExact;
    }

    if (mcVersion) {
      const aExactMc = (a.game_versions || []).includes(mcVersion) ? 1 : 0;
      const bExactMc = (b.game_versions || []).includes(mcVersion) ? 1 : 0;
      if (bExactMc !== aExactMc) return bExactMc - aExactMc;
    }

    const bDate = Date.parse(b.date_published || "") || 0;
    const aDate = Date.parse(a.date_published || "") || 0;
    return bDate - aDate;
  })[0];
}

/** Evaluates compatibility between an instance and a project's available versions. */
export function checkInstanceModCompatibility(
  instance: { mc_version: string; instance_type: string },
  versions: VersionFile[],
  contentType = "mod"
): { compatible: boolean; bestVersion: VersionFile | null; reason?: string } {
  if (contentType === "mod" && instance.instance_type === "vanilla") {
    return {
      compatible: false,
      bestVersion: null,
      reason: "Vanilla не поддерживает моды (требуется Forge, Fabric или NeoForge)",
    };
  }

  const best = findBestCompatibleVersion(versions, instance.mc_version, instance.instance_type, contentType);
  if (!best) {
    if (contentType === "mod") {
      return {
        compatible: false,
        bestVersion: null,
        reason: `Нет версии для Minecraft ${instance.mc_version} (${instance.instance_type})`,
      };
    }
    return {
      compatible: false,
      bestVersion: null,
      reason: `Нет версии для Minecraft ${instance.mc_version}`,
    };
  }

  return {
    compatible: true,
    bestVersion: best,
  };
}

export interface AutoInstallOptions {
  instance: { id: string; name: string; mc_version: string; instance_type: string };
  project: ProjectCard;
  versions?: VersionFile[];
  body?: string;
  onToast?: (msg: string, type: "ok" | "err" | "info") => void;
}

/**
 * CurseForge Launcher 1-Click Auto Install:
 * Resolves the matching file, automatically satisfies required dependencies,
 * and queues download directly into the selected instance.
 */
export async function autoInstallModIntoInstance(options: AutoInstallOptions): Promise<InstallProfileModResult> {
  const { instance, project } = options;
  let versions = options.versions;

  if (!versions || versions.length === 0) {
    const cached = api.getCachedModDetails(project);
    if (cached?.versions && cached.versions.length > 0) {
      versions = cached.versions;
    } else {
      const det = await api.getModDetails(project);
      versions = det.versions || [];
    }
  }

  const contentType = (project.content_type || "mod").toLowerCase();
  const compat = checkInstanceModCompatibility(instance, versions, contentType);
  if (!compat.compatible || !compat.bestVersion) {
    throw new Error(
      compat.reason ||
        `Для «${project.title}» не найдено подходящей версии под ${instance.mc_version} (${instance.instance_type})`
    );
  }

  // Strict duplicate presence check against files already in the profile
  const knownFiles = new Set(
    versions
      .map((v) => (v.filename || "").toLowerCase())
      .filter(Boolean)
  );
  const slugLow = (project.slug || "").toLowerCase().trim();
  const matchesLocalFile = (rawName: string): boolean => {
    const clean = rawName.toLowerCase().replace(/\.disabled$/, "");
    if (clean === compat.bestVersion!.filename.toLowerCase() || knownFiles.has(clean)) {
      return true;
    }
    if (slugLow.length >= 3) {
      if (
        clean.startsWith(`${slugLow}-`) ||
        clean.startsWith(`${slugLow}_`) ||
        clean.startsWith(`${slugLow}+`) ||
        clean === `${slugLow}.jar` ||
        clean === `${slugLow}.zip`
      ) {
        return true;
      }
    }
    return false;
  };

  if (contentType === "resourcepack") {
    const packs = await api.listInstanceResourcePacks(instance.id).catch(() => []);
    if (packs.some((p) => matchesLocalFile(p.filename))) {
      throw new Error(`Ресурспак «${project.title}» уже установлен в профиль «${instance.name}»`);
    }
  } else if (contentType === "shader") {
    const shaders = await api.listInstanceShaderPacks(instance.id).catch(() => []);
    if (shaders.some((s) => matchesLocalFile(s.filename))) {
      throw new Error(`Шейдерпак «${project.title}» уже установлен в профиль «${instance.name}»`);
    }
  } else if (contentType === "mod") {
    const installed = await api.listInstanceMods(instance.id).catch(() => []);
    if (installed.some((m) => matchesLocalFile(m.filename))) {
      throw new Error(`Мод «${project.title}» уже установлен в профиль «${instance.name}»`);
    }
  }

  return await installModWithDependencies({
    target: { id: instance.id, name: instance.name },
    project,
    file: compat.bestVersion,
    mcVersion: instance.mc_version,
    loader: instance.instance_type,
    body: options.body,
  });
}

/** Resolves and queues the selected mod, resource pack, or shader into a profile. */
export async function installModWithDependencies(options: InstallProfileModOptions): Promise<InstallProfileModResult> {
  const { target, project, file } = options;
  const projectType = (
    (project as { content_type?: string; project_type?: string }).content_type ||
    (project as { content_type?: string; project_type?: string }).project_type ||
    "mod"
  ).toLowerCase();
  const category = projectType === "resourcepack" ? "resourcepack" : projectType === "shader" ? "shader" : "mod";

  const main = installInput(target.id, file, `${category}install:${target.id}:${file.platform}:${file.version_id}`, project.title, category);
  if (!main) {
    throw new Error(category === "mod" ? "У выбранной версии нет доступного файла .jar" : "У выбранной версии нет доступного файла .zip");
  }

  if (category !== "mod") {
    await api.installInstanceMod(main);
    return {
      queued: 1,
      dependencies: 0,
      alreadyInstalled: 0,
    };
  }

  const installed = await api.listInstanceMods(target.id);
  const existingNames = new Set(installed.flatMap((mod) => [mod.filename.toLowerCase(), mod.filename.toLowerCase().replace(/\.disabled$/, "")]));
  const required = (file.dependencies || []).filter((dep) => dep.type === "required" && (dep.project_id || dep.version_id));
  const shouldResolve = Boolean(file.download_url && file.download_url.toLowerCase().includes(".jar"));
  let dependencies: InstallableFile[] = [];

  if (shouldResolve) {
    const result = await api.resolveDependencies({
      dependencies: required,
      mc_version: options.mcVersion || file.game_versions?.[0] || "",
      loader: options.loader || file.loaders?.[0] || "",
      file_url: file.download_url,
      platform: file.platform,
      project_id: file.mod_id,
      title: project.title,
      summary: project.summary,
      body: options.body || "",
      changelog: file.changelog || "",
      existing_source_keys: sourceKeys(file),
      existing_filenames: [...installed.map((mod) => mod.filename), file.filename],
    });
    const needsConfirm = result.needs_confirm || [];
    let finalResult = result;
    const includeMedium =
      needsConfirm.length > 0 &&
      window.confirm(
        `Найдены зависимости со средней уверенностью:\n${needsConfirm.map((dep) => `• ${dep.title || dep.filename}`).join("\n")}\n\nДобавить их в профиль?`
      );
    if (includeMedium) {
      finalResult = await api.resolveDependencies({
        dependencies: required,
        mc_version: options.mcVersion || file.game_versions?.[0] || "",
        loader: options.loader || file.loaders?.[0] || "",
        file_url: file.download_url,
        platform: file.platform,
        project_id: file.mod_id,
        title: project.title,
        summary: project.summary,
        body: options.body || "",
        changelog: file.changelog || "",
        allow_medium_confidence: true,
        existing_source_keys: sourceKeys(file),
        existing_filenames: [...installed.map((mod) => mod.filename), file.filename],
      });
    }
    const unresolved = finalResult.unresolved || [];
    const resolverErrors = Object.values(finalResult.errors || {}).filter(Boolean);
    if (unresolved.length || resolverErrors.length || finalResult.status === "error") {
      const reasons = [...unresolved.map((dep) => dep.reason || dep.mod_id), ...resolverErrors];
      throw new Error(`Не удалось разрешить обязательные зависимости: ${reasons.join("; ") || "ошибка поиска"}`);
    }
    dependencies = (finalResult.resolved || []).map(cartItemToInstallableFile);
  }

  const groupID = main.group_id || `modinstall:${target.id}:${file.platform}:${file.version_id}`;
  const candidates = [main];
  for (const dep of dependencies) {
    if (dep.folder && dep.folder !== "mods") {
      throw new Error(`Зависимость «${dep.title || dep.filename}» размещается в «${dep.folder}» и пока не поддерживается установщиком профиля`);
    }
    const input = installInput(target.id, dep, groupID, dep.name || dep.title || "Зависимость");
    if (!input) throw new Error(`Не удалось получить прямой .jar-файл зависимости «${dep.title || dep.filename}»`);
    candidates.push(input);
  }
  const unique = new Map<string, InstallInstanceModInput>();
  for (const item of candidates) {
    const key = item.filename.toLowerCase();
    if (unique.has(key)) continue;
    if (existingNames.has(key)) {
      if (key === main.filename.toLowerCase()) throw new Error(`Мод «${main.filename}» уже установлен в профиль`);
      continue;
    }
    unique.set(key, item);
  }
  if (!unique.has(main.filename.toLowerCase())) {
    throw new Error(`Мод «${main.filename}» уже установлен в профиль`);
  }
  const inputs = [...unique.values()];
  let queued = 0;
  try {
    // Queue dependencies first so a rejected dependency request cannot leave the main mod queued alone.
    for (const input of [...inputs.slice(1), inputs[0]]) {
      await api.installInstanceMod(input);
      queued++;
    }
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    throw new Error(queued ? `Частичная установка: в очередь добавлено ${queued} из ${inputs.length} файлов. ${reason}` : reason);
  }
  return {
    queued,
    dependencies: queued - 1,
    alreadyInstalled: candidates.length - unique.size,
  };
}
