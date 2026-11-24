import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";
import { DependencyTreeList } from "./DependencyTreeList";
import { ModDependenciesTab } from "./modmodal/ModDependenciesTab";
import { ModSourcesTab } from "./modmodal/ModSourcesTab";
import { useOverlayTransition, useScrollFade } from "../hooks";
import { installModWithDependencies } from "../utils/profileModInstall";
import { CategoryIcon, CloseIcon, EnvBadgeIcon, LoaderIcon, PlatformPill } from "../icons";
import type {
  ContentType,
  DepResolutionStatus,
  DepTreeNode,
  UnresolvedDep,
  DependentsResponse,
  FilterCategory,
  GalleryItem,
  ModDetailsResponse,
  ModVersionsResponse,
  ProjectCard,
  VersionFile,
} from "../types";
import {
  cachedIconUrl,
  extractDominantRgbFromImg,
  fallbackIconDataUri,
  fallbackRgbFromTitle,
  filterRealLoaders,
  formatBytes,
  formatDate,
  formatLoaderName,
  formatMcVersionRange,
  formatNumber,
  getCachedAmbientRgb,
  getEnvKind,
  isImageUrlLoaded,
  markImageUrlLoaded,
  mapCategoryTags,
  renderSafeBodyHtml,
  stripHtmlTags,
} from "../utils";

export type ModalTab = "description" | "gallery" | "files" | "dependencies" | "dependents" | "sources";

interface ModModalProps {
  item: ProjectCard | null;
  preloadedDetails?: ModDetailsResponse | null;
  initialTab?: ModalTab;
  historyPrev: ProjectCard | null;
  availableCategories: FilterCategory[];
  activeVersion: string;
  activeLoadersIn: string[];
  activeLoadersOut: string[];
  enabledSources: string[];
  onClose: () => void;
  onBack: () => void;
  onOpenOther: (next: ProjectCard, pushHistory: boolean) => void;
  onItemMerged?: (merged: ProjectCard) => void;
  onQuickDownload?: (item: ProjectCard) => void;
  onAuthorClick: (author: string) => void;
  onCategoryClick: (catId: string) => void;
  onOpenDependentsCatalog: (item: ProjectCard, opts: { relation: string; projectFilter: string; contentType?: ContentType }) => void;
  onOpenLightbox: (items: GalleryItem[], idx: number) => void;
  modInstallTarget?: { id: string; name: string } | null;
  isTargetProfileInstalled?: boolean;
  onClearModInstall?: () => void;
  onToast?: (text: string, kind?: "info" | "ok" | "err") => void;
}

const BADGE_HOST_RE = /(shields\.io|badgen\.net|badge\.svg|img\.shields|modrinth\.com\/badge|cf\.way2muchnoise\.eu)/i;

function buildVersionsFromDetails(det: ModDetailsResponse | null): ModVersionsResponse | null {
  if (!det || !Array.isArray(det.versions)) return null;
  return {
    ok: true,
    items: det.versions,
    merged_item: det.merged_item || null,
    page: 1,
    limit: det.versions.length,
    total: det.versions.length,
    has_more: false,
    facets: det.versions_facets || {
      loaders: det.meta_loaders || [],
      game_versions: det.meta_game_versions || [],
      channels: [],
    },
  };
}

export const ModModal: React.FC<ModModalProps> = ({
  item,
  preloadedDetails = null,
  initialTab = "description",
  historyPrev,
  availableCategories,
  activeVersion,
  activeLoadersIn,
  activeLoadersOut,
  enabledSources,
  onClose,
  onBack,
  onOpenOther,
  onItemMerged,
  onQuickDownload,
  onAuthorClick,
  onCategoryClick,
  onOpenDependentsCatalog,
  onOpenLightbox,
  modInstallTarget = null,
  isTargetProfileInstalled = false,
  onClearModInstall,
  onToast,
}) => {
  const isOpen = Boolean(item);
  const { mounted, shown } = useOverlayTransition(isOpen, 240);
  const [retainedItem, setRetainedItem] = useState<ProjectCard | null>(item);
  useEffect(() => {
    if (item) setRetainedItem(item);
  }, [item]);
  const [tab, setTab] = useState<ModalTab>(initialTab);
  const [details, setDetails] = useState<ModDetailsResponse | null>(preloadedDetails);
  const [detailsOwnerGid, setDetailsOwnerGid] = useState(() => (item && preloadedDetails ? item.group_id : ""));
  const [detailsLoading, setDetailsLoading] = useState(!preloadedDetails);
  const [detailsError, setDetailsError] = useState("");
  const [heroGlowRgb, setHeroGlowRgb] = useState("140, 152, 170");
  const [compactBarShown, setCompactBarShown] = useState(false);
  const [heroPhotoReady, setHeroPhotoReady] = useState(false);
  const [loadedGalUrls, setLoadedGalUrls] = useState<Set<string>>(() => new Set());
  const [copiedFileKey, setCopiedFileKey] = useState("");
  const [installingVersion, setInstallingVersion] = useState("");

  const installVersionToProfile = async (v: VersionFile, project: ProjectCard) => {
    if (!modInstallTarget) return;
    const key = `${v.platform}:${v.version_id}`;
    setInstallingVersion(key);
    try {
      const result = await installModWithDependencies({
        target: modInstallTarget,
        project,
        file: v,
        mcVersion: v.game_versions?.includes(activeVersion) ? activeVersion : v.game_versions?.[0] || activeVersion,
        loader: v.loaders?.find((loader) => activeLoadersIn.includes(loader)) || v.loaders?.[0] || activeLoadersIn[0] || "",
        body: ownedDetails?.body || "",
      });
      onToast?.(`В профиль «${modInstallTarget.name}» добавлено в очередь: ${result.queued} файлов, зависимостей: ${result.dependencies}`, "ok");
    } catch (err) {
      onToast?.(err instanceof Error ? err.message : String(err), "err");
    } finally {
      setInstallingVersion("");
    }
  };
  const copyTimerRef = useRef<number | null>(null);

  // Files tab state — immediately hydrated from preloadedDetails.versions (zero post-load!)
  const [versData, setVersData] = useState<ModVersionsResponse | null>(() => buildVersionsFromDetails(preloadedDetails));
  const [versOwnerGid, setVersOwnerGid] = useState(() => (item && preloadedDetails?.versions ? item.group_id : ""));
  const [versLoadedGid, setVersLoadedGid] = useState(() => (item && preloadedDetails?.versions ? item.group_id : ""));
  const [versLoading, setVersLoading] = useState(false);
  const [versError, setVersError] = useState("");
  const [versRetryTick, setVersRetryTick] = useState(0);
  const [fileLoader, setFileLoader] = useState("all");
  const [fileMc, setFileMc] = useState("");
  const [fileChannel, setFileChannel] = useState("all");
  const [fileSource, setFileSource] = useState("all");
  const [fileSearch, setFileSearch] = useState("");
  const [openLogKey, setOpenLogKey] = useState("");
  const [openDepKey, setOpenDepKey] = useState("");
  const [logCache, setLogCache] = useState<Record<string, { loading: boolean; text: string; format: string }>>({});
  const [depCache, setDepCache] = useState<Record<string, { loading: boolean; tree: DepTreeNode[]; unresolved?: UnresolvedDep[]; warnings?: string[]; errors?: Record<string, string>; status?: DepResolutionStatus; fatalError?: string }>>({});
  const currentGidRef = useRef(item?.group_id || "");
  currentGidRef.current = item?.group_id || "";
  const depsAbortRef = useRef<AbortController | null>(null);
  const depsSeqRef = useRef(0);
  const fileDepAbortRef = useRef<AbortController | null>(null);
  const fileDepSeqRef = useRef(0);

  // Own Dependencies tab filter state (tree resolution delegated to ModDependenciesTab -> useDependencyTree)
  const [ownDepMc, setOwnDepMc] = useState("");
  const [ownDepLoader, setOwnDepLoader] = useState("all");

  // Addons / Dependents tab state (strictly separated by content type: mod / resourcepack / datapack / shader)
  const [depsContentType, setDepsContentType] = useState<ContentType>("mod");
  const [depsRel, setDepsRel] = useState("required");
  const [depsPf, setDepsPf] = useState("no_modpacks");
  const [depsSearch, setDepsSearch] = useState("");
  const [depsSort, setDepsSort] = useState("default");
  const [depsUseFilters, setDepsUseFilters] = useState(false);
  const [depsItems, setDepsItems] = useState<ProjectCard[]>([]);
  const [depsMeta, setDepsMeta] = useState<DependentsResponse | null>(null);
  const [depsOwnerGid, setDepsOwnerGid] = useState("");
  const [depsLoading, setDepsLoading] = useState(false);
  const [depsLoadingMore, setDepsLoadingMore] = useState(false);
  const [depsError, setDepsError] = useState("");
  const [depsLoadedKey, setDepsLoadedKey] = useState("");

  const modalWinRef = useRef<HTMLDivElement | null>(null);
  const modalBodyRef = useRef<HTMLDivElement | null>(null);
  const tabsRef = useRef<HTMLDivElement | null>(null);
  const descRef = useRef<HTMLDivElement | null>(null);
  const [tabInd, setTabInd] = useState({ left: 0, width: 0, ready: false });

  useEffect(() => {
    if (!item) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [item, onClose]);

  useEffect(() => {
    // Always abort all in-flight per-project requests and bump sequences when project changes or modal closes
    depsAbortRef.current?.abort();
    depsAbortRef.current = null;
    depsSeqRef.current++;
    fileDepAbortRef.current?.abort();
    fileDepAbortRef.current = null;
    fileDepSeqRef.current++;

    if (!item) {
      setDepsLoading(false);
      setDepsLoadingMore(false);
      return;
    }

    const reqGid = item.group_id;
    const rgbKey = item.icon_url || reqGid;
    setHeroGlowRgb(getCachedAmbientRgb(rgbKey) || fallbackRgbFromTitle(item.title));
    setTab(initialTab);
    setDetailsError("");
    setVersError("");
    setVersRetryTick(0);
    setFileLoader("all");
    setFileMc("");
    setFileChannel("all");
    setFileSource("all");
    setFileSearch("");
    setOpenLogKey("");
    setOpenDepKey("");
    setLogCache({});
    setDepCache({});

    // Fully reset Dependents & Dependencies state for the newly opened project
    setDepsContentType("mod");
    setDepsRel("required");
    setDepsPf("no_modpacks");
    setDepsSearch("");
    setDepsSort("default");
    setDepsItems([]);
    setDepsMeta(null);
    setDepsOwnerGid("");
    setDepsLoadedKey("");
    setDepsError("");
    setDepsLoading(false);
    setDepsLoadingMore(false);

    setOwnDepMc(activeVersion || "");
    setOwnDepLoader(activeLoadersIn.length === 1 ? activeLoadersIn[0] : "all");

    // Only accept preloadedDetails if it actually belongs to THIS project!
    const preloadedMatches =
      preloadedDetails &&
      (!preloadedDetails.merged_item?.group_id ||
        preloadedDetails.merged_item.group_id === reqGid ||
        (item.sources || []).some((s) =>
          (preloadedDetails.merged_item?.sources || []).some((ms) => ms.source_key === s.source_key)
        ))
        ? preloadedDetails
        : null;

    const ready = preloadedMatches || api.getCachedModDetails(item);
    if (ready) {
      setDetails(ready);
      setDetailsOwnerGid(reqGid);
      setDetailsLoading(false);
      const preVers = buildVersionsFromDetails(ready);
      if (preVers) {
        setVersData(preVers);
        setVersOwnerGid(reqGid);
        setVersLoadedGid(reqGid);
        setVersLoading(false);
      } else {
        setVersData(null);
        setVersOwnerGid("");
        setVersLoadedGid("");
      }
      return;
    }

    const ctrl = new AbortController();
    setDetails(null);
    setDetailsOwnerGid("");
    setDetailsLoading(true);
    setVersData(null);
    setVersOwnerGid("");
    setVersLoadedGid("");
    setVersLoading(false);

    api
      .getModDetails(item, ctrl.signal)
      .then((res) => {
        if (ctrl.signal.aborted || currentGidRef.current !== reqGid) return;
        setDetails(res);
        setDetailsOwnerGid(reqGid);
        setDetailsLoading(false);
        const preVers = buildVersionsFromDetails(res);
        if (preVers) {
          setVersData(preVers);
          setVersOwnerGid(reqGid);
          setVersLoadedGid(reqGid);
        }
        if (res.merged_item && onItemMerged && res.merged_item.sources?.length > (item.sources?.length || 0)) {
          onItemMerged(res.merged_item);
        }
      })
      .catch((err) => {
        if ((err as Error).name === "AbortError" || ctrl.signal.aborted || currentGidRef.current !== reqGid) return;
        setDetailsError(String((err as Error).message || err));
        setDetailsLoading(false);
      });

    return () => ctrl.abort();
  }, [item?.group_id, initialTab, preloadedDetails]);

  const ownedDetails = details && item && detailsOwnerGid === item.group_id ? details : null;
  const ownedVersData = versData && item && versOwnerGid === item.group_id ? versData : null;
  const ownedDepsItems = item && depsOwnerGid === item.group_id ? depsItems : [];
  const ownedDepsMeta = item && depsOwnerGid === item.group_id ? depsMeta : null;

  const itemSourcesForVersions = ownedDetails?.merged_item?.sources || item?.sources || [];
  const sourcesSig = itemSourcesForVersions.map((s) => s.source_key).join(",");

  useEffect(() => {
    if (!item || (tab !== "files" && tab !== "dependencies")) return;
    const reqGid = item.group_id;
    if (ownedVersData && versLoadedGid === reqGid && versRetryTick === 0) return;
    const loadKey = `${reqGid}|${sourcesSig}`;
    if (versLoadedGid === loadKey && ownedVersData) return;

    const ctrl = new AbortController();
    setVersLoading(true);
    setVersError("");

    api
      .getModVersions(
        {
          item: ownedDetails?.merged_item || item,
          sources: itemSourcesForVersions,
          content_type: item.content_type || "mod",
          preload_all: true,
          sort: "mc_version_desc",
        },
        ctrl.signal
      )
      .then((res) => {
        if (ctrl.signal.aborted || currentGidRef.current !== reqGid) return;
        setVersData(res);
        setVersOwnerGid(reqGid);
        setVersLoadedGid(loadKey);
        setVersLoading(false);
      })
      .catch((err) => {
        if ((err as Error).name === "AbortError" || ctrl.signal.aborted || currentGidRef.current !== reqGid) return;
        setVersError(String((err as Error).message || err));
        setVersLoading(false);
      });

    return () => ctrl.abort();
  }, [item?.group_id, tab, sourcesSig, versRetryTick, ownedVersData]);

  // Bind clickable images inside description -> open Lightbox
  useEffect(() => {
    if (tab !== "description" || detailsLoading || !descRef.current) return;
    const root = descRef.current;
    const allImgs = Array.from(root.querySelectorAll("img"));
    for (const img of allImgs) {
      const src = img.getAttribute("src") || "";
      const isBadge = BADGE_HOST_RE.test(src) || (img.naturalHeight > 0 && img.naturalHeight <= 38);
      if (isBadge) {
        img.classList.add("js-badge-img");
      } else {
        img.classList.add("js-content-img");
      }
    }
    const imgs = allImgs.filter((img) => {
      const src = img.getAttribute("src") || "";
      return src && !BADGE_HOST_RE.test(src) && !img.closest("a");
    });
    if (!imgs.length) return;
    const galleryItems: GalleryItem[] = imgs.map((img) => ({
      url: img.getAttribute("src") || "",
      title: img.getAttribute("alt") || "",
      description: "",
    }));
    const handlers: Array<[HTMLImageElement, () => void]> = [];
    imgs.forEach((img, idx) => {
      img.classList.add("js-zoomable-img");
      const fn = () => onOpenLightbox(galleryItems, idx);
      img.addEventListener("click", fn);
      handlers.push([img, fn]);
    });
    const copyCleanups: Array<() => void> = [];
    const pres = Array.from(root.querySelectorAll("pre"));
    pres.forEach((pre) => {
      pre.classList.add("md-code-block");
      const existing = pre.querySelector(".code-copy-btn");
      if (existing) existing.remove();

      const btn = document.createElement("button");
      btn.type = "button";
      btn.className = "code-copy-btn";
      btn.title = "Копировать код";
      btn.setAttribute("aria-label", "Копировать код");
      btn.innerHTML =
        '<svg class="ico-copy" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>' +
        '<svg class="ico-check" width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"/></svg>';

      let resetTimer = 0;
      const onCopyClick = (e: MouseEvent) => {
        e.stopPropagation();
        const codeEl = pre.querySelector("code");
        const txt = (codeEl ? codeEl.innerText : pre.innerText).trim();
        if (!txt) return;
        navigator.clipboard?.writeText(txt).catch(() => {});
        btn.classList.add("is-copied");
        if (resetTimer) window.clearTimeout(resetTimer);
        resetTimer = window.setTimeout(() => btn.classList.remove("is-copied"), 1600);
      };

      btn.addEventListener("click", onCopyClick);
      pre.appendChild(btn);
      copyCleanups.push(() => {
        if (resetTimer) window.clearTimeout(resetTimer);
        btn.removeEventListener("click", onCopyClick);
        btn.remove();
      });
    });

    return () => {
      handlers.forEach(([img, fn]) => img.removeEventListener("click", fn));
      copyCleanups.forEach((fn) => fn());
    };
  }, [tab, detailsLoading, ownedDetails?.body, onOpenLightbox]);

  const handleCopyFilename = (key: string, filename: string) => {
    if (!filename) return;
    navigator.clipboard?.writeText(filename).catch(() => {});
    setCopiedFileKey(key);
    if (copyTimerRef.current) window.clearTimeout(copyTimerRef.current);
    copyTimerRef.current = window.setTimeout(() => setCopiedFileKey(""), 1600);
  };

  const toggleFileChangelog = async (v: VersionFile, vidKey: string) => {
    if (openLogKey === vidKey) {
      setOpenLogKey("");
      return;
    }
    setOpenLogKey(vidKey);
    if (logCache[vidKey]) return;
    if (v.changelog && v.changelog.trim()) {
      setLogCache((prev) => ({
        ...prev,
        [vidKey]: { loading: false, text: v.changelog, format: v.platform === "curseforge" ? "html" : "markdown" },
      }));
      return;
    }
    if (v.platform === "curseforge" && v.mod_id && v.version_id) {
      setLogCache((prev) => ({ ...prev, [vidKey]: { loading: true, text: "", format: "html" } }));
      try {
        const res = await api.getFileChangelog({ platform: v.platform, mod_id: v.mod_id, file_id: v.version_id });
        setLogCache((prev) => ({
          ...prev,
          [vidKey]: { loading: false, text: res.changelog || "Описание изменений отсутствует.", format: res.format || "html" },
        }));
      } catch {
        setLogCache((prev) => ({
          ...prev,
          [vidKey]: { loading: false, text: "Не удалось загрузить список изменений.", format: "markdown" },
        }));
      }
    } else {
      setLogCache((prev) => ({
        ...prev,
        [vidKey]: { loading: false, text: "Автор не указал список изменений для этой версии.", format: "markdown" },
      }));
    }
  };

  const fetchFileDeps = async (v: VersionFile, vidKey: string) => {
    const reqGid = currentGidRef.current;
    const deps = (v.dependencies || []).filter((d) => d.project_id || d.version_id);
    if (deps.length === 0 && !v.download_url) {
      setDepCache((prev) => ({ ...prev, [vidKey]: { loading: false, tree: [] } }));
      return;
    }
    fileDepAbortRef.current?.abort();
    const ctrl = new AbortController();
    fileDepAbortRef.current = ctrl;
    const seq = ++fileDepSeqRef.current;

    setDepCache((prev) => ({ ...prev, [vidKey]: { loading: true, tree: [] } }));
    try {
      const res = await api.getDependencyTree(deps, {
        mc_version: (fileMc && v.game_versions.includes(fileMc) ? fileMc : v.game_versions[0]) || "",
        loader: (fileLoader !== "all" && v.loaders.includes(fileLoader) ? fileLoader : v.loaders[0]) || "",
        file_url: v.download_url || "",
        platform: v.platform,
        project_id: v.mod_id,
        title: activeItem?.title || "",
        summary: activeItem?.summary || "",
        body: ownedDetails?.body || "",
        changelog: v.changelog || "",
        max_depth: 3,
        signal: ctrl.signal,
      });
      if (ctrl.signal.aborted || seq !== fileDepSeqRef.current || currentGidRef.current !== reqGid) return;
      setDepCache((prev) => ({
        ...prev,
        [vidKey]: {
          loading: false,
          tree: res.tree || [],
          unresolved: res.unresolved || [],
          warnings: res.warnings || [],
          errors: res.errors || {},
          status: res.status || "none",
        },
      }));
    } catch (err) {
      if ((err as Error).name === "AbortError" || ctrl.signal.aborted || currentGidRef.current !== reqGid) return;
      setDepCache((prev) => ({
        ...prev,
        [vidKey]: {
          loading: false,
          tree: [],
          status: "error",
          fatalError: `Не удалось загрузить зависимости: ${(err as Error).message}`,
        },
      }));
    }
  };

  const toggleFileDeps = async (v: VersionFile, vidKey: string) => {
    if (openDepKey === vidKey) {
      setOpenDepKey("");
      return;
    }
    setOpenDepKey(vidKey);
    const cached = depCache[vidKey];
    if (cached && !cached.loading && !cached.fatalError && cached.status !== "error") return;
    await fetchFileDeps(v, vidKey);
  };

  const depsFilterSig = `${item?.group_id || ""}|${sourcesSig}|${depsContentType}|${depsRel}|${depsPf}|${depsUseFilters ? `${activeVersion}|${activeLoadersIn.join(",")}|${activeLoadersOut.join(",")}` : "nofilt"}|${enabledSources.join(",")}`;

  const loadDependents = async (append: boolean) => {
    if (!item) return;
    const reqGid = item.group_id;
    const reqSig = depsFilterSig;
    const targetCard = ownedDetails?.merged_item || item;

    if (!append) {
      depsAbortRef.current?.abort();
      depsAbortRef.current = new AbortController();
      setDepsItems([]);
      setDepsMeta(null);
      setDepsOwnerGid(reqGid);
      setDepsLoading(true);
      setDepsError("");
    } else {
      if (depsOwnerGid !== reqGid) return;
      if (!depsAbortRef.current) {
        depsAbortRef.current = new AbortController();
      }
      setDepsLoadingMore(true);
    }
    const seq = ++depsSeqRef.current;
    const signal = depsAbortRef.current.signal;

    try {
      const res = await api.getDependents(
        {
          item: targetCard,
          relation: depsRel,
          project_filter: depsPf,
          content_type: depsContentType,
          mc_version: depsUseFilters ? activeVersion : "",
          loaders_in: depsUseFilters ? activeLoadersIn : [],
          loaders_out: depsUseFilters ? activeLoadersOut : [],
          sources: enabledSources,
          sort: depsSort,
          cf_index: append ? ownedDepsMeta?.next_cf_index || 0 : 0,
          page_size: 50,
        },
        signal
      );
      // Strictly verify response still belongs to the currently open project and sequence!
      if (seq !== depsSeqRef.current || signal.aborted || currentGidRef.current !== reqGid) return;
      setDepsOwnerGid(reqGid);
      if (append) {
        setDepsItems((prev) => {
          const seen = new Set(prev.map((x) => x.group_id));
          return [...prev, ...(res.items || []).filter((x) => !seen.has(x.group_id))];
        });
        setDepsMeta((prev) => {
          if (!prev) return res;
          const mergedCounts: Record<string, number> = { ...(prev.counts_by_type || {}) };
          for (const [k, v] of Object.entries(res.counts_by_type || {})) {
            mergedCounts[k] = (mergedCounts[k] || 0) + Number(v || 0);
          }
          return {
            ...res,
            total_cf: Math.max(Number(prev.total_cf || 0), Number(res.total_cf || 0)),
            cf_scanned: Number(prev.cf_scanned || 0) + Number(res.cf_scanned || 0),
            skipped_modpacks: Number(prev.skipped_modpacks || 0) + Number(res.skipped_modpacks || 0),
            counts_by_type: mergedCounts,
          };
        });
      } else {
        setDepsItems(res.items || []);
        setDepsMeta(res);
      }
      setDepsLoadedKey(reqSig);
    } catch (err) {
      if ((err as Error).name === "AbortError" || seq !== depsSeqRef.current || currentGidRef.current !== reqGid) return;
      setDepsError(String((err as Error).message || err));
    } finally {
      if (seq === depsSeqRef.current && currentGidRef.current === reqGid) {
        setDepsLoading(false);
        setDepsLoadingMore(false);
      }
    }
  };

  useEffect(() => {
    if (!item || tab !== "dependents") return;
    if (depsLoadedKey !== depsFilterSig) {
      void loadDependents(false);
    }
  }, [item?.group_id, tab, depsFilterSig]);

  // Hero cover parallax + 1.5px reading progress bar (direct CSS vars via RAF, zero React re-renders)
  useEffect(() => {
    const bodyEl = modalBodyRef.current;
    const winEl = modalWinRef.current;
    if (!mounted || !bodyEl || !winEl) return;

    bodyEl.scrollTop = 0;
    winEl.style.setProperty("--modal-scroll", "0");
    winEl.style.setProperty("--modal-read-progress", "0");
    setCompactBarShown(false);

    let raf = 0;
    const update = () => {
      raf = 0;
      const maxScroll = Math.max(0, bodyEl.scrollHeight - bodyEl.clientHeight);
      const top = bodyEl.scrollTop;
      const ratio = maxScroll > 20 ? Math.min(1, Math.max(0, top / maxScroll)) : 0;
      const heroShift = Math.min(1, Math.max(0, top / 260));
      winEl.style.setProperty("--modal-scroll", heroShift.toFixed(4));
      winEl.style.setProperty("--modal-read-progress", ratio.toFixed(4));
      const nextCompact = top > 118;
      setCompactBarShown((prev) => (prev === nextCompact ? prev : nextCompact));
    };

    const onScroll = () => {
      if (!raf) raf = requestAnimationFrame(update);
    };

    bodyEl.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      if (raf) cancelAnimationFrame(raf);
      bodyEl.removeEventListener("scroll", onScroll);
    };
  }, [mounted, item?.group_id, tab]);

  const prevTabIdxRef = useRef<number>(-1);
  const [tabSlideDir, setTabSlideDir] = useState<"left" | "right" | "none">("none");

  useLayoutEffect(() => {
    const order: ModalTab[] = ["description", "gallery", "files", "dependencies", "dependents", "sources"];
    const nextIdx = order.indexOf(tab);
    const prevIdx = prevTabIdxRef.current;
    if (prevIdx >= 0 && nextIdx >= 0 && prevIdx !== nextIdx) {
      setTabSlideDir(nextIdx > prevIdx ? "left" : "right");
    } else {
      setTabSlideDir("none");
    }
    prevTabIdxRef.current = nextIdx;

    if (!item || !tabsRef.current) return;
    const active = tabsRef.current.querySelector<HTMLButtonElement>(`[data-tab="${tab}"]`);
    if (active) {
      setTabInd({ left: active.offsetLeft, width: active.offsetWidth, ready: true });
    }
  }, [item, tab]);

  const baseItem = item || retainedItem;
  const activeItem = ownedDetails?.merged_item || baseItem;
  const itemSupportsLoaders = (activeItem?.content_type || "mod") === "mod" || (activeItem?.content_type || "mod") === "datapack";
  const modalCats = useMemo(
    () => mapCategoryTags(activeItem?.categories || [], availableCategories, 8),
    [activeItem?.categories, availableCategories]
  );

  const filteredFiles = useMemo(() => {
    const list = ownedVersData?.items || [];
    const ql = fileSearch.trim().toLowerCase();
    return list.filter((v) => {
      if (fileLoader !== "all" && !v.loaders.includes(fileLoader)) return false;
      if (fileMc && !v.game_versions.includes(fileMc)) return false;
      if (fileChannel !== "all" && v.release_type !== fileChannel) return false;
      if (fileSource !== "all" && v.platform !== fileSource) return false;
      if (ql) {
        const hay = `${v.name} ${v.filename} ${v.version_number} ${v.game_versions.join(" ")}`.toLowerCase();
        if (!hay.includes(ql)) return false;
      }
      return true;
    });
  }, [ownedVersData?.items, fileLoader, fileMc, fileChannel, fileSource, fileSearch]);

  // Representative file for the "Зависимости" tab strictly matching selected MC version & loader
  const { ownDepFile, ownDepMismatch } = useMemo(() => {
    const list = ownedVersData?.items || [];
    if (!list.length) return { ownDepFile: null as VersionFile | null, ownDepMismatch: false };
    const hasFilter = Boolean(ownDepMc || (itemSupportsLoaders && ownDepLoader !== "all"));
    const matchPool = list.filter((v) => {
      if (ownDepMc && !v.game_versions.includes(ownDepMc)) return false;
      if (itemSupportsLoaders && ownDepLoader !== "all" && !v.loaders.includes(ownDepLoader)) return false;
      return true;
    });
    if (hasFilter && matchPool.length === 0) {
      return { ownDepFile: null as VersionFile | null, ownDepMismatch: true };
    }
    const pool = matchPool.length > 0 ? matchPool : list;
    // Prefer a matching release that declares dependencies (e.g. if one platform has full metadata), else newest release
    const relWithDeps = pool.find((v) => (v.release_type || "release") === "release" && (v.dependencies || []).length > 0);
    if (relWithDeps) return { ownDepFile: relWithDeps, ownDepMismatch: false };
    const rel = pool.find((v) => (v.release_type || "release") === "release");
    return { ownDepFile: rel || pool[0] || null, ownDepMismatch: false };
  }, [ownedVersData?.items, ownDepMc, ownDepLoader, itemSupportsLoaders]);

  const filteredDeps = useMemo(() => {
    const ql = depsSearch.trim().toLowerCase();
    let list = ownedDepsItems.filter((it) => {
      if (!ql) return true;
      const hay = `${it.title} ${it.summary} ${(it.authors || []).join(" ")}`.toLowerCase();
      return hay.includes(ql);
    });
    if (depsSort === "downloads") {
      list = [...list].sort((a, b) => (b.downloads || 0) - (a.downloads || 0));
    } else if (depsSort === "updated") {
      list = [...list].sort((a, b) => String(b.date_modified || "").localeCompare(String(a.date_modified || "")));
    }
    return list;
  }, [ownedDepsItems, depsSearch, depsSort]);

  useScrollFade(modalBodyRef, [mounted, item?.group_id, tab, detailsLoading, versLoading, filteredFiles.length, filteredDeps.length]);

  const mergedLinks = useMemo(() => {
    const out = { source_url: "", issues_url: "", wiki_url: "", discord_url: "" };
    const cands = [
      ownedDetails?.links,
      ...(ownedDetails?.sources_details || []).map((sd) => sd.links),
      activeItem?.links,
    ].filter(Boolean);
    for (const l of cands) {
      if (!out.source_url && l?.source_url) out.source_url = l.source_url;
      if (!out.issues_url && l?.issues_url) out.issues_url = l.issues_url;
      if (!out.wiki_url && l?.wiki_url) out.wiki_url = l.wiki_url;
      if (!out.discord_url && l?.discord_url) out.discord_url = l.discord_url;
    }
    return out;
  }, [ownedDetails, activeItem]);

  const renderedDescHtml = useMemo(
    () => renderSafeBodyHtml(ownedDetails?.body || activeItem?.summary || "", ownedDetails?.body_format || "markdown"),
    [ownedDetails?.body, ownedDetails?.body_format, activeItem?.summary]
  );
  const { shownFiles, latestRelIdx, maxFileSize } = useMemo(() => {
    const slice = filteredFiles.slice(0, 60);
    const lIdx = filteredFiles.findIndex((f) => (f.release_type || "release") === "release");
    let maxSz = 1;
    for (const f of slice) {
      const sz = Number(f.file_size || 0);
      if (sz > maxSz) maxSz = sz;
    }
    return { shownFiles: slice, latestRelIdx: lIdx, maxFileSize: maxSz };
  }, [filteredFiles]);
  const heroPhoto = ownedDetails?.gallery?.[0]?.url || "";

  useEffect(() => {
    if (!heroPhoto) {
      setHeroPhotoReady(false);
      return;
    }
    setHeroPhotoReady(false);
    let cancelled = false;
    const im = new Image();
    im.decoding = "async";
    im.onload = () => {
      if (!cancelled) setHeroPhotoReady(true);
    };
    im.onerror = () => {
      if (!cancelled) setHeroPhotoReady(false);
    };
    im.src = heroPhoto;
    if (im.complete && im.naturalWidth > 0) {
      setHeroPhotoReady(true);
    }
    return () => {
      cancelled = true;
    };
  }, [heroPhoto]);
  const hasCatalogFilters = Boolean(activeVersion || activeLoadersIn.length || activeLoadersOut.length);
  const envKind = activeItem && (activeItem.content_type || "mod") === "mod" ? getEnvKind(activeItem) : "";

  const secondaryLinks = [
    mergedLinks.source_url ? { key: "src", label: "Исходный код", url: mergedLinks.source_url } : null,
    mergedLinks.issues_url ? { key: "iss", label: "Баг-трекер", url: mergedLinks.issues_url } : null,
    mergedLinks.wiki_url ? { key: "wiki", label: "Wiki", url: mergedLinks.wiki_url } : null,
    mergedLinks.discord_url ? { key: "dc", label: "Discord", url: mergedLinks.discord_url } : null,
  ].filter(Boolean) as Array<{ key: string; label: string; url: string }>;

  if (!mounted || !activeItem) return null;

  return (
    <div
      className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-hidden={!shown}
      onClick={onClose}
    >
      {activeItem && (
        <div ref={modalWinRef} className="modal-window modal-xl" onClick={(e) => e.stopPropagation()}>
          <div className="modal-header" id="modModalHeader">
            <div
              className={`modal-hero-bg${heroPhoto ? (heroPhotoReady ? " has-photo is-photo-loaded" : " has-photo is-photo-loading") : " has-glow"}`}
              style={
                heroPhoto
                  ? ({
                      backgroundImage: `url(${JSON.stringify(heroPhoto)})`,
                      "--hero-glow-rgb": heroGlowRgb,
                    } as React.CSSProperties)
                  : ({ "--hero-glow-rgb": heroGlowRgb } as React.CSSProperties)
              }
              aria-hidden="true"
            />
            <div style={{ flex: 1, minWidth: 0, position: "relative", zIndex: 1 }}>
              {historyPrev && (
                <div className="modal-back-row">
                  <button type="button" className="btn btn-ghost btn-xs" onClick={onBack}>
                    ← Назад к {historyPrev.title}
                  </button>
                </div>
              )}
              <div className="card-head" style={{ minWidth: 0, maxHeight: "none", height: "auto", overflow: "visible" }}>
                <img
                  width={72}
                  height={72}
                  className="card-icon"
                  src={activeItem.icon_url ? cachedIconUrl(activeItem.icon_url, 160) : fallbackIconDataUri(activeItem.title)}
                  alt=""
                  onLoad={(e) => {
                    const rgb = extractDominantRgbFromImg(
                      e.currentTarget,
                      activeItem.icon_url || activeItem.group_id,
                      activeItem.title
                    );
                    if (rgb) setHeroGlowRgb(rgb);
                  }}
                  onError={(e) => {
                    e.currentTarget.src = fallbackIconDataUri(activeItem.title);
                  }}
                />
                <div className="card-title-wrap">
                  <h2 className="modal-title">{activeItem.title}</h2>
                  <div className="modal-subtitle">
                    Авторы:{" "}
                    {(activeItem.authors || []).slice(0, 5).map((a, i) => (
                      <React.Fragment key={a}>
                        {i > 0 && ", "}
                        <button
                          type="button"
                          className="author-link"
                          onClick={() => {
                            onClose();
                            onAuthorClick(a);
                          }}
                        >
                          {a}
                        </button>
                      </React.Fragment>
                    ))}
                    {(!activeItem.authors || activeItem.authors.length === 0) && "—"} • Скачиваний:{" "}
                    <span className="mono-num">{formatNumber(activeItem.downloads)}</span>
                  </div>
                  <div className="modal-header-meta-row">
                    <div className="card-platforms">
                      {(activeItem.sources || []).map((s) => (
                        <PlatformPill key={s.source_key} platform={s.platform} href={s.url} showArrow size={14} />
                      ))}
                      {envKind && <EnvBadgeIcon env={envKind} />}
                    </div>
                    {modalCats.length > 0 && (
                      <div className="modal-header-tags">
                        {modalCats.map((c) => (
                          <button
                            key={c.id}
                            type="button"
                            className="pill pill-cat js-cat-tag"
                            title={`Фильтровать по категории: ${c.label}`}
                            onClick={() => {
                              onClose();
                              onCategoryClick(c.id);
                            }}
                          >
                            <CategoryIcon id={c.id} size={11.5} />
                            <span>{c.label}</span>
                          </button>
                        ))}
                      </div>
                    )}
                  </div>
                </div>
              </div>
            </div>
            <button
              type="button"
              className="icon-close"
              aria-label="Закрыть"
              style={{ position: "relative", zIndex: 1 }}
              onClick={onClose}
            >
              <CloseIcon size={15} />
            </button>
          </div>

          <div className="modal-tabs" ref={tabsRef}>
            <span
              className={`modal-tab-indicator${tabInd.ready ? " ready" : ""}`}
              style={{ width: `${tabInd.width}px`, transform: `translateX(${tabInd.left}px)` }}
              aria-hidden="true"
            />
            <button type="button" className={`modal-tab${tab === "description" ? " active" : ""}`} data-tab="description" onClick={() => setTab("description")}>
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/><line x1="16" y1="13" x2="8" y2="13"/><line x1="16" y1="17" x2="8" y2="17"/></svg>
              <span>Описание</span>
            </button>
            <button type="button" className={`modal-tab${tab === "gallery" ? " active" : ""}`} data-tab="gallery" onClick={() => setTab("gallery")}>
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="8.5" cy="8.5" r="1.5"/><polyline points="21 15 16 10 5 21"/></svg>
              <span>Галерея</span>
            </button>
            <button type="button" className={`modal-tab${tab === "files" ? " active" : ""}`} data-tab="files" onClick={() => setTab("files")}>
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              <span>Файлы</span>
            </button>
            <button type="button" className={`modal-tab${tab === "dependencies" ? " active" : ""}`} data-tab="dependencies" onClick={() => setTab("dependencies")}>
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7" rx="1.5"/><rect x="14" y="14" width="7" height="7" rx="1.5"/><path d="M10 6.5h4a3.5 3.5 0 0 1 3.5 3.5v4"/></svg>
              <span>Зависимости</span>
            </button>
            <button type="button" className={`modal-tab${tab === "dependents" ? " active" : ""}`} data-tab="dependents" onClick={() => setTab("dependents")}>
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M6 21V9a9 9 0 0 0 9 9"/></svg>
              <span>Аддоны</span>
            </button>
            <button type="button" className={`modal-tab${tab === "sources" ? " active" : ""}`} data-tab="sources" onClick={() => setTab("sources")}>
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/></svg>
              <span>Источники</span>
            </button>
          </div>

          <div className="modal-read-track" aria-hidden="true"><div className="modal-read-bar" /></div>

          <div className="modal-body-shell">
            {modInstallTarget && (
              <div className="mh-info-callout" style={{ margin: "8px 14px", position: "sticky", top: 0, zIndex: 3 }}>
                Установка в профиль «{modInstallTarget.name}»
                <button type="button" className="btn btn-ghost btn-xs" onClick={onClearModInstall}>Отменить</button>
              </div>
            )}
            <div className={`modal-compact-bar${compactBarShown ? " is-visible" : ""}`} aria-hidden={!compactBarShown}>
              <div className="modal-compact-left">
                <img
                  width={24}
                  height={24}
                  className="modal-compact-icon"
                  src={activeItem.icon_url ? cachedIconUrl(activeItem.icon_url, 64) : fallbackIconDataUri(activeItem.title)}
                  alt=""
                  onError={(e) => {
                    e.currentTarget.src = fallbackIconDataUri(activeItem.title);
                  }}
                />
                <span className="modal-compact-title" title={activeItem.title}>{activeItem.title}</span>
              </div>
              <div className="modal-compact-actions">
                <button
                  type="button"
                  className={`btn ${modInstallTarget && isTargetProfileInstalled ? "btn-ghost is-installed" : "btn-primary"} btn-xs btn-quick-dl`}
                  tabIndex={compactBarShown ? 0 : -1}
                  disabled={Boolean(modInstallTarget && isTargetProfileInstalled)}
                  onClick={() => {
                    if (modInstallTarget && isTargetProfileInstalled) return;
                    if (onQuickDownload) {
                      onQuickDownload(activeItem);
                    } else {
                      setTab("files");
                    }
                  }}
                >
                  {modInstallTarget && isTargetProfileInstalled ? (
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                      <polyline points="20 6 9 17 4 12" />
                    </svg>
                  ) : (
                    <svg className="dl-arrow-ico" width="12.5" height="12.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                      <g className="dl-arrow-head">
                        <polyline points="7 10 12 15 17 10" />
                        <line x1="12" y1="15" x2="12" y2="3" />
                      </g>
                    </svg>
                  )}
                  <span>
                    {modInstallTarget
                      ? isTargetProfileInstalled
                        ? `Уже в «${modInstallTarget.name}»`
                        : `В профиль «${modInstallTarget.name}»`
                      : "В профиль"}
                  </span>
                </button>
              </div>
            </div>

          <div className="modal-body" ref={modalBodyRef}>
            <div key={tab} className={`modal-tab-pane tab-slide-${tabSlideDir}`}>
              {ownedDetails?.errors && Object.keys(ownedDetails.errors).length > 0 && (
                <div className="modal-source-warn" role="status" aria-live="polite">
                  {Object.entries(ownedDetails.errors).map(([plat, msg]) => (
                    <div key={plat} className="modal-source-warn-row">
                      <span className="source-error-dot" aria-hidden="true" />
                      <span>
                        <strong>{plat === "curseforge" ? "CurseForge" : "Modrinth"}:</strong> {msg}
                      </span>
                    </div>
                  ))}
                </div>
              )}
              {tab === "description" && (
                detailsLoading ? (
                  <div className="empty-state"><span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />Загрузка описания...</div>
                ) : detailsError ? (
                  <div className="empty-state">Ошибка: {detailsError}</div>
                ) : (
                  <div>
                    {(() => {
                      const mcInfo = formatMcVersionRange(ownedDetails?.meta_game_versions?.length ? ownedDetails.meta_game_versions : activeItem.game_versions);
                      const loaderList = itemSupportsLoaders
                        ? filterRealLoaders(ownedDetails?.meta_loaders?.length ? ownedDetails.meta_loaders : activeItem.loaders || [], activeItem.content_type || "mod")
                        : [];
                      const envText =
                        envKind === "client"
                          ? "Только клиент"
                          : envKind === "server"
                            ? "Только сервер"
                            : envKind === "both"
                              ? "Клиент и сервер"
                              : "Не указана";
                      return (
                        <div className="modal-compat-strip" aria-label="Сводка совместимости">
                          <div className="modal-compat-cell">
                            <span className="modal-compat-lbl">Версии Minecraft</span>
                            <strong className="mono-num" title={mcInfo?.full || undefined}>
                              {mcInfo ? mcInfo.label : "Не указаны"}
                            </strong>
                          </div>
                          {itemSupportsLoaders && (
                            <div className="modal-compat-cell">
                              <span className="modal-compat-lbl">Загрузчики</span>
                              <div className="modal-compat-loaders">
                                {loaderList.length > 0 ? (
                                  loaderList.map((l) => (
                                    <span key={l} className="modal-compat-loader-chip">
                                      <LoaderIcon loader={l} size={11.5} />
                                      <span>{formatLoaderName(l)}</span>
                                    </span>
                                  ))
                                ) : (
                                  <strong>—</strong>
                                )}
                              </div>
                            </div>
                          )}
                          {(activeItem.content_type || "mod") === "mod" && (
                            <div className="modal-compat-cell">
                              <span className="modal-compat-lbl">Совместимость</span>
                              <strong>{envText}</strong>
                            </div>
                          )}
                          <div className="modal-compat-cell">
                            <span className="modal-compat-lbl">Обновлён</span>
                            <strong className="mono-num">{formatDate(activeItem.date_modified)}</strong>
                          </div>
                          {ownedDetails?.license && (
                            <div className="modal-compat-cell">
                              <span className="modal-compat-lbl">Лицензия</span>
                              <strong className="mono-num">{ownedDetails.license}</strong>
                            </div>
                          )}
                        </div>
                      );
                    })()}
                    <div
                      ref={descRef}
                      className="mod-description"
                      dangerouslySetInnerHTML={{ __html: renderedDescHtml }}
                    />
                  </div>
                )
              )}

              {tab === "gallery" && (
                detailsLoading ? (
                  <div className="empty-state"><span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />Загрузка галереи...</div>
                ) : !ownedDetails?.gallery?.length ? (
                  <div className="empty-state">
                    <svg className="empty-state-svg" viewBox="0 0 48 48" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                      <rect x="6" y="10" width="36" height="28" rx="4" />
                      <circle cx="17" cy="20" r="3" />
                      <path d="M42 30l-9-9-11 11-5-5-11 11" />
                    </svg>
                    <h3>Нет скриншотов</h3>
                    <p>У этого проекта пока нет изображений в галерее.</p>
                  </div>
                ) : (
                  <div className="gallery-grid is-uniform">
                    {(ownedDetails?.gallery || []).map((g, i) => (
                      <figure
                        key={`${g.url}-${i}`}
                        className={`gallery-item${g.title ? " has-caption" : ""}`}
                        onClick={() => onOpenLightbox(ownedDetails?.gallery || [], i)}
                      >
                        {(() => {
                          const gReady = isImageUrlLoaded(g.url) || loadedGalUrls.has(g.url);
                          const markGalReady = () => {
                            markImageUrlLoaded(g.url);
                            if (!loadedGalUrls.has(g.url)) {
                              setLoadedGalUrls((prev) => {
                                const next = new Set(prev);
                                next.add(g.url);
                                return next;
                              });
                            }
                          };
                          return (
                        <div className={`gallery-img-wrap img-skel-host${gReady ? " is-img-ready" : ""}`}>
                          <span className="img-skel-shimmer" aria-hidden="true" />
                          <img
                            className={`img-fade${gReady ? " is-loaded" : ""}`}
                            src={g.url}
                            alt={g.title || ""}
                            loading={i < 2 ? "eager" : "lazy"}
                            decoding="async"
                            onLoad={markGalReady}
                            onError={markGalReady}
                            ref={(el) => {
                              if (el && el.complete && el.naturalWidth > 0 && !gReady) markGalReady();
                            }}
                          />
                          <span className="gallery-zoom-badge" aria-hidden="true">
                            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round">
                              <circle cx="11" cy="11" r="7" />
                              <line x1="21" y1="21" x2="16.65" y2="16.65" />
                              <line x1="11" y1="8" x2="11" y2="14" />
                              <line x1="8" y1="11" x2="14" y2="11" />
                            </svg>
                          </span>
                          {g.title && (
                            <figcaption className="gallery-caption-overlay">
                              <span>{g.title}</span>
                            </figcaption>
                          )}
                        </div>
                          );
                        })()}
                      </figure>
                    ))}
                  </div>
                )
              )}

              {tab === "files" && (
                <div>
                  <div className="files-toolbar" style={{ display: "flex", gap: 8, flexWrap: "wrap", marginBottom: 12 }}>
                    <input
                      type="search"
                      className="text-input"
                      style={{ flex: 1, minWidth: 160 }}
                      placeholder="Поиск версии / файла..."
                      value={fileSearch}
                      onChange={(e) => setFileSearch(e.target.value)}
                    />
                    {itemSupportsLoaders && (ownedVersData?.facets?.loaders?.length || 0) > 0 && (
                      <CustomSelect
                        value={fileLoader}
                        options={[
                          { value: "all", label: "Все загрузчики" },
                          ...ownedVersData!.facets.loaders.map((l): CustomSelectOption => ({
                            value: l,
                            label: formatLoaderName(l),
                            icon: <LoaderIcon loader={l} size={12.5} />,
                          })),
                        ]}
                        onChange={setFileLoader}
                        className="cs-auto"
                        ariaLabel="Загрузчик"
                      />
                    )}
                    {(ownedVersData?.facets?.game_versions?.length || 0) > 0 && (
                      <CustomSelect
                        value={fileMc}
                        options={[
                          { value: "", label: "Любая MC" },
                          ...ownedVersData!.facets.game_versions.map((gv): CustomSelectOption => ({
                            value: gv,
                            label: gv,
                          })),
                        ]}
                        onChange={setFileMc}
                        searchable
                        searchPlaceholder="Версия MC..."
                        mono
                        className="cs-auto"
                        ariaLabel="Версия MC"
                      />
                    )}
                    <CustomSelect
                      value={fileChannel}
                      options={[
                        { value: "all", label: "Все каналы" },
                        { value: "release", label: "Release", icon: <span className="channel-dot dot-release" /> },
                        { value: "beta", label: "Beta", icon: <span className="channel-dot dot-beta" /> },
                        { value: "alpha", label: "Alpha", icon: <span className="channel-dot dot-alpha" /> },
                      ]}
                      onChange={setFileChannel}
                      className="cs-auto"
                      ariaLabel="Канал"
                    />
                    {(activeItem.platforms?.length || 0) > 1 && (
                      <CustomSelect
                        value={fileSource}
                        options={[
                          { value: "all", label: "Все источники" },
                          { value: "modrinth", label: "Modrinth" },
                          { value: "curseforge", label: "CurseForge" },
                        ]}
                        onChange={setFileSource}
                        className="cs-auto"
                        ariaLabel="Источник файла"
                      />
                    )}
                  </div>

                  {versLoading ? (
                    <div className="empty-state"><span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />Загрузка файлов...</div>
                  ) : versError ? (
                    <div className="empty-state">
                      <div>Ошибка загрузки файлов: {versError}</div>
                      <button
                        type="button"
                        className="btn btn-primary btn-xs"
                        style={{ marginTop: 10 }}
                        onClick={() => {
                          setVersLoadedGid("");
                          setVersRetryTick((t) => t + 1);
                        }}
                      >
                        Повторить
                      </button>
                    </div>
                  ) : !filteredFiles.length ? (
                    <div className="empty-state">Файлы не найдены.</div>
                  ) : (
                    <div className="versions-list">
                      {shownFiles.map((v, vIdx) => {
                          const isLatestRelease = vIdx === latestRelIdx;
                          const sizePct = Math.max(4, Math.min(100, Math.round((Number(v.file_size || 0) / maxFileSize) * 100)));
                        const vidKey = `${activeItem.group_id}:${v.platform}:${v.version_id}`;
                        const gvs = v.game_versions || [];
                        const verPart = gvs.length > 2 ? `${gvs[0]}–${gvs[gvs.length - 1]}` : gvs.join(", ");
                        const ldPart = itemSupportsLoaders ? (v.loaders || []).map(formatLoaderName).join(", ") : "";
                        const bracketParts = [verPart, ldPart].filter(Boolean);

                        const isLogOpen = openLogKey === vidKey;
                        const isDepOpen = openDepKey === vidKey;
                        const isExtraOpen = isLogOpen || isDepOpen;
                        const logState = logCache[vidKey];
                        const depState = depCache[vidKey];
                        const depCount = (v.dependencies || []).filter((d) => (d.project_id || d.version_id) && (d.type === "required" || d.type === "optional" || d.type === "incompatible" || d.type === "embedded" || d.type === "include" || d.type === "tool")).length;

                        return (
                          <div key={`${vidKey}-${v.filename}`} className={`version-row-wrap channel-${v.release_type || "release"}`}>
                            <div className={`version-row${isLatestRelease ? " is-latest-release" : ""}`} style={{ "--file-size-pct": `${sizePct}%` } as React.CSSProperties}>
                              <div className="version-main">
                                <div className="version-title-row">
                                  {bracketParts.length > 0 && (
                                    <span
                                      className="file-bracket-tag mono-num"
                                      title={`MC: ${gvs.join(", ") || "—"} | Загрузчики: ${ldPart || "—"}`}
                                    >
                                      [{bracketParts.join(" • ")}]
                                    </span>
                                  )}
                                  <span className="version-name">{v.name || v.filename}</span>
                                  {v.version_number && v.version_number !== v.name && (
                                    <span className="version-num-tag mono-num">{v.version_number}</span>
                                  )}
                                  <span className={`pill pill-${v.release_type || "release"}`}><span className={`channel-dot dot-${v.release_type || "release"}`} aria-hidden="true" /><span>{v.release_type || "release"}</span></span>
                                  <PlatformPill platform={v.platform} href={v.fallback_url || undefined} showArrow={Boolean(v.fallback_url)} size={12} />
                                </div>
                                <div className="version-meta">
                                  <button
                                    type="button"
                                    className={`version-filename-btn${copiedFileKey === `${vidKey}-${v.filename}` ? " is-copied" : ""}`}
                                    title="Нажмите, чтобы скопировать имя файла"
                                    onClick={(e) => {
                                      e.stopPropagation();
                                      handleCopyFilename(`${vidKey}-${v.filename}`, v.filename);
                                    }}
                                  >
                                    <span>{v.filename}</span>
                                    {copiedFileKey === `${vidKey}-${v.filename}` ? (
                                      <svg className="copy-ico check-draw-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                        <polyline points="20 6 9 17 4 12" />
                                      </svg>
                                    ) : (
                                      <svg className="copy-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                                        <rect x="9" y="9" width="13" height="13" rx="2" />
                                        <path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1" />
                                      </svg>
                                    )}
                                  </button>
                                  <span className="version-size-chip" title={`Вес файла: ${formatBytes(v.file_size)} (${sizePct}% от самого крупного)`}>
                                    <span>{formatBytes(v.file_size)}</span>
                                    <span className="file-size-meter" aria-hidden="true">
                                      <span className="file-size-meter-fill" />
                                    </span>
                                  </span>
                                  <span>• {formatDate(v.date_published)}</span>
                                </div>
                              </div>
                              <div className="version-actions">
                                <button
                                  type="button"
                                  className={`btn btn-ghost btn-xs btn-ver-toggle${isLogOpen ? " is-active" : ""}`}
                                  title="Показать список изменений (Changelog)"
                                  onClick={() => void toggleFileChangelog(v, vidKey)}
                                >
                                  Лог
                                </button>
                                {(depCount > 0 || (v.download_url && v.download_url.toLowerCase().includes(".jar"))) && (
                                  <button
                                    type="button"
                                    className={`btn btn-ghost btn-xs btn-ver-toggle${isDepOpen ? " is-active" : ""}`}
                                    title={depCount > 0 ? `Показать зависимости версии (${depCount})` : "Проверить зависимости из манифеста .jar"}
                                    onClick={() => void toggleFileDeps(v, vidKey)}
                                  >
                                    {depCount > 0 ? `Завис. (${depCount})` : "Завис."}
                                  </button>
                                )}
                                {activeItem.content_type === "mod" && (
                                  <button
                                    type="button"
                                    className="btn btn-ghost btn-xs"
                                    disabled={
                                      !v.download_url ||
                                      !v.filename.toLowerCase().endsWith(".jar") ||
                                      Boolean(installingVersion) ||
                                      Boolean(modInstallTarget && isTargetProfileInstalled)
                                    }
                                    onClick={() => void installVersionToProfile(v, activeItem)}
                                  >
                                    {installingVersion === `${v.platform}:${v.version_id}`
                                      ? "Добавление…"
                                      : modInstallTarget
                                        ? isTargetProfileInstalled
                                          ? `Уже в «${modInstallTarget.name}»`
                                          : `Установить в профиль «${modInstallTarget.name}»`
                                        : "Выбрать профиль для установки"}
                                  </button>
                                )}
                              </div>
                            </div>

                            <div className={`version-extra-collapse${isExtraOpen ? " is-open" : ""}`} aria-hidden={!isExtraOpen}>
                              <div className="version-extra-inner">
                                <div className="version-extra-panel">
                                  {isLogOpen && (
                                    <div>
                                      <div className="version-extra-head">
                                        <span>Список изменений — {v.version_number || v.name}</span>
                                        <button type="button" className="btn btn-ghost btn-xs" onClick={() => setOpenLogKey("")}>Скрыть</button>
                                      </div>
                                      {logState?.loading ? (
                                        <div className="modal-subtitle"><span className="spinner spinner-xs" /> Загрузка changelog...</div>
                                      ) : (
                                        <div
                                          className="changelog-content"
                                          dangerouslySetInnerHTML={{
                                            __html: renderSafeBodyHtml(
                                              logState?.text || "Описание изменений отсутствует.",
                                              logState?.format || "markdown"
                                            ),
                                          }}
                                        />
                                      )}
                                    </div>
                                  )}
                                  {isDepOpen && (
                                    <div style={{ marginTop: isLogOpen ? 12 : 0 }}>
                                      <div className="version-extra-head">
                                        <span>Зависимости версии</span>
                                        <button type="button" className="btn btn-ghost btn-xs" onClick={() => setOpenDepKey("")}>Скрыть</button>
                                      </div>
                                      <DependencyTreeList
                                        tree={depState?.tree || []}
                                        unresolved={depState?.unresolved}
                                        warnings={depState?.warnings}
                                        errors={depState?.errors}
                                        status={depState?.status}
                                        loading={Boolean(depState?.loading)}
                                        fatalError={depState?.fatalError}
                                        emptyTitle="Зависимости не объявлены"
                                        emptyHint="Для этой версии не найдено зависимостей в API и манифесте."
                                        onRetry={() => void fetchFileDeps(v, vidKey)}
                                      />
                                    </div>
                                  )}
                                </div>
                              </div>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  )}
                </div>
              )}

              {tab === "dependencies" && (
                <ModDependenciesTab
                  projectGroupId={activeItem.group_id}
                  projectTitle={activeItem.title}
                  projectSummary={activeItem.summary}
                  projectBody={ownedDetails?.body || ""}
                  itemSupportsLoaders={itemSupportsLoaders}
                  versData={ownedVersData}
                  versLoading={versLoading}
                  ownDepMc={ownDepMc}
                  ownDepLoader={ownDepLoader}
                  onChangeMc={setOwnDepMc}
                  onChangeLoader={setOwnDepLoader}
                  ownDepFile={ownDepFile}
                  ownDepMismatch={ownDepMismatch}
                />
              )}

              {tab === "dependents" && (
                <div>
                  <div className="deps-toolbar">
                    <div className="deps-type-tabs" role="tablist" aria-label="Тип связанных проектов">
                      {([
                        { id: "mod", label: "Моды" },
                        { id: "resourcepack", label: "Ресурспаки" },
                        { id: "datapack", label: "Датапаки" },
                        { id: "shader", label: "Шейдеры" },
                      ] as Array<{ id: ContentType; label: string }>).map((ctOpt) => {
                        const cnt = ownedDepsMeta?.counts_by_type?.[ctOpt.id];
                        const active = depsContentType === ctOpt.id;
                        return (
                          <button
                            key={ctOpt.id}
                            type="button"
                            role="tab"
                            aria-selected={active}
                            className={`deps-type-pill${active ? " is-active" : ""}`}
                            onClick={() => setDepsContentType(ctOpt.id)}
                          >
                            <span>{ctOpt.label}</span>
                            {typeof cnt === "number" && <span className="deps-type-count mono-num">{cnt}</span>}
                          </button>
                        );
                      })}
                    </div>
                    <div className="deps-toolbar-row">
                      <CustomSelect
                        value={depsRel}
                        options={[
                          { value: "required", label: "Обязательная зависимость" },
                          { value: "optional", label: "Опциональная зависимость" },
                          { value: "all", label: "Все типы связей" },
                          { value: "embedded", label: "Встроенная библиотека" },
                          { value: "tool", label: "Инструмент" },
                          { value: "include", label: "Включено" },
                          { value: "incompatible", label: "Несовместимо" },
                        ]}
                        onChange={setDepsRel}
                        className="cs-auto"
                        ariaLabel="Тип зависимости"
                      />
                      <input
                        type="search"
                        className="text-input deps-search-input"
                        placeholder="Фильтр по названию..."
                        value={depsSearch}
                        onChange={(e) => setDepsSearch(e.target.value)}
                      />
                      <CustomSelect
                        value={depsSort}
                        options={[
                          { value: "default", label: "Как на CurseForge" },
                          { value: "downloads", label: "По скачиваниям" },
                          { value: "updated", label: "По обновлению" },
                        ]}
                        onChange={setDepsSort}
                        className="cs-auto"
                        ariaLabel="Сортировка зависимых"
                      />
                    </div>
                    <div className="deps-toolbar-sub">
                      <span className="mono-num">
                        {depsLoading
                          ? "Загрузка…"
                          : ownedDepsMeta?.total_cf && ownedDepsMeta.source_mode === "cf_web"
                            ? `Загружено: ${filteredDeps.length} • На CurseForge: ~${formatNumber(ownedDepsMeta.total_cf)}`
                            : ownedDepsMeta?.source_mode === "cf_core_api"
                              ? `Найдено: ${filteredDeps.length}${ownedDepsMeta.cf_scanned ? ` (проверено: ${formatNumber(ownedDepsMeta.cf_scanned)} • Core API)` : " • Core API"}`
                              : `Найдено: ${filteredDeps.length}`}
                      </span>
                      <div className="deps-toolbar-right">
                        {hasCatalogFilters && (
                          <label className="chip-toggle deps-filter-toggle">
                            <input type="checkbox" checked={depsUseFilters} onChange={(e) => setDepsUseFilters(e.target.checked)} />
                            <span>Фильтры</span>
                          </label>
                        )}
                        {ownedDepsMeta?.cf_dependents_url && (
                          <a className="btn btn-ghost btn-xs" href={ownedDepsMeta.cf_dependents_url} target="_blank" rel="noopener noreferrer">
                            <img className="platform-icon-img platform-icon-cf" src="/assets/curseforge.svg" width={13} height={13} alt="CurseForge" />
                            <span>CF ↗</span>
                          </a>
                        )}
                        <button
                          type="button"
                          className="btn btn-primary btn-xs"
                          onClick={() => {
                            onClose();
                            onOpenDependentsCatalog(activeItem, { relation: depsRel, projectFilter: depsPf, contentType: depsContentType });
                          }}
                        >
                          В каталог ↗
                        </button>
                      </div>
                    </div>
                  </div>

                  {depsLoading && !ownedDepsItems.length ? (
                    <div className="empty-state"><span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />Загрузка Dependents...</div>
                  ) : depsError && !ownedDepsItems.length ? (
                    <div className="empty-state">Ошибка загрузки Dependents: {depsError}</div>
                  ) : !filteredDeps.length ? (
                    <div className="empty-state">
                      <div>Подтверждённых связанных проектов типа «{depsContentType === "resourcepack" ? "Ресурспаки" : depsContentType === "datapack" ? "Датапаки" : depsContentType === "shader" ? "Шейдеры" : "Моды"}» на загруженных страницах не найдено.</div>
                      {ownedDepsMeta?.errors && Object.keys(ownedDepsMeta.errors).length > 0 && (
                        <div className="dep-warn-note" style={{ marginTop: 8 }} role="status">
                          {Object.values(ownedDepsMeta.errors).join(" • ")}
                        </div>
                      )}
                      {ownedDepsMeta && (ownedDepsMeta.has_more || (ownedDepsMeta.skipped_modpacks > 0 && depsPf !== "all")) && (
                        <div style={{ marginTop: 10, display: "flex", gap: 8, justifyContent: "center", flexWrap: "wrap" }}>
                          {ownedDepsMeta.has_more && (
                            <button
                              type="button"
                              className={`btn btn-primary btn-xs${depsLoadingMore ? " is-loading" : ""}`}
                              disabled={depsLoadingMore}
                              onClick={() => loadDependents(true)}
                            >
                              {depsLoadingMore ? "Сканирование…" : "Загрузить следующие страницы"}
                            </button>
                          )}
                          {ownedDepsMeta.skipped_modpacks > 0 && depsPf !== "all" && (
                            <button type="button" className="btn btn-ghost btn-xs" onClick={() => setDepsPf("all")}>
                              Показать с модпаками ({ownedDepsMeta.skipped_modpacks})
                            </button>
                          )}
                        </div>
                      )}
                    </div>
                  ) : (
                    <>
                      {ownedDepsMeta?.errors && Object.keys(ownedDepsMeta.errors).length > 0 && (
                        <div className="dep-warn-note" role="status">{Object.values(ownedDepsMeta.errors).join(" • ")}</div>
                      )}
                      <div className="deps-grid">
                        {filteredDeps.map((dep) => {
                          const fb = fallbackIconDataUri(dep.title);
                          const depRel = dep.dependent_info?.relation || "dependent";
                          const relCls = depRel === "required" ? "pill-dep-req" : depRel === "incompatible" ? "pill-dep-incompat" : "pill-dep-rel";
                          const depLoaders = filterRealLoaders(dep.loaders || [], dep.content_type || "mod");
                          return (
                            <div
                              key={dep.group_id}
                              className="dep-card"
                              onClick={() => {
                                if (dep.content_type === "modpack" && dep.sources?.[0]?.url) {
                                  window.open(dep.sources[0].url, "_blank", "noopener,noreferrer");
                                } else {
                                  onOpenOther(dep, true);
                                }
                              }}
                            >
                              <img
                                width={52}
                                height={52}
                                className="dep-card-icon"
                                src={dep.icon_url ? cachedIconUrl(dep.icon_url, 112) : fb}
                                alt=""
                                loading="lazy"
                                onError={(e) => {
                                  e.currentTarget.src = fb;
                                }}
                              />
                              <div className="dep-card-body">
                                <div className="dep-card-head">
                                  <span className="dep-card-title" title={dep.title}>{dep.title}</span>
                                </div>
                                <div className="dep-card-pills">
                                  {(dep.platforms || []).map((p) => (
                                    <PlatformPill key={p} platform={p} size={12} />
                                  ))}
                                  {dep.dependent_info && <span className={`pill ${relCls}`}>{dep.dependent_info.label}</span>}
                                  {dep.content_type === "modpack" && <span className="pill pill-cat">Модпак</span>}
                                  {depLoaders.map((l) => (
                                    <span key={l} className="meta-ico-badge" title={formatLoaderName(l)}>
                                      <LoaderIcon loader={l} size={11} />
                                    </span>
                                  ))}
                                </div>
                                <p className="dep-card-summary">{stripHtmlTags(dep.summary) || "Без описания"}</p>
                                <div className="dep-card-meta">
                                  <span>от {(dep.authors || []).slice(0, 2).join(", ") || "—"}</span>
                                  <span>↓ {formatNumber(dep.downloads)} • {formatDate(dep.date_modified)}</span>
                                </div>
                              </div>
                            </div>
                          );
                        })}
                      </div>
                      {ownedDepsMeta?.has_more && (
                        <div className="deps-load-more-row">
                          <button
                            type="button"
                            className={`btn btn-ghost btn-sm${depsLoadingMore ? " is-loading" : ""}`}
                            disabled={depsLoadingMore}
                            aria-busy={depsLoadingMore}
                            onClick={() => loadDependents(true)}
                          >
                            {depsLoadingMore ? (
                              <><span className="spinner spinner-xs" aria-hidden="true" /><span>Загрузка…</span></>
                            ) : (
                              "Загрузить ещё"
                            )}
                          </button>
                        </div>
                      )}
                    </>
                  )}
                </div>
              )}

              {tab === "sources" && (
                <ModSourcesTab
                  activeItem={activeItem}
                  license={ownedDetails?.license}
                  secondaryLinks={secondaryLinks}
                  onClose={onClose}
                  onAuthorClick={onAuthorClick}
                />
              )}
            </div>
          </div>
          </div>
        </div>
      )}
    </div>
  );
};
