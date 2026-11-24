import { useCallback, useEffect, useMemo, useState } from "react";
import { api } from "../api";
interface CartLookupIndex {
  groupIds: Set<string>;
  sourceKeys: Set<string>;
  titles: Set<string>;
}
import { pushToast } from "../toastBus";
import type { CartItem, DepResolutionStatus, ProjectCard, SavedPack, UnresolvedDep, VersionFile } from "../types";

const CART_KEY = "modhub_cart_v1";

function loadCartArr(): CartItem[] {
  try {
    const raw = localStorage.getItem(CART_KEY);
    const arr = raw ? JSON.parse(raw) : [];
    return Array.isArray(arr) ? arr : [];
  } catch {
    return [];
  }
}

function parseMcParts(v: string): [number, number, number] {
  const parts = String(v || "").split(".");
  return [Number(parts[0] || 0), Number(parts[1] || 0), Number(parts[2] || 0)];
}

export function pickNewestMcVersion(gameVersions: string[], preferred = ""): string {
  const list = (gameVersions || []).filter(Boolean);
  if (preferred && list.includes(preferred)) return preferred;
  if (!list.length) return preferred || "";
  return [...list].sort((a, b) => {
    const pa = parseMcParts(a);
    const pb = parseMcParts(b);
    if (pa[0] !== pb[0]) return pb[0] - pa[0];
    if (pa[1] !== pb[1]) return pb[1] - pa[1];
    return pb[2] - pa[2];
  })[0];
}

export function buildCartEntryFromVersionFile(
  v: VersionFile,
  item: ProjectCard,
  preferredMc = "",
  preferredLoader = ""
): CartItem {
  const folderMap: Record<string, string> = {
    mod: "mods",
    resourcepack: "resourcepacks",
    shader: "shaderpacks",
    datapack: "datapacks",
  };
  const folder = folderMap[item.content_type] || "mods";
  const chosenMc = pickNewestMcVersion(v.game_versions || [], preferredMc);
  const chosenLoader =
    (preferredLoader && (v.loaders || []).some((l) => l.toLowerCase() === preferredLoader.toLowerCase())
      ? preferredLoader
      : (v.loaders || [])[0]) || "";

  return {
    title: item.title,
    icon_url: item.icon_url,
    platform: v.platform,
    source_key: v.source_key,
    source_keys: (item.sources || []).map((s) => s.source_key),
    group_id: item.group_id,
    filename: v.filename,
    file_size: v.file_size,
    download_url: v.download_url,
    fallback_url: v.fallback_url,
    is_available: v.is_available,
    folder,
    loaders: v.loaders || [],
    game_versions: v.game_versions || [],
    mc_version: chosenMc,
    loader: chosenLoader,
    auto_dep_for: [],
  };
}

export function triggerBlobDownload(blob: Blob, filename: string): void {
  const u = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = u;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  window.setTimeout(() => URL.revokeObjectURL(u), 2000);
}

export function reportPackDepOutcome(
  mainFilename: string,
  resolvedDeps: CartItem[],
  unresolvedDeps?: UnresolvedDep[],
  warnings?: string[],
  errors?: Record<string, string>,
  status?: DepResolutionStatus
): void {
  const unresCount = unresolvedDeps?.length || 0;
  const errMsgs = Object.values(errors || {}).filter(Boolean);
  const warnMsgs = [...(warnings || []), ...errMsgs];

  if (resolvedDeps.length > 0 && unresCount === 0 && warnMsgs.length === 0 && status !== "partial" && status !== "error") {
    const names = resolvedDeps.map((d) => d.title).filter(Boolean).slice(0, 3).join(", ");
    pushToast(`Добавлено: ${mainFilename} + ${resolvedDeps.length} завис. (${names})`, "ok");
    return;
  }

  if (unresCount > 0 || status === "partial" || status === "error" || warnMsgs.length > 0) {
    const missingNames = (unresolvedDeps || []).map((u) => u.mod_id).filter(Boolean).slice(0, 3).join(", ");
    const addedPart = resolvedDeps.length > 0 ? ` + ${resolvedDeps.length} завис.` : "";
    const missingPart =
      unresCount > 0
        ? `Не удалось разрешить обязательные зависимости (${unresCount}): ${missingNames || "см. карточку мода"}`
        : warnMsgs[0] || "Не все зависимости удалось проверить";
    pushToast(`Добавлен ${mainFilename}${addedPart}. ${missingPart}`, "err");
    return;
  }

  pushToast(`Добавлено в сборку: ${mainFilename}`, "ok");
}

export interface AddVersionFileOpts {
  mcVersion?: string;
  loader?: string;
}

export function usePackCart(version: string, loadersIn: string[]) {
  const [cart, setCart] = useState<CartItem[]>(loadCartArr);
  const [savedPacks, setSavedPacks] = useState<SavedPack[]>([]);

  useEffect(() => {
    const t = window.setTimeout(() => {
      try {
        localStorage.setItem(CART_KEY, JSON.stringify(cart));
      } catch {}
    }, 120);
    return () => window.clearTimeout(t);
  }, [cart]);

  const cartLookup = useMemo<CartLookupIndex>(() => {
    const groupIds = new Set<string>();
    const sourceKeys = new Set<string>();
    const titles = new Set<string>();
    for (const c of cart) {
      if (c.group_id) groupIds.add(c.group_id);
      if (c.source_key) sourceKeys.add(c.source_key);
      if (c.source_keys) {
        for (const sk of c.source_keys) sourceKeys.add(sk);
      }
      const nt = (c.title || "").trim().toLowerCase();
      if (nt) titles.add(nt);
    }
    return { groupIds, sourceKeys, titles };
  }, [cart]);

  const addCartEntries = useCallback((entries: CartItem[], parentKey = "") => {
    setCart((prev) => {
      const next = prev.map((item) => ({
        ...item,
        auto_dep_for: [...(item.auto_dep_for || [])],
      }));
      for (const entry of entries) {
        const key = `${entry.folder}:${entry.filename}`;
        const existingIdx = next.findIndex((c) => `${c.folder}:${c.filename}` === key);
        if (existingIdx >= 0) {
          if (parentKey) {
            const parents = new Set(next[existingIdx].auto_dep_for || []);
            parents.add(parentKey);
            next[existingIdx] = {
              ...next[existingIdx],
              auto_dep_for: Array.from(parents),
            };
          }
          continue;
        }
        next.push({
          ...entry,
          auto_dep_for: parentKey ? [parentKey] : [...(entry.auto_dep_for || [])],
        });
      }
      return next;
    });
  }, []);

  const pruneCascadeDependents = (prev: CartItem[], targetIdx: number): CartItem[] => {
    const target = prev[targetIdx];
    if (!target) return prev;
    const parentKey = `${target.folder}:${target.filename}`;
    const out: CartItem[] = [];
    for (let i = 0; i < prev.length; i++) {
      if (i === targetIdx) continue;
      const c = prev[i];
      if (!c.auto_dep_for?.length) {
        out.push(c);
        continue;
      }
      const rem = c.auto_dep_for.filter((k) => k !== parentKey);
      if (rem.length === 0) continue;
      out.push(rem.length === c.auto_dep_for.length ? c : { ...c, auto_dep_for: rem });
    }
    return out;
  };

  const removeCartAt = useCallback((index: number) => {
    setCart((prev) => pruneCascadeDependents(prev, index));
  }, []);

  const removeCartByKey = useCallback((folder: string, filename: string) => {
    const wantFolder = folder || "mods";
    setCart((prev) => {
      const idx = prev.findIndex((c) => (c.folder || "mods") === wantFolder && c.filename === filename);
      if (idx < 0) return prev;
      return pruneCascadeDependents(prev, idx);
    });
  }, []);

  const handleAddVersionFile = useCallback(
    async (v: VersionFile, item: ProjectCard, opts?: AddVersionFileOpts) => {
      const prefMc = opts?.mcVersion || version;
      const prefLoader = opts?.loader || loadersIn[0] || "";
      const mainEntry = buildCartEntryFromVersionFile(v, item, prefMc, prefLoader);
      const parentKey = `${mainEntry.folder}:${mainEntry.filename}`;
      addCartEntries([mainEntry]);

      const depRefs = (v.dependencies || []).filter(
        (d) => (d.type === "required" || d.type === "incompatible") && (d.project_id || d.version_id)
      );
      const canInspectManifest =
        mainEntry.folder === "mods" && Boolean(v.download_url && v.download_url.toLowerCase().includes(".jar"));



      try {
        const existingKeys = [
          ...cart.flatMap((c) => [c.source_key, ...(c.source_keys || [])]),
          mainEntry.source_key,
          ...(mainEntry.source_keys || []),
        ].filter(Boolean);
        const existingFns = [...cart.map((c) => c.filename), mainEntry.filename].filter(Boolean);

        const res = await api.resolveDependencies({
          dependencies: depRefs,
          mc_version: mainEntry.mc_version,
          loader: mainEntry.loader,
          file_url: canInspectManifest ? v.download_url : "",
          platform: v.platform,
          project_id: v.mod_id,
          title: item.title || "",
          summary: item.summary || "",
          changelog: v.changelog || "",
          existing_source_keys: existingKeys,
          existing_filenames: existingFns,
        });
        const depEntries = res.resolved || [];
        if (depEntries.length > 0) {
          addCartEntries(depEntries, parentKey);
        }
        const confirmList = res.needs_confirm || [];
        if (confirmList.length > 0 && depEntries.length === 0 && !(res.unresolved || []).length) {
          const names = confirmList.map((d) => d.title).slice(0, 3).join(", ");
          pushToast(
            `Добавлен ${v.filename}. В описании найдены вероятные зависимости (${names}) — проверьте вкладку «Зависимости»`,
            "info"
          );
          return;
        }
        reportPackDepOutcome(
          v.filename,
          depEntries,
          res.unresolved,
          res.warnings,
          res.errors,
          res.status
        );
      } catch (err) {
        pushToast(
          `Добавлен ${v.filename}, но не удалось разрешить зависимости: ${(err as Error).message}`,
          "err"
        );
      }
    },
    [addCartEntries, cart, loadersIn, version]
  );

  return {
    cart,
    setCart,
    cartLookup,
    savedPacks,
    setSavedPacks,
    addCartEntries,
    removeCartAt,
    removeCartByKey,
    handleAddVersionFile,
  };
}
