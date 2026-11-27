import React, { lazy, startTransition, Suspense, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { useAppSection } from "./hooks/useAppSection";
import { api } from "./api";
import { AppNavRail, type AppSection } from "./components/AppNavRail";
import { ContextMenu } from "./components/ContextMenu";
import { CustomSelect, type CustomSelectOption } from "./components/CustomSelect";
import { DependentsBanner } from "./components/DependentsBanner";
import { ErrorBoundary } from "./components/ErrorBoundary";
import { VirtualizedResults } from "./components/VirtualizedResults";
import { useAccounts } from "./hooks/useAccounts";
import { useDownloads } from "./hooks/useDownloads";
import { useInstances } from "./hooks/useInstances";
import { isSessionActive, useLauncher } from "./hooks/useLauncher";
import { useThemeSettings } from "./hooks/useThemeSettings";
import { SearchSessionDeduper } from "./utils/sessionDeduper";
import type { ModalTab } from "./components/ModModal";
import { ScrollTopButton } from "./components/ScrollTopButton";
import { Sidebar } from "./components/Sidebar";
import { TitleBar } from "./components/TitleBar";
import { ToastHost } from "./components/ToastHost";
import { TooltipHost } from "./components/TooltipHost";
import { Topbar } from "./components/Topbar";
const HomePage = lazy(() => import("./pages/HomePage").then((m) => ({ default: m.HomePage })));
const InstancesPage = lazy(() => import("./pages/InstancesPage").then((m) => ({ default: m.InstancesPage })));
const DownloadsPage = lazy(() => import("./pages/DownloadsPage").then((m) => ({ default: m.DownloadsPage })));
const AccountsPage = lazy(() => import("./pages/AccountsPage").then((m) => ({ default: m.AccountsPage })));
const SettingsPage = lazy(() => import("./pages/SettingsPage").then((m) => ({ default: m.SettingsPage })));
import { desktopBridge, subscribeBackendEvents } from "./services/bridge";
import { CloseIcon } from "./icons";
import { InstallToProfileModal } from "./components/InstallToProfileModal";
import { autoInstallModIntoInstance } from "./utils/profileModInstall";
import type { DesktopAppInfo } from "./services/wails";
import {
  getPageScrollBottomDistance,
  initSmoothScroll,
  scrollToTopSmooth,
  setBackgroundScrollLocked,
  setPageScrollTop,
  subscribePageScroll,
} from "./smoothScroll";
import { pushToast } from "./toastBus";
import type {
  ContentType,
  FilterCategory,
  GalleryItem,
  GameInstance,
  InstalledModInfo,
  ModDetailsResponse,
  ProjectCard,
  PublicConfig,
  SortMode,
  ViewMode,
} from "./types";
import { formatLoaderName, sha1FileHex } from "./utils";

const ModModal = lazy(() => import("./components/ModModal").then((m) => ({ default: m.ModModal })));
const QuickDownloadModal = lazy(() =>
  import("./components/QuickDownloadModal").then((m) => ({ default: m.QuickDownloadModal }))
);
const SettingsModal = lazy(() => import("./components/SettingsModal").then((m) => ({ default: m.SettingsModal })));
const Lightbox = lazy(() => import("./components/Lightbox").then((m) => ({ default: m.Lightbox })));
const AboutModal = lazy(() => import("./components/AboutModal").then((m) => ({ default: m.AboutModal })));

const RECENT_KEY = "modhub_recent_v1";
const FILTERS_KEY = "modhub_filters_v1";

interface PersistedFilters {
  contentType?: ContentType;
  version?: string;
  loadersIn?: string[];
  loadersOut?: string[];
  categoriesIn?: string[];
  categoriesOut?: string[];
  catMode?: "and" | "or";
  env?: string;
  authorFilter?: string;
  minDownloads?: number;
  updatedWithin?: number;
  hideAbandoned?: boolean;
  sort?: SortMode;
  srcModrinth?: boolean;
  srcCurseforge?: boolean;
}

function loadSavedFilters(): PersistedFilters {
  try {
    const raw = localStorage.getItem(FILTERS_KEY);
    if (!raw) return {};
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? (parsed as PersistedFilters) : {};
  } catch {
    return {};
  }
}

const SORT_OPTIONS: CustomSelectOption[] = [
  { value: "downloads", label: "По скачиваниям" },
  { value: "relevance", label: "По релевантности" },
  { value: "updated", label: "По обновлению" },
  { value: "newest", label: "Сначала новые" },
];

function loadArr<T>(key: string): T[] {
  try {
    const raw = localStorage.getItem(key);
    const arr = raw ? JSON.parse(raw) : [];
    return Array.isArray(arr) ? arr : [];
  } catch {
    return [];
  }
}

const SkeletonCard: React.FC<{ viewMode: ViewMode }> = ({ viewMode }) => {
  if (viewMode === "list") {
    return (
      <div className="skeleton-card-exact is-list">
        <div className="sk-box" style={{ width: 72, height: 72, borderRadius: 12, flexShrink: 0 }} />
        <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 8 }}>
          <div className="sk-box" style={{ width: "36%", height: 15 }} />
          <div className="sk-box" style={{ width: "76%", height: 12 }} />
        </div>
        <div className="sk-box" style={{ width: 96, height: 22 }} />
        <div className="sk-box" style={{ width: 108, height: 26 }} />
      </div>
    );
  }
  return (
    <div className="skeleton-card-exact">
      <div style={{ display: "flex", gap: 11, alignItems: "flex-start" }}>
        <div className="sk-box" style={{ width: 60, height: 60, borderRadius: 12, flexShrink: 0 }} />
        <div style={{ flex: 1, display: "flex", flexDirection: "column", gap: 7, paddingTop: 2 }}>
          <div className="sk-box" style={{ width: "78%", height: 14 }} />
          <div className="sk-box" style={{ width: "48%", height: 11 }} />
          <div style={{ display: "flex", gap: 5 }}>
            <div className="sk-box" style={{ width: 21, height: 21, borderRadius: 6 }} />
            <div className="sk-box" style={{ width: 21, height: 21, borderRadius: 6 }} />
            <div className="sk-box" style={{ width: 21, height: 21, borderRadius: 6 }} />
          </div>
        </div>
      </div>
      <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
        <div className="sk-box" style={{ width: "100%", height: 11 }} />
        <div className="sk-box" style={{ width: "82%", height: 11 }} />
      </div>
      <div style={{ display: "flex", gap: 6 }}>
        <div className="sk-box" style={{ width: 72, height: 20 }} />
        <div className="sk-box" style={{ width: 64, height: 20 }} />
      </div>
      <div style={{ marginTop: "auto", display: "flex", justifyContent: "space-between", alignItems: "center", paddingTop: 6, borderTop: "1px solid var(--border-subtle)" }}>
        <div className="sk-box" style={{ width: 104, height: 13 }} />
        <div className="sk-box" style={{ width: 88, height: 24 }} />
      </div>
    </div>
  );
};


function stabilizeCards(prev: ProjectCard[], next: ProjectCard[]): ProjectCard[] {
  if (!prev.length || !next.length) return next;
  const prevMap = new Map<string, ProjectCard>();
  for (const item of prev) {
    prevMap.set(item.group_id, item);
  }
  let changed = prev.length !== next.length;
  const out = next.map((n, idx) => {
    const p = prevMap.get(n.group_id);
    if (
      p &&
      p.title === n.title &&
      p.summary === n.summary &&
      p.icon_url === n.icon_url &&
      p.downloads === n.downloads &&
      p.date_modified === n.date_modified &&
      (p.platforms?.length || 0) === (n.platforms?.length || 0) &&
      (p.sources?.length || 0) === (n.sources?.length || 0) &&
      (p.dependent_info?.relation || "") === (n.dependent_info?.relation || "")
    ) {
      if (prev[idx] !== p) changed = true;
      return p;
    }
    changed = true;
    return n;
  });
  return changed ? out : prev;
}

export const App: React.FC = () => {
  const {
    theme,
    accent,
    setAccent,
    bgPreset,
    setBgPreset,
    viewMode,
    setViewMode,
    handleToggleTheme,
  } = useThemeSettings();

  // Filters (persisted safely in localStorage under modhub_filters_v1)
  const initialFiltersRef = useRef<PersistedFilters | null>(null);
  if (initialFiltersRef.current === null) {
    initialFiltersRef.current = loadSavedFilters();
  }
  const initF = initialFiltersRef.current;

  const [debouncedQuery, setDebouncedQuery] = useState("");
  const [contentType, setContentType] = useState<ContentType>(
    initF.contentType === "resourcepack" || initF.contentType === "shader" || initF.contentType === "datapack"
      ? initF.contentType
      : "mod"
  );
  const [version, setVersion] = useState(typeof initF.version === "string" ? initF.version : "");
  const [loadersIn, setLoadersIn] = useState<string[]>(Array.isArray(initF.loadersIn) ? initF.loadersIn : []);
  const [loadersOut, setLoadersOut] = useState<string[]>(Array.isArray(initF.loadersOut) ? initF.loadersOut : []);
  const [categoriesIn, setCategoriesIn] = useState<string[]>(Array.isArray(initF.categoriesIn) ? initF.categoriesIn : []);
  const [categoriesOut, setCategoriesOut] = useState<string[]>(Array.isArray(initF.categoriesOut) ? initF.categoriesOut : []);
  const [catMode, setCatMode] = useState<"and" | "or">(initF.catMode === "or" ? "or" : "and");
  const [env, setEnv] = useState(typeof initF.env === "string" ? initF.env : "");
  const [authorFilter, setAuthorFilter] = useState(typeof initF.authorFilter === "string" ? initF.authorFilter : "");
  const [minDownloads, setMinDownloads] = useState(typeof initF.minDownloads === "number" ? initF.minDownloads : 0);
  const [updatedWithin, setUpdatedWithin] = useState(typeof initF.updatedWithin === "number" ? initF.updatedWithin : 0);
  const [hideAbandoned, setHideAbandoned] = useState(Boolean(initF.hideAbandoned));
  const [sort, setSort] = useState<SortMode>(
    initF.sort === "relevance" || initF.sort === "updated" || initF.sort === "newest" ? initF.sort : "downloads"
  );
  const [srcModrinth, setSrcModrinth] = useState(initF.srcModrinth !== false);
  const [srcCurseforge, setSrcCurseforge] = useState(initF.srcCurseforge !== false);
  const [retryTick, setRetryTick] = useState(0);

  // Dependents Catalog Page Mode
  const [dependentsTarget, setDependentsTarget] = useState<ProjectCard | null>(null);
  const [dependentsRelation, setDependentsRelation] = useState("required");
  const [dependentsProjectFilter, setDependentsProjectFilter] = useState("no_modpacks");
  const [depsTotalCf, setDepsTotalCf] = useState(0);
  const [depsSourceMode, setDepsSourceMode] = useState("cf_web");
  const [depsCfUrl, setDepsCfUrl] = useState("");
  const [depsCountsByType, setDepsCountsByType] = useState<Record<string, number>>({});
  const [depsCfScanned, setDepsCfScanned] = useState(0);
  const [depsNextCfIndex, setDepsNextCfIndex] = useState(0);
  const [depsSkippedModpacks, setDepsSkippedModpacks] = useState(0);

  // Metadata
  const [mcVersionsAll, setMcVersionsAll] = useState<string[]>([]);
  const [mcVersionsByLoader, setMcVersionsByLoader] = useState<Record<string, string[]>>({});
  const [availableCategories, setAvailableCategories] = useState<FilterCategory[]>([]);
  const [config, setConfig] = useState<PublicConfig>({
    curseforge_configured: false,
    curseforge_key_masked: "",
    merge_threshold: 70,
  });

  // Results
  const [results, setResults] = useState<ProjectCard[]>([]);
  const [fuzzySuggestion, setFuzzySuggestion] = useState("");
  const [apiErrors, setApiErrors] = useState<Record<string, string>>({});
  const [cfStatus, setCfStatus] = useState("disabled");
  const [configReady, setConfigReady] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [isLoadingMore, setIsLoadingMore] = useState(false);
  const [hasMore, setHasMore] = useState(false);
  const [nextMrOffset, setNextMrOffset] = useState(0);
  const [nextCfOffset, setNextCfOffset] = useState(0);
  const mrHasMoreRef = useRef(true);
  const cfHasMoreRef = useRef(true);
  const sessionDeduperRef = useRef<SearchSessionDeduper>(new SearchSessionDeduper());
  const [slowLoading, setSlowLoading] = useState(false);

  // Installed SHA-1 map & busy states
  const [installedMap, setInstalledMap] = useState<Record<string, InstalledModInfo>>({});
  // Recent Projects
  const [recentProjects, setRecentProjects] = useState<ProjectCard[]>(() => loadArr<ProjectCard>(RECENT_KEY));

  // Desktop Shell Navigation, Download Manager & Native App Info
  const [activeSection, setActiveSection] = useAppSection();
  const [modInstallTarget, setModInstallTarget] = useState<GameInstance | null>(null);
  const requestedInstanceSelect = null;
  const [filtersOpen, setFiltersOpen] = useState(false);
  const [installToProfileItem, setInstallToProfileItem] = useState<ProjectCard | null>(null);
  const [busyGroupIdSet, setBusyGroupIdSet] = useState<Set<string>>(() => new Set());
  const [armedContentTick, setArmedContentTick] = useState(0);
  const [armedContent, setArmedContent] = useState<{
    instanceId: string;
    mods: string[];
    resourcepacks: string[];
    shaders: string[];
    byProjectId: Record<string, InstalledModInfo>;
  } | null>(null);
  const [appInfo, setAppInfo] = useState<DesktopAppInfo | null>(null);
  const isDesktop = desktopBridge.isDesktop();
  const downloads = useDownloads();
  const instanceMgr = useInstances();
  const accountMgr = useAccounts();
  const launcher = useLauncher();

  const runningInstanceIds = useMemo(() => {
    const ids = new Set<string>();
    for (const [instId, sess] of Object.entries(launcher.sessionsByInstance)) {
      if (isSessionActive(sess.state)) {
        ids.add(instId);
      }
    }
    return ids;
  }, [launcher.sessionsByInstance]);

  const activeRunningSessionEntry = useMemo(() => {
    for (const [instId, sess] of Object.entries(launcher.sessionsByInstance)) {
      if (isSessionActive(sess.state)) {
        const inst = instanceMgr.instances.find((i) => i.id === instId);
        return { session: sess, name: inst?.name || instId };
      }
    }
    return null;
  }, [launcher.sessionsByInstance, instanceMgr.instances]);

  const prevRunningCountRef = useRef(launcher.runningCount);
  useEffect(() => {
    const prev = prevRunningCountRef.current;
    const curr = launcher.runningCount;
    prevRunningCountRef.current = curr;

    if (prev === 0 && curr > 0) {
      const autoPhantom = localStorage.getItem("modhub_auto_phantom");
      if (autoPhantom === "true") {
        desktopBridge.enterPhantomMode().catch(() => {});
        pushToast("Phantom Mode: окно ModHub свернуто, RAM сброшена до ~15 МБ", "info");
      } else {
        desktopBridge.trimMemory().catch(() => {});
      }
    } else if (prev > 0 && curr === 0) {
      desktopBridge.showWindow().catch(() => {});
      pushToast("Игровая сессия завершена. ModHub снова активен!", "ok");
    }
  }, [launcher.runningCount]);

  // Overlays
  const [mobileSidebarOpen, setMobileSidebarOpen] = useState(false);
  const [activeMod, setActiveMod] = useState<ProjectCard | null>(null);
  const [activeModDetails, setActiveModDetails] = useState<ModDetailsResponse | null>(null);
  const [activeModTab, setActiveModTab] = useState<ModalTab>("description");
  const [modalHistory, setModalHistory] = useState<ProjectCard[]>([]);
  const [quickDownloadItem, setQuickDownloadItem] = useState<ProjectCard | null>(null);
  const [quickDownloadDetails, setQuickDownloadDetails] = useState<ModDetailsResponse | null>(null);
  const [preloadingTitle, setPreloadingTitle] = useState("");
  const preloadAbortRef = useRef<AbortController | null>(null);
  const preloadDelayTimerRef = useRef<number | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [aboutOpen, setAboutOpen] = useState(false);
  const [lightbox, setLightbox] = useState<{ items: GalleryItem[]; index: number }>({ items: [], index: -1 });

  // Mount-once flags so lazy chunks load on demand yet stay mounted to play smooth close transitions
  const [modModalEver, setModModalEver] = useState(false);
  const [qdModalEver, setQdModalEver] = useState(false);
  const [settingsEver, setSettingsEver] = useState(false);
  const [aboutEver, setAboutEver] = useState(false);
  const [lightboxEver, setLightboxEver] = useState(false);

  useEffect(() => { if (activeMod) setModModalEver(true); }, [activeMod]);
  useEffect(() => { if (quickDownloadItem) setQdModalEver(true); }, [quickDownloadItem]);
  useEffect(() => { if (settingsOpen) setSettingsEver(true); }, [settingsOpen]);
  useEffect(() => { if (aboutOpen) setAboutEver(true); }, [aboutOpen]);
  useEffect(() => { if (lightbox.index >= 0) setLightboxEver(true); }, [lightbox.index]);
  const [ctxMenu, setCtxMenu] = useState<{ x: number; y: number; item: ProjectCard } | null>(null);

  useEffect(() => {
    const onGlobalKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && !e.shiftKey && !e.altKey && !document.activeElement?.closest('[aria-modal="true"]')) {
        if (e.key === "1") { e.preventDefault(); setActiveSection("home"); }
        else if (e.key === "2") { e.preventDefault(); setActiveSection("instances"); }
        else if (e.key === "3") { e.preventDefault(); setActiveSection("catalog"); }
        else if (e.key === "4") { e.preventDefault(); setActiveSection("downloads"); }
        else if (e.key === "5") { e.preventDefault(); setActiveSection("accounts"); }
        else if (e.key === "6") { e.preventDefault(); setActiveSection("settings"); }
      }
      if (e.key === "Escape") {
        if (installToProfileItem) setInstallToProfileItem(null);
        else if (activeMod) setActiveMod(null);
        else if (quickDownloadItem) setQuickDownloadItem(null);
        else if (settingsOpen) setSettingsOpen(false);
        else if (aboutOpen) setAboutOpen(false);
        else if (ctxMenu) setCtxMenu(null);
      }
    };
    window.addEventListener("keydown", onGlobalKey);
    return () => window.removeEventListener("keydown", onGlobalKey);
  }, [activeMod, quickDownloadItem, installToProfileItem, settingsOpen, aboutOpen, ctxMenu, setActiveSection]);

  // Suppress default web browser context menu in WebView2 for native desktop feel
  useEffect(() => {
    const handleGlobalContextMenu = (e: MouseEvent) => {
      const target = e.target as HTMLElement | null;
      if (
        target instanceof HTMLInputElement ||
        target instanceof HTMLTextAreaElement ||
        target?.isContentEditable
      ) {
        return; // Allow native copy/paste context menu on text inputs
      }
      if (target?.closest(".mod-card") || target?.closest(".card-ctx-menu") || target?.closest("[data-context-menu]")) {
        return; // Handled by custom context menus
      }
      e.preventDefault();
    };
    window.addEventListener("contextmenu", handleGlobalContextMenu);
    return () => window.removeEventListener("contextmenu", handleGlobalContextMenu);
  }, []);

  const searchSeqRef = useRef(0);
  const abortRef = useRef<AbortController | null>(null);
  const sentinelRef = useRef<HTMLDivElement | null>(null);
  const loadingMoreLockRef = useRef(false);

  // Smooth snappy vertical scrolling
  useEffect(() => initSmoothScroll(), []);

  // O(1) busy lookup indices
  const quickBusySet = busyGroupIdSet;

  // Global background scroll lock whenever ANY overlay is open
  const hasOpenOverlay = Boolean(
    activeMod ||
      quickDownloadItem ||
      installToProfileItem ||
      Boolean(preloadingTitle) ||
      settingsOpen ||
      aboutOpen ||
      lightbox.index >= 0 ||
      mobileSidebarOpen
  );

  useEffect(() => {
    setBackgroundScrollLocked(hasOpenOverlay);
    return () => {
      setBackgroundScrollLocked(false);
    };
  }, [hasOpenOverlay]);



  useEffect(() => {
    const t = window.setTimeout(() => {
      try { localStorage.setItem(RECENT_KEY, JSON.stringify(recentProjects)); } catch {}
    }, 120);
    return () => window.clearTimeout(t);
  }, [recentProjects]);

  useEffect(() => {
    const t = window.setTimeout(() => {
      try {
        const snapshot: PersistedFilters = {
          contentType,
          version,
          loadersIn,
          loadersOut,
          categoriesIn,
          categoriesOut,
          catMode,
          env,
          authorFilter,
          minDownloads,
          updatedWithin,
          hideAbandoned,
          sort,
          srcModrinth,
          srcCurseforge,
        };
        localStorage.setItem(FILTERS_KEY, JSON.stringify(snapshot));
      } catch {}
    }, 140);
    return () => window.clearTimeout(t);
  }, [
    contentType,
    version,
    loadersIn,
    loadersOut,
    categoriesIn,
    categoriesOut,
    catMode,
    env,
    authorFilter,
    minDownloads,
    updatedWithin,
    hideAbandoned,
    sort,
    srcModrinth,
    srcCurseforge,
  ]);

  const effectiveQuery = useMemo(() => {
    const parts: string[] = [];
    const baseQ = debouncedQuery.trim();
    if (baseQ) parts.push(baseQ);
    const auth = authorFilter.trim();
    if (auth && !/(?:^|\s)author:/i.test(baseQ)) {
      parts.push(auth.includes(" ") ? `author:"${auth}"` : `author:${auth}`);
    }
    if (minDownloads > 0 && !/(?:^|\s)(?:dl|downloads):/i.test(baseQ)) {
      parts.push(`dl:>=${minDownloads}`);
    }
    return parts.join(" ").trim();
  }, [debouncedQuery, authorFilter, minDownloads]);

  useEffect(() => {
    api
      .getConfig()
      .then(setConfig)
      .catch(() => {})
      .finally(() => setConfigReady(true));
    api
      .getMcVersions()
      .then((r) => {
        setMcVersionsAll(r.versions || []);
        setMcVersionsByLoader(r.by_loader || {});
      })
      .catch(() => {});
    desktopBridge
      .getAppInfo()
      .then((info) => {
        if (info) setAppInfo(info);
      })
      .catch(() => {});
  }, []);

  // Sync actually installed mods, resourcepacks, and shaderpacks for the currently armed profile
  useEffect(() => {
    if (!modInstallTarget?.id) {
      setArmedContent(null);
      return;
    }
    const targetId = modInstallTarget.id;
    let alive = true;

    Promise.all([
      api.listInstanceMods(targetId).catch(() => []),
      api.listInstanceResourcePacks(targetId).catch(() => []),
      api.listInstanceShaderPacks(targetId).catch(() => []),
    ]).then(([mods, packs, shaders]) => {
      if (!alive) return;
      setArmedContent((prev) => ({
        instanceId: targetId,
        mods: mods.map((m) => m.filename.toLowerCase()),
        resourcepacks: packs.map((p) => p.filename.toLowerCase()),
        shaders: shaders.map((s) => s.filename.toLowerCase()),
        byProjectId: prev?.instanceId === targetId ? prev.byProjectId : {},
      }));
    });

    // Enrich Modrinth project IDs by SHA-1 in background (non-blocking)
    api
      .checkInstanceModUpdates(targetId)
      .then((updates) => {
        if (!alive || !Array.isArray(updates)) return;
        const map: Record<string, InstalledModInfo> = {};
        for (const u of updates) {
          if (u.project_id) {
            map[u.project_id] = {
              installed_version_id: u.new_version_id || "local",
              installed_version_number: u.current_version || "локально",
              has_update: Boolean(u.has_update),
              latest_version_number: u.new_version_number || u.current_version || "",
              filename: u.current_filename,
            };
          }
        }
        setArmedContent((prev) =>
          prev && prev.instanceId === targetId
            ? { ...prev, byProjectId: { ...prev.byProjectId, ...map } }
            : prev
        );
      })
      .catch(() => {});

    return () => {
      alive = false;
    };
  }, [modInstallTarget?.id, armedContentTick]);

  // Subscribe to Go EventBus events forwarded via Wails Runtime Events
  useEffect(() => {
    return subscribeBackendEvents((ev) => {
      if (ev.name === "config.updated" && ev.payload && typeof ev.payload === "object") {
        setConfig(ev.payload as PublicConfig);
        void accountMgr.refresh();
      } else if (ev.name === "cache.cleared") {
        api.clearClientCache();
      } else if (ev.name === "download.completed" || ev.name === "instance.updated") {
        if (modInstallTarget?.id) {
          setArmedContentTick((t) => t + 1);
        }
        if (ev.name === "download.completed") {
          void instanceMgr.refresh();
        }
      }
    });
  }, [modInstallTarget?.id, accountMgr.refresh, instanceMgr.refresh]);

  const handleConfigSaved = useCallback(
    (nextCfg: PublicConfig) => {
      setConfig(nextCfg);
      void accountMgr.refresh();
    },
    [accountMgr.refresh]
  );

  // Sync active section data when switching tabs or returning window focus (keeps Entra ID, downloads, accounts, instances fresh)
  const syncActiveSection = useCallback(
    (sec: AppSection) => {
      if (sec === "home" || sec === "settings" || sec === "accounts") {
        api.getConfig().then(setConfig).catch(() => {});
      }
      if (sec === "accounts" || sec === "home" || sec === "instances") {
        void accountMgr.refresh();
      }
      if (sec === "instances" || sec === "home") {
        void instanceMgr.refresh();
        void launcher.refresh();
      }
      if (sec === "downloads" || sec === "home") {
        void downloads.refresh();
      }
      if (sec === "catalog" && modInstallTarget?.id) {
        setArmedContentTick((t) => t + 1);
      }
    },
    [accountMgr.refresh, instanceMgr.refresh, launcher.refresh, downloads.refresh, modInstallTarget?.id]
  );

  useEffect(() => {
    const onFocusOrVisible = () => {
      if (document.visibilityState === "hidden") return;
      syncActiveSection(activeSection);
    };
    window.addEventListener("focus", onFocusOrVisible);
    document.addEventListener("visibilitychange", onFocusOrVisible);
    return () => {
      window.removeEventListener("focus", onFocusOrVisible);
      document.removeEventListener("visibilitychange", onFocusOrVisible);
    };
  }, [activeSection, syncActiveSection]);

  useEffect(() => {
    api.getFilters(contentType).then(setAvailableCategories).catch(() => setAvailableCategories([]));
  }, [contentType]);

  const visibleMcVersions = useMemo(() => {
    if (contentType === "mod" && loadersIn.length > 0) {
      const union = new Set<string>();
      for (const l of loadersIn) {
        for (const v of mcVersionsByLoader[l] || []) union.add(v);
      }
      if (union.size > 0) return mcVersionsAll.filter((v) => union.has(v));
    }
    return mcVersionsAll;
  }, [contentType, loadersIn, mcVersionsAll, mcVersionsByLoader]);

  const enabledSources = useMemo(() => {
    const arr: string[] = [];
    if (srcModrinth) arr.push("modrinth");
    if (srcCurseforge) arr.push("curseforge");
    return arr.length ? arr : ["modrinth"];
  }, [srcModrinth, srcCurseforge]);

  const targetCompatMc = useMemo(() => {
    if (version) return version;
    return "";
  }, [version]);

  const showLoadersCol = contentType === "mod" || contentType === "datapack";

  const targetCompatLoader = useMemo(() => {
    if (!showLoadersCol) return "";
    if (loadersIn.length === 1) return loadersIn[0];
    return "";
  }, [showLoadersCol, loadersIn]);

  useEffect(() => {
    if (!isLoading) {
      setSlowLoading(false);
      return;
    }
    const t = window.setTimeout(() => setSlowLoading(true), 240);
    return () => window.clearTimeout(t);
  }, [isLoading]);

  const recordRecent = useCallback((item: ProjectCard) => {
    if (!item?.group_id || item.content_type === "modpack") return;
    setRecentProjects((prev) => [item, ...prev.filter((x) => x.group_id !== item.group_id)].slice(0, 14));
  }, []);

  const applyMergedItemUpdate = useCallback((origGroupId: string, merged: ProjectCard) => {
    if (!merged || !merged.group_id) return;
    const mergedKeys = new Set((merged.sources || []).map((s) => s.source_key));
    const deduped = sessionDeduperRef.current.updateMerged(origGroupId, merged);
    setResults((prev) => {
      if (deduped.length > 0 && prev.length === deduped.length) {
        return deduped;
      }
      return prev.map((it) => {
        if (it.group_id === origGroupId || (it.sources || []).some((s) => mergedKeys.has(s.source_key))) {
          return { ...merged, dependent_info: it.dependent_info || merged.dependent_info };
        }
        return it;
      });
    });
    setRecentProjects((prev) =>
      prev.map((it) =>
        it.group_id === origGroupId || (it.sources || []).some((s) => mergedKeys.has(s.source_key))
          ? merged
          : it
      )
    );
  }, []);

  const executeFetch = useCallback(
    async (append: boolean) => {
      if (!append) {
        abortRef.current?.abort();
        abortRef.current = new AbortController();
        loadingMoreLockRef.current = false;
        setIsLoading(true);
        setIsLoadingMore(false);
      } else {
        if (loadingMoreLockRef.current || isLoading || isLoadingMore || !hasMore) return;
        loadingMoreLockRef.current = true;
        setIsLoadingMore(true);
      }

      const seq = ++searchSeqRef.current;
      const signal = abortRef.current?.signal;

      try {
        if (dependentsTarget) {
          const depSort = sort === "relevance" ? "default" : sort;
          const res = await api.getDependents(
            {
              item: dependentsTarget,
              relation: dependentsRelation,
              project_filter: dependentsProjectFilter,
              content_type: contentType,
              q: effectiveQuery,
              mc_version: version,
              loaders_in: loadersIn,
              loaders_out: loadersOut,
              categories: categoriesIn,
              categories_out: categoriesOut,
              cat_mode: catMode,
              env: contentType === "mod" ? env : "",
              updated_within: updatedWithin,
              hide_abandoned: hideAbandoned,
              sources: enabledSources,
              sort: depSort,
              cf_index: append ? depsNextCfIndex : 0,
              page_size: 50,
            },
            signal
          );
          if (seq !== searchSeqRef.current) return;
          if (append) {
            setResults((prev) => {
              const seen = new Set(prev.map((x) => x.group_id));
              return [...prev, ...(res.items || []).filter((x) => !seen.has(x.group_id))];
            });
            setDepsSkippedModpacks((p) => p + Number(res.skipped_modpacks || 0));
            setDepsTotalCf((p) => Math.max(p, Number(res.total_cf || 0)));
            setDepsCfScanned((p) => p + Number(res.cf_scanned || 0));
            if (res.counts_by_type) {
              setDepsCountsByType((prev) => {
                const next: Record<string, number> = { ...prev };
                for (const [k, v] of Object.entries(res.counts_by_type || {})) {
                  next[k] = (next[k] || 0) + Number(v || 0);
                }
                return next;
              });
            }
          } else {
            setResults((prev) => stabilizeCards(prev, res.items || []));
            setDepsSkippedModpacks(Number(res.skipped_modpacks || 0));
            setDepsTotalCf(Number(res.total_cf || 0));
            setDepsCfScanned(Number(res.cf_scanned || 0));
            setDepsCountsByType(res.counts_by_type || {});
          }
          setDepsSourceMode(res.source_mode || "cf_web");
          setDepsCfUrl(res.cf_dependents_url || "");
          setDepsNextCfIndex(Number(res.next_cf_index || 0));
          setHasMore(Boolean(res.has_more));
          setCfStatus(res.curseforge_status || "disabled");
          setFuzzySuggestion("");
          setApiErrors(res.errors || {});
          return;
        }

        if (!append) {
          sessionDeduperRef.current.reset();
          mrHasMoreRef.current = enabledSources.includes("modrinth");
          cfHasMoreRef.current = enabledSources.includes("curseforge") && Boolean(config.curseforge_configured);
        }

        const baseSearchParams = {
          q: effectiveQuery,
          type: contentType,
          version,
          loaders_in: loadersIn.join(","),
          loaders_out: loadersOut.join(","),
          categories: categoriesIn.join(","),
          categories_out: categoriesOut.join(","),
          cat_mode: catMode,
          env: contentType === "mod" ? env : "",
          updated_within: updatedWithin || undefined,
          hide_abandoned: hideAbandoned || undefined,
          sort,
          merge: "1",
          page: append ? 2 : 1,
          limit: 24,
        };

        const runDualProgressive =
          enabledSources.includes("modrinth") &&
          enabledSources.includes("curseforge") &&
          Boolean(config.curseforge_configured) &&
          !/(?:^|\s)dep:/i.test(effectiveQuery);

        if (runDualProgressive) {
          const runMR = !append || mrHasMoreRef.current;
          const runCF = !append || cfHasMoreRef.current;
          const combinedErrors: Record<string, string> = {};
          let firstArrived = false;
          let latestCfStatus = cfStatus;

          const jobs: Promise<void>[] = [];
          const handlePlatformBatch = (plat: "modrinth" | "curseforge", res: Awaited<ReturnType<typeof api.search>>) => {
            if (seq !== searchSeqRef.current || signal?.aborted) return;
            const ingestRes = sessionDeduperRef.current.ingest(res.items || [], !append ? sort : undefined);
            if (!append && !firstArrived) {
              firstArrived = true;
              setResults((prev) => stabilizeCards(prev, ingestRes.items));
              setIsLoading(false);
            } else {
              setResults(ingestRes.items);
            }
            if (plat === "modrinth") {
              setNextMrOffset(res.next_mr_offset || 0);
              mrHasMoreRef.current = Boolean(res.has_more);
            } else {
              setNextCfOffset(res.next_cf_offset || 0);
              cfHasMoreRef.current = Boolean(res.has_more);
              latestCfStatus = res.curseforge_status || "ok";
              setCfStatus(latestCfStatus);
            }
            if (res.fuzzy_suggestion) setFuzzySuggestion(res.fuzzy_suggestion);
            if (res.errors && Object.keys(res.errors).length > 0) {
              Object.assign(combinedErrors, res.errors);
              setApiErrors({ ...combinedErrors });
            }
            setHasMore(mrHasMoreRef.current || cfHasMoreRef.current);
          };

          if (runMR) {
            jobs.push(
              api
                .search(
                  { ...baseSearchParams, sources: "modrinth", mr_offset: append ? nextMrOffset : 0, cf_offset: 0 },
                  signal
                )
                .then((res) => handlePlatformBatch("modrinth", res))
                .catch((err) => {
                  if ((err as Error).name === "AbortError" || seq !== searchSeqRef.current) return;
                  mrHasMoreRef.current = false;
                  combinedErrors.modrinth = String((err as Error).message || err);
                  setApiErrors({ ...combinedErrors });
                })
            );
          }
          if (runCF) {
            jobs.push(
              api
                .search(
                  { ...baseSearchParams, sources: "curseforge", mr_offset: 0, cf_offset: append ? nextCfOffset : 0 },
                  signal
                )
                .then((res) => handlePlatformBatch("curseforge", res))
                .catch((err) => {
                  if ((err as Error).name === "AbortError" || seq !== searchSeqRef.current) return;
                  cfHasMoreRef.current = false;
                  combinedErrors.curseforge = String((err as Error).message || err);
                  setApiErrors({ ...combinedErrors });
                })
            );
          }

          await Promise.all(jobs);
          if (seq !== searchSeqRef.current) return;
          if (!firstArrived && !append) {
            setResults(sessionDeduperRef.current.getItems());
          }
          setHasMore(mrHasMoreRef.current || cfHasMoreRef.current);
          setApiErrors({ ...combinedErrors });
          return;
        }

        const res = await api.search(
          {
            ...baseSearchParams,
            sources: enabledSources.join(","),
            mr_offset: append ? nextMrOffset : 0,
            cf_offset: append ? nextCfOffset : 0,
          },
          signal
        );
        if (seq !== searchSeqRef.current) return;
        const ingestRes = sessionDeduperRef.current.ingest(res.items || []);
        if (append) {
          setResults(ingestRes.items);
        } else {
          setResults((prev) => stabilizeCards(prev, ingestRes.items));
        }
        setHasMore(Boolean(res.has_more));
        setNextMrOffset(res.next_mr_offset || 0);
        setNextCfOffset(res.next_cf_offset || 0);
        setFuzzySuggestion(res.fuzzy_suggestion || "");
        setCfStatus(res.curseforge_status || "disabled");
        setApiErrors(res.errors || {});
      } catch (err) {
        if ((err as Error).name === "AbortError" || seq !== searchSeqRef.current) return;
        setApiErrors({ api: String((err as Error).message || err) });
      } finally {
        if (seq === searchSeqRef.current) {
          loadingMoreLockRef.current = false;
          setIsLoading(false);
          setIsLoadingMore(false);
        }
      }
    },
    [
      dependentsTarget,
      dependentsRelation,
      dependentsProjectFilter,
      effectiveQuery,
      contentType,
      version,
      loadersIn,
      loadersOut,
      categoriesIn,
      categoriesOut,
      catMode,
      env,
      updatedWithin,
      hideAbandoned,
      sort,
      enabledSources,
      config.curseforge_configured,
      cfStatus,
      nextMrOffset,
      nextCfOffset,
      depsNextCfIndex,
      isLoading,
      isLoadingMore,
      hasMore,
    ]
  );

  const loadMoreRef = useRef<() => void>(() => {});
  loadMoreRef.current = () => {
    if (hasMore && !isLoading && !isLoadingMore && !loadingMoreLockRef.current) {
      executeFetch(true);
    }
  };

  useEffect(() => {
    if (!configReady) return;
    executeFetch(false);
  }, [
    configReady,
    effectiveQuery,
    retryTick,
    contentType,
    version,
    loadersIn,
    loadersOut,
    categoriesIn,
    categoriesOut,
    catMode,
    env,
    updatedWithin,
    hideAbandoned,
    sort,
    enabledSources,
    dependentsTarget,
    dependentsRelation,
    dependentsProjectFilter,
    config.curseforge_configured,
    config.proxy_url,
  ]);

  // Dual Automatic Infinite Scroll
  useEffect(() => {
    const el = sentinelRef.current;
    const obs = el
      ? new IntersectionObserver(
          (entries) => {
            if (entries[0]?.isIntersecting) {
              loadMoreRef.current();
            }
          },
          { rootMargin: "650px 0px" }
        )
      : null;
    if (el && obs) obs.observe(el);

    const onScroll = () => {
      const distBottom = getPageScrollBottomDistance();
      if (distBottom < 700) {
        loadMoreRef.current();
      }
    };
    const unsubScroll = subscribePageScroll(onScroll);
    return () => {
      obs?.disconnect();
      unsubScroll();
    };
  }, []);

  useEffect(() => {
    if (!isLoading && !isLoadingMore && hasMore && results.length > 0 && results.length < 48) {
      const distBottom = getPageScrollBottomDistance();
      if (distBottom < 450) {
        loadMoreRef.current();
      }
    }
  }, [results.length, isLoading, isLoadingMore, hasMore]);

  const cancelPreload = useCallback(() => {
    if (preloadDelayTimerRef.current !== null) {
      window.clearTimeout(preloadDelayTimerRef.current);
      preloadDelayTimerRef.current = null;
    }
    preloadAbortRef.current?.abort();
    preloadAbortRef.current = null;
    setPreloadingTitle("");
  }, []);

  useEffect(() => {
    if (!preloadingTitle) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") cancelPreload();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [preloadingTitle, cancelPreload]);

  // Pre-fetches cross-platform merge + ALL versions + full details BEFORE displaying modal/picker,
  // delaying the loading indicator by 160ms so fast responses never flicker!
  const resolveBeforeOpen = useCallback(
    async (item: ProjectCard): Promise<{ item: ProjectCard; details: ModDetailsResponse } | null> => {
      const cached = api.getCachedModDetails(item);
      if (cached) {
        const merged = cached.merged_item || item;
        if (cached.merged_item && (cached.merged_item.sources?.length || 0) > (item.sources?.length || 0)) {
          applyMergedItemUpdate(item.group_id, cached.merged_item);
        }
        return { item: merged, details: cached };
      }

      if (preloadDelayTimerRef.current !== null) {
        window.clearTimeout(preloadDelayTimerRef.current);
        preloadDelayTimerRef.current = null;
      }
      preloadAbortRef.current?.abort();
      const ctrl = new AbortController();
      preloadAbortRef.current = ctrl;

      const label = item.title || "проект";
      preloadDelayTimerRef.current = window.setTimeout(() => {
        preloadDelayTimerRef.current = null;
        if (!ctrl.signal.aborted) {
          setPreloadingTitle(label);
        }
      }, 160);

      try {
        const det = await api.getModDetails(item, ctrl.signal);
        if (ctrl.signal.aborted) return null;
        const merged = det.merged_item || item;
        if (det.merged_item && (det.merged_item.sources?.length || 0) > (item.sources?.length || 0)) {
          applyMergedItemUpdate(item.group_id, det.merged_item);
        }
        return { item: merged, details: det };
      } catch (err) {
        if ((err as Error).name === "AbortError") return null;
        pushToast(`Ошибка загрузки проекта: ${(err as Error).message}`, "err");
        return null;
      } finally {
        if (preloadDelayTimerRef.current !== null) {
          window.clearTimeout(preloadDelayTimerRef.current);
          preloadDelayTimerRef.current = null;
        }
        if (preloadAbortRef.current === ctrl) {
          preloadAbortRef.current = null;
          setPreloadingTitle("");
        }
      }
    },
    [applyMergedItemUpdate]
  );

  const activeFiltersCount = useMemo(() => {
    let count = 0;
    count += categoriesIn.length + categoriesOut.length;
    count += loadersIn.length + loadersOut.length;
    if (version) count += 1;
    if (env) count += 1;
    if (minDownloads > 0) count += 1;
    if (updatedWithin > 0) count += 1;
    if (hideAbandoned) count += 1;
    if (authorFilter.trim()) count += 1;
    if (dependentsTarget) count += 1;
    return count;
  }, [
    categoriesIn,
    categoriesOut,
    loadersIn,
    loadersOut,
    version,
    env,
    minDownloads,
    updatedWithin,
    hideAbandoned,
    authorFilter,
    dependentsTarget,
  ]);

  const resolveInstalledForCard = useCallback(
    (item: ProjectCard): InstalledModInfo | undefined => {
      if (modInstallTarget && armedContent && armedContent.instanceId === modInstallTarget.id) {
        const byProj = armedContent.byProjectId;
        if (byProj[item.group_id]) return byProj[item.group_id];
        for (const s of item.sources || []) {
          if (byProj[s.id]) return byProj[s.id];
          if (s.source_key && byProj[s.source_key]) return byProj[s.source_key];
        }

        const ct = (item.content_type || "mod").toLowerCase();
        const fileList =
          ct === "resourcepack"
            ? armedContent.resourcepacks
            : ct === "shader"
              ? armedContent.shaders
              : ct === "mod"
                ? armedContent.mods
                : [];

        if (fileList.length > 0) {
          const cached = api.getCachedModDetails(item);
          const knownNames = cached?.versions
            ? new Set(cached.versions.map((v) => (v.filename || "").toLowerCase()).filter(Boolean))
            : null;
          const slugLow = (item.slug || "").toLowerCase().trim();
          const titleClean = item.title.toLowerCase().replace(/[^a-z0-9]/g, "");

          for (const rawFn of fileList) {
            const clean = rawFn.replace(/\.disabled$/, "");
            if (knownNames?.has(clean)) {
              return {
                installed_version_id: "profile",
                installed_version_number: "в профиле",
                has_update: false,
                latest_version_number: "",
                filename: rawFn,
              };
            }
            if (
              slugLow.length >= 3 &&
              (clean.startsWith(`${slugLow}-`) ||
                clean.startsWith(`${slugLow}_`) ||
                clean.startsWith(`${slugLow}+`) ||
                clean === `${slugLow}.jar` ||
                clean === `${slugLow}.zip`)
            ) {
              return {
                installed_version_id: "profile",
                installed_version_number: "в профиле",
                has_update: false,
                latest_version_number: "",
                filename: rawFn,
              };
            }
            const stemClean = clean.replace(/\.(jar|zip)$/, "").replace(/[^a-z0-9]/g, "");
            if (titleClean.length >= 5 && (stemClean === titleClean || stemClean.startsWith(titleClean))) {
              return {
                installed_version_id: "profile",
                installed_version_number: "в профиле",
                has_update: false,
                latest_version_number: "",
                filename: rawFn,
              };
            }
          }
        }
        return undefined;
      }

      if (installedMap[item.group_id]) return installedMap[item.group_id];
      for (const s of item.sources || []) {
        if (installedMap[s.id]) return installedMap[s.id];
        if (s.source_key && installedMap[s.source_key]) return installedMap[s.source_key];
      }
      return undefined;
    },
    [modInstallTarget, armedContent, installedMap]
  );

  const handleToggleFilters = useCallback(() => {
    setFiltersOpen((prev) => !prev);
  }, []);

  const handlePrimaryActionImpl = async (item: ProjectCard, originEl?: HTMLElement | null) => {
    void originEl;
    if (modInstallTarget) {
      const alreadyInstalled = resolveInstalledForCard(item);
      if (alreadyInstalled && !alreadyInstalled.has_update) {
        pushToast(`«${item.title}» уже установлен в профиль «${modInstallTarget.name}»`, "info");
        return;
      }

      const gid = item.group_id;
      setBusyGroupIdSet((prev) => new Set(prev).add(gid));
      try {
        const result = await autoInstallModIntoInstance({
          instance: modInstallTarget,
          project: item,
          onToast: pushToast,
        });
        pushToast(
          `✓ «${item.title}» успешно установлен в профиль «${modInstallTarget.name}» (файлов: ${result.queued})`,
          "ok"
        );
        const info: InstalledModInfo = {
          installed_version_id: "local",
          filename: `${item.slug || item.title}.${item.content_type === "mod" ? "jar" : "zip"}`,
          installed_version_number: "latest",
          latest_version_number: "latest",
          has_update: false,
        };
        setArmedContent((prev) => {
          const base = prev && prev.instanceId === modInstallTarget.id
            ? prev
            : { instanceId: modInstallTarget.id, mods: [], resourcepacks: [], shaders: [], byProjectId: {} };
          const nextByProj: Record<string, InstalledModInfo> = { ...base.byProjectId, [item.group_id]: info };
          for (const s of item.sources || []) {
            if (s.id) nextByProj[s.id] = info;
            if (s.source_key) nextByProj[s.source_key] = info;
          }
          return { ...base, byProjectId: nextByProj };
        });
        window.setTimeout(() => setArmedContentTick((t) => t + 1), 650);
      } catch (err) {
        pushToast(err instanceof Error ? err.message : String(err), "err");
      } finally {
        setBusyGroupIdSet((prev) => {
          const next = new Set(prev);
          next.delete(gid);
          return next;
        });
      }
      return;
    }

    setInstallToProfileItem(item);
  };

  const handlePrimaryAction = useCallback((item: ProjectCard, originEl?: HTMLElement | null) => {
    handlePrimaryActionImpl(item, originEl);
  }, [modInstallTarget, resolveInstalledForCard]);

  const handleScanInstalledFiles = async (fileList: FileList) => {
    const files = Array.from(fileList).filter((f) => /\.(jar|zip)$/i.test(f.name)).slice(0, 400);
    if (!files.length) {
      pushToast("Файлы .jar / .zip не найдены", "err");
      return;
    }
    pushToast(`Сканируем ${files.length} файлов (SHA-1)...`, "info");
    try {
      const hashes = await Promise.all(files.map(sha1FileHex));
      const res = await api.checkInstalledHashes(hashes, version, loadersIn[0] || "");
      setInstalledMap(res.installed || {});
      pushToast(`Распознано установленных модов: ${res.matched_count}`, "ok");
    } catch (err) {
      pushToast(`Ошибка сканирования: ${(err as Error).message}`, "err");
    }
  };

  const handleScanInstalledNative = useCallback(async () => {
    try {
      const res = await desktopBridge.scanInstalledFolder(version, loadersIn[0] || "");
      if (!res || res.cancelled) return;
      if (res.scanned_files === 0) {
        pushToast("Файлы .jar / .zip не найдены в выбранной папке", "err");
        return;
      }
      setInstalledMap(res.installed || {});
      pushToast(`Проверено ${res.scanned_files} файлов, распознано модов: ${res.matched_count}`, "ok");
    } catch (err) {
      pushToast(`Ошибка сканирования: ${(err as Error).message}`, "err");
    }
  }, [version, loadersIn]);

  const handleUrlPaste = async (rawUrl: string) => {
    setPreloadingTitle("проект по ссылке");
    try {
      const card = await api.resolveUrl(rawUrl);
      setPreloadingTitle("");
      const resolved = await resolveBeforeOpen(card);
      if (!resolved) return;
      recordRecent(resolved.item);
      setModalHistory([]);
      setActiveModTab("description");
      setActiveModDetails(resolved.details);
      setActiveMod(resolved.item);
    } catch (err) {
      setPreloadingTitle("");
      pushToast(`Ошибка ссылки: ${(err as Error).message}`, "err");
    }
  };

  const handleTriState = (
    id: string,
    action: "toggle_in" | "toggle_out",
    inArr: string[],
    outArr: string[],
    setIn: React.Dispatch<React.SetStateAction<string[]>>,
    setOut: React.Dispatch<React.SetStateAction<string[]>>
  ) => {
    if (action === "toggle_out") {
      setIn(inArr.filter((x) => x !== id));
      setOut(outArr.includes(id) ? outArr.filter((x) => x !== id) : [...outArr, id]);
    } else {
      if (outArr.includes(id)) setOut(outArr.filter((x) => x !== id));
      else setIn(inArr.includes(id) ? inArr.filter((x) => x !== id) : [...inArr, id]);
    }
  };

  const activateDependentsMode = (target: ProjectCard, opts?: { relation?: string; projectFilter?: string; contentType?: ContentType }) => {
    setDebouncedQuery("");
    if (opts?.contentType) setContentType(opts.contentType);
    if (opts?.relation) setDependentsRelation(opts.relation);
    if (opts?.projectFilter) setDependentsProjectFilter(opts.projectFilter);
    setDepsCountsByType({});
    setDepsCfScanned(0);
    setDepsNextCfIndex(0);
    setDependentsTarget(target);
    scrollToTopSmooth();
  };

  const resetAllFilters = useCallback(() => {
    setDebouncedQuery("");
    setVersion("");
    setLoadersIn([]);
    setLoadersOut([]);
    setCategoriesIn([]);
    setCategoriesOut([]);
    setCatMode("and");
    setEnv("");
    setAuthorFilter("");
    setMinDownloads(0);
    setUpdatedWithin(0);
    setHideAbandoned(false);
    setSort("downloads");
    setSrcModrinth(true);
    setSrcCurseforge(true);
    setDependentsTarget(null);
    scrollToTopSmooth();
  }, []);

  const handleSwitchContentType = useCallback((ct: ContentType) => {
    if (ct === contentType) return;
    abortRef.current?.abort();
    searchSeqRef.current += 1;
    sessionDeduperRef.current.reset();
    loadingMoreLockRef.current = false;
    setResults([]);
    setHasMore(false);
    setNextMrOffset(0);
    setNextCfOffset(0);
    setFuzzySuggestion("");
    setApiErrors({});
    setIsLoadingMore(false);
    setSlowLoading(false);
    setIsLoading(true);
    setContentType(ct);
    setLoadersIn(
      (ct === "mod" || ct === "datapack") && modInstallTarget && modInstallTarget.instance_type !== "vanilla"
        ? [modInstallTarget.instance_type]
        : []
    );
    setLoadersOut([]);
    setCategoriesIn([]);
    setCategoriesOut([]);
    setEnv("");
    scrollToTopSmooth();
  }, [contentType, modInstallTarget]);

  const openCard = useCallback(
    (item: ProjectCard, tab: ModalTab = "description") => {
      if (item.content_type === "modpack" && item.sources?.[0]?.url) {
        window.open(item.sources[0].url, "_blank", "noopener,noreferrer");
        return;
      }
      void (async () => {
        const resolved = await resolveBeforeOpen(item);
        if (!resolved) return;
        recordRecent(resolved.item);
        setModalHistory([]);
        setActiveModTab(tab);
        setActiveModDetails(resolved.details);
        setActiveMod(resolved.item);
      })();
    },
    [recordRecent, resolveBeforeOpen]
  );

  const installedCount = useMemo(() => Object.keys(installedMap).length, [installedMap]);
  const handleQueryCommit = useCallback((q: string) => {
    setDebouncedQuery(q);
    scrollToTopSmooth();
  }, []);
  const handleOpenMobileSidebar = useCallback(() => setMobileSidebarOpen(true), []);
  const handleOpenSettings = useCallback(() => setSettingsOpen(true), []);
  const previousSection = useRef(activeSection);
  useEffect(() => {
    if (previousSection.current === activeSection) return;
    previousSection.current = activeSection;
    setMobileSidebarOpen(false);
    setPageScrollTop(0);
    syncActiveSection(activeSection);
  }, [activeSection, syncActiveSection]);
  const handleSelectSection = useCallback((sec: AppSection) => {
    setMobileSidebarOpen(false);
    setActiveSection(sec);
    if (sec === activeSection) syncActiveSection(sec);
  }, [activeSection, setActiveSection, syncActiveSection]);
  const handleOpenAbout = useCallback(() => setAboutOpen(true), []);

  const handleAuthorFilter = useCallback((a: string) => {
    setDependentsTarget(null);
    setDebouncedQuery(`author:${a}`);
    scrollToTopSmooth();
  }, []);

  const handleCategoryTagAdd = useCallback((cid: string) => {
    setCategoriesOut((p) => p.filter((x) => x !== cid));
    setCategoriesIn((p) => (p.includes(cid) ? p : [...p, cid]));
    scrollToTopSmooth();
  }, []);

  const handleCardContextMenu = useCallback((e: React.MouseEvent, it: ProjectCard) => {
    e.preventDefault();
    setCtxMenu({ x: e.clientX, y: e.clientY, item: it });
  }, []);

  const cfDotState: "ok" | "warn" | "err" =
    cfStatus === "error" ? "err" : config.curseforge_configured ? "ok" : "warn";

  const showStatsBar = Boolean(fuzzySuggestion || Object.keys(apiErrors).length > 0);
  const showCfNotice = srcCurseforge && cfStatus === "no_key";

  return (
    <div className="mh-app-root">
      <TitleBar
        appVersion={appInfo?.version || "0.9.0"}
        activeSession={activeRunningSessionEntry?.session || null}
        activeInstanceName={activeRunningSessionEntry?.name}
        onTriggerPhantom={async () => {
          try {
            await desktopBridge.enterPhantomMode();
            pushToast("Phantom Mode: окно свернуто в трей, RAM освобождена", "info");
          } catch (err) {
            console.warn("Phantom mode error:", err);
          }
        }}
      />

      <div className="mh-shell">
        <AppNavRail
        activeSection={activeSection}
        onSelectSection={handleSelectSection}
        instancesCount={instanceMgr.instances.length}
        modInstallTarget={modInstallTarget}
        runningInstanceIds={runningInstanceIds}
        downloadSummary={downloads.summary}
        accounts={accountMgr.accounts}
        activeAccount={accountMgr.activeAccount}
        onSelectAccount={(accountId) => {
          void accountMgr.setActive(accountId);
        }}
        cfStatus={cfDotState}
        isDesktop={isDesktop}
        theme={theme}
        onToggleTheme={handleToggleTheme}
        onOpenAbout={handleOpenAbout}
      />

      <div className="mh-shell-workspace">
        <ErrorBoundary resetKey={activeSection}>
          <Suspense fallback={<div className="empty-state" style={{ margin: "32px auto" }}>Загрузка раздела…</div>}>
        {activeSection === "home" && (
          <HomePage
            appInfo={appInfo}
            config={config}
            instancesCount={instanceMgr.instances.length}
            runningGamesCount={launcher.runningCount}
            activeAccount={accountMgr.activeAccount}
            downloadSummary={downloads.summary}
            recentProjects={recentProjects}
            onNavigate={handleSelectSection}
            onOpenProject={openCard}
            onOpenAbout={handleOpenAbout}
          />
        )}

        {activeSection === "instances" && (
          <InstancesPage
            instances={instanceMgr.instances}
            paths={instanceMgr.paths}
            loading={instanceMgr.loading}
            loadError={instanceMgr.loadError}
            mcVersions={mcVersionsAll}
            accounts={accountMgr.accounts}
            activeAccount={accountMgr.activeAccount}
            sessionsByInstance={launcher.sessionsByInstance}
            logsByInstance={launcher.logsByInstance}
            systemMem={launcher.systemMem}
            onCreate={instanceMgr.create}
            onUpdate={instanceMgr.update}
            onDuplicate={instanceMgr.duplicate}
            onDelete={instanceMgr.remove}
            onRefresh={instanceMgr.refresh}
            onVerify={instanceMgr.verify}
            onRepair={instanceMgr.repair}
            onLaunch={launcher.launch}
            onStop={launcher.stop}
            onLoadLogs={launcher.loadInstanceLogs}
            onClearDisplayLogs={launcher.clearDisplayLogs}
            onNavigate={handleSelectSection}
            onToast={pushToast}
            modInstallTarget={modInstallTarget}
            onArmModInstall={(inst) => {
              setModInstallTarget(inst);
              if (inst.mc_version) setVersion(inst.mc_version);
              if (inst.instance_type && inst.instance_type !== "vanilla") {
                setLoadersIn([inst.instance_type]);
              }
              setActiveSection("catalog");
            }}
            onClearModInstall={() => setModInstallTarget(null)}
            requestedInstanceSelect={requestedInstanceSelect}
            activeInstallsById={instanceMgr.activeInstallsById}
          />
        )}

        {activeSection === "downloads" && (
          <DownloadsPage
            tasks={downloads.tasks}
            summary={downloads.summary}
            loading={downloads.loading}
            onPause={downloads.pause}
            onResume={downloads.resume}
            onCancel={downloads.cancel}
            onRetry={downloads.retry}
            onRemove={downloads.remove}
            onClearFinished={downloads.clearFinished}
            onSetMaxConcurrent={downloads.setMaxConcurrent}
            onNavigate={handleSelectSection}
          />
        )}

        {activeSection === "accounts" && (
          <AccountsPage
            accounts={accountMgr.accounts}
            activeAccount={accountMgr.activeAccount}
            authConfig={accountMgr.config}
            flow={accountMgr.flow}
            loading={accountMgr.loading}
            onStartLogin={accountMgr.startLogin}
            onCancelLogin={accountMgr.cancelLogin}
            onAddOffline={accountMgr.addOffline}
            onSetActive={accountMgr.setActive}
            onRefreshAuth={accountMgr.refreshAuth}
            onCheckLicense={accountMgr.checkLicense}
            onSignOut={accountMgr.signOut}
            onDelete={accountMgr.remove}
            onConfigSaved={async (nextCfg) => {
              setConfig(nextCfg);
              await accountMgr.refresh();
            }}
            onNavigate={handleSelectSection}
            onToast={pushToast}
          />
        )}

        {activeSection === "settings" && (
          <SettingsPage
            appInfo={appInfo}
            config={config}
            theme={theme}
            accent={accent}
            bgPreset={bgPreset}
            onToggleTheme={handleToggleTheme}
            onAccentChange={setAccent}
            onBgPresetChange={setBgPreset}
            onConfigSaved={handleConfigSaved}
            onOpenDownloads={() => handleSelectSection("downloads")}
            onToast={pushToast}
          />
        )}
          </Suspense>
        </ErrorBoundary>

        {activeSection === "catalog" && (
          <>
            <Topbar
              query={debouncedQuery}
              onQueryCommit={handleQueryCommit}
              onUrlPaste={handleUrlPaste}
              contentType={contentType}
              onContentTypeChange={handleSwitchContentType}
              theme={theme}
              onToggleTheme={handleToggleTheme}
              cfStatus={cfDotState}
              installedCount={installedCount}
              onResetAll={resetAllFilters}
              onOpenMobileSidebar={handleOpenMobileSidebar}
              filtersOpen={filtersOpen}
              onToggleFilters={handleToggleFilters}
              activeFiltersCount={activeFiltersCount}
              onScanInstalledFiles={handleScanInstalledFiles}
              onScanInstalledNative={isDesktop ? handleScanInstalledNative : undefined}
              onOpenSettings={handleOpenSettings}
              onOpenAbout={handleOpenAbout}
            />

            <div
              className={`sidebar-backdrop overlay-animated${mobileSidebarOpen ? " is-open" : ""}`}
              onClick={() => setMobileSidebarOpen(false)}
            />

            <main className="app-layout">
              {modInstallTarget && (
                <div className="armed-profile-bar" role="status">
                  <div className="armed-profile-bar-info">
                    <div className="armed-profile-indicator">
                      <span className="armed-profile-pulse" aria-hidden="true" />
                      <span className="armed-profile-label">Целевой профиль:</span>
                    </div>
                    <strong className="armed-profile-name">{modInstallTarget.name}</strong>
                    <span className="armed-profile-badge mono-num">MC {modInstallTarget.mc_version}</span>
                    <span className="armed-profile-badge">{formatLoaderName(modInstallTarget.instance_type)}</span>
                    <span className="armed-profile-desc">
                      Клик «В профиль» сразу скачивает подходящую версию с зависимостями
                    </span>
                  </div>
                  <div className="armed-profile-bar-actions">
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs armed-clear-btn"
                      onClick={() => setModInstallTarget(null)}
                      title="Сбросить привязку к профилю"
                    >
                      <CloseIcon size={12} />
                      <span>Отвязать профиль</span>
                    </button>
                  </div>
                </div>
              )}
              <Sidebar
                mobileOpen={mobileSidebarOpen}
                onCloseMobile={() => setMobileSidebarOpen(false)}
                popoverOpen={filtersOpen}
                onClosePopover={() => setFiltersOpen(false)}
          contentType={contentType}
          mcVersions={visibleMcVersions}
          version={version}
          onVersionChange={(v) => {
            setVersion(v);
            scrollToTopSmooth();
          }}
          loadersIn={loadersIn}
          loadersOut={loadersOut}
          onLoaderAction={(id, act) => {
            handleTriState(id, act, loadersIn, loadersOut, setLoadersIn, setLoadersOut);
            scrollToTopSmooth();
          }}
          onClearLoaders={() => {
            setLoadersIn([]);
            setLoadersOut([]);
            scrollToTopSmooth();
          }}
          categories={availableCategories}
          categoriesIn={categoriesIn}
          categoriesOut={categoriesOut}
          catMode={catMode}
          onCatModeChange={setCatMode}
          onCategoryAction={(id, act) => {
            handleTriState(id, act, categoriesIn, categoriesOut, setCategoriesIn, setCategoriesOut);
            scrollToTopSmooth();
          }}
          onClearCategories={() => {
            setCategoriesIn([]);
            setCategoriesOut([]);
            scrollToTopSmooth();
          }}
          env={env}
          onEnvChange={(v) => {
            setEnv(v);
            scrollToTopSmooth();
          }}
          authorFilter={authorFilter}
          onAuthorFilterChange={(a) => {
            setAuthorFilter(a);
            scrollToTopSmooth();
          }}
          minDownloads={minDownloads}
          onMinDownloadsChange={(n) => {
            setMinDownloads(n);
            scrollToTopSmooth();
          }}
          updatedWithin={updatedWithin}
          onUpdatedWithinChange={(d) => {
            setUpdatedWithin(d);
            scrollToTopSmooth();
          }}
          hideAbandoned={hideAbandoned}
          onHideAbandonedChange={(v) => {
            setHideAbandoned(v);
            scrollToTopSmooth();
          }}
          srcModrinth={srcModrinth}
          srcCurseforge={srcCurseforge}
          cfConfigured={config.curseforge_configured}
          onToggleModrinth={setSrcModrinth}
          onToggleCurseforge={setSrcCurseforge}
          dependentsTarget={dependentsTarget}
          onSelectDependentsTarget={(t) => activateDependentsMode(t)}
          onClearDependentsTarget={() => setDependentsTarget(null)}
          onResetAll={resetAllFilters}
        />

        <section className="content-area">
          <DependentsBanner
            target={dependentsTarget}
            contentType={contentType}
            countsByType={depsCountsByType}
            relation={dependentsRelation}
            projectFilter={dependentsProjectFilter}
            loadedCount={results.length}
            totalCf={depsTotalCf}
            cfScanned={depsCfScanned}
            sourceMode={depsSourceMode}
            cfUrl={depsCfUrl}
            isLoading={isLoading}
            onContentTypeChange={handleSwitchContentType}
            onRelationChange={setDependentsRelation}
            onProjectFilterChange={setDependentsProjectFilter}
            onOpenBase={() => dependentsTarget && openCard(dependentsTarget)}
            onExit={() => setDependentsTarget(null)}
          />

          <div className={`reveal-slot${showCfNotice ? " is-shown" : ""}`}>
            <div className="reveal-inner">
              <div className="cf-notice">
                <span>CurseForge пока не подключён, поэтому сейчас показываются результаты только из Modrinth. Подключите CurseForge, чтобы искать сразу по обеим площадкам.</span>
                <button type="button" className="link-btn" onClick={() => setSettingsOpen(true)}>
                  Подключить CurseForge
                </button>
              </div>
            </div>
          </div>

          <div className={`reveal-slot${showStatsBar ? " is-shown" : ""}`}>
            <div className="reveal-inner">
              <section className="stats-bar" aria-live="polite">
                <div className="stats-left">
                  {fuzzySuggestion && <span className="pill pill-fuzzy">Умный поиск: {fuzzySuggestion}</span>}
                  {Object.entries(apiErrors).map(([k, v]) => (
                    <span key={k} className="source-error-inline" title={v}>
                      <span className="source-error-dot" aria-hidden="true" />
                      <span>{v}</span>
                    </span>
                  ))}
                </div>
                {Object.keys(apiErrors).length > 0 && (
                  <div className="stats-right">
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs"
                      onClick={() => {
                        api.clearClientCache();
                        setRetryTick((t) => t + 1);
                      }}
                    >
                      Повторить
                    </button>
                    {apiErrors.curseforge && (
                      <button type="button" className="btn btn-ghost btn-xs" onClick={() => setSettingsOpen(true)}>
                        Настройки API
                      </button>
                    )}
                  </div>
                )}
              </section>
            </div>
          </div>

          {/* Right-aligned Custom Sort & View Mode Toolbar above cards */}
          <div className="catalog-toolbar">
            <div className="catalog-toolbar-left mono-num">
              {results.length > 0 ? (
                <>
                  <span>Показано: {results.length}</span>
                  {slowLoading && (
                    <span className="toolbar-refresh-badge" role="status" aria-live="polite">
                      <span className="spinner spinner-xs" aria-hidden="true" />
                      <span>Обновление…</span>
                    </span>
                  )}
                </>
              ) : isLoading ? (
                "Загрузка…"
              ) : (
                ""
              )}
            </div>
            <div className="catalog-toolbar-right">
              <CustomSelect
                value={sort}
                options={SORT_OPTIONS}
                onChange={(v) => {
                  setSort(v as SortMode);
                  scrollToTopSmooth();
                }}
                className="cs-toolbar-sort"
                ariaLabel="Сортировка"
              />
              <div className={`view-switcher${viewMode === "list" ? " is-list" : ""}`} role="group" aria-label="Вид каталога">
                <span className="view-switcher-indicator" aria-hidden="true" />
                <button
                  className={`view-btn${viewMode === "grid" ? " active" : ""}`}
                  type="button"
                  title="Сетка карточек"
                  aria-label="Сетка"
                  onClick={() => {
                    startTransition(() => setViewMode("grid"));
                          }}
                >
                  <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
                </button>
                <button
                  className={`view-btn${viewMode === "list" ? " active" : ""}`}
                  type="button"
                  title="Табличный список"
                  aria-label="Список"
                  onClick={() => {
                    startTransition(() => setViewMode("list"));
                          }}
                >
                  <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="8" y1="6" x2="21" y2="6"/><line x1="8" y1="12" x2="21" y2="12"/><line x1="8" y1="18" x2="21" y2="18"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/></svg>
                </button>
              </div>
            </div>
          </div>

          <ErrorBoundary>
            {viewMode === "list" && results.length > 0 && (
              <div className={`list-table-head${showLoadersCol ? "" : " no-loaders-col"}`} aria-hidden="true">
                <span>Проект</span>
                {showLoadersCol && (
                  <span>{contentType === "datapack" ? "Формат / Загрузчики" : "Загрузчики / Среда"}</span>
                )}
                <span>Статистика</span>
                <span style={{ textAlign: "right" }}>Действие</span>
              </div>
            )}

            {isLoading && results.length === 0 ? (
              <section className={`results-grid${viewMode === "list" ? " view-list" : ""}`}>
                {Array.from({ length: viewMode === "list" ? 8 : 12 }).map((_, i) => (
                  <SkeletonCard key={i} viewMode={viewMode} />
                ))}
              </section>
            ) : !results.length ? (
              <section className="results-grid">
                <div className="empty-state">
                  {dependentsTarget && (hasMore || (depsSkippedModpacks > 0 && dependentsProjectFilter !== "all")) ? (
                    <>
                      <h3>
                        {depsSkippedModpacks > 0
                          ? `На просмотренных страницах CurseForge попались в основном модпаки (${depsSkippedModpacks} шт.)`
                          : "На загруженных страницах пока нет проектов выбранного типа"}
                      </h3>
                      <p>Можно просканировать следующие страницы или включить отображение модпаков.</p>
                      <div style={{ marginTop: 12, display: "flex", gap: 8, justifyContent: "center", flexWrap: "wrap" }}>
                        {hasMore && (
                          <button type="button" className="btn btn-primary btn-sm" onClick={() => executeFetch(true)}>
                            Сканировать дальше
                          </button>
                        )}
                        <button type="button" className="btn btn-ghost btn-sm" onClick={() => setDependentsProjectFilter("all")}>
                          Показать с модпаками
                        </button>
                      </div>
                    </>
                  ) : Object.keys(apiErrors).length > 0 ? (
                    <>
                      <h3>Не удалось подключиться к API</h3>
                      <p className="mono-num" style={{ marginTop: 6, fontSize: 12, opacity: 0.85 }}>
                        {Object.entries(apiErrors)
                          .map(([k, v]) => `${k}: ${v}`)
                          .join(" | ")}
                      </p>
                      <div style={{ marginTop: 14, display: "flex", gap: 8, justifyContent: "center", flexWrap: "wrap" }}>
                        <button type="button" className="btn btn-primary btn-sm" onClick={() => executeFetch(false)}>
                          Повторить
                        </button>
                        <button type="button" className="btn btn-ghost btn-sm" onClick={() => setSettingsOpen(true)}>
                          Настройки сети и API
                        </button>
                      </div>
                    </>
                  ) : (
                    <>
                      <svg className="empty-state-svg" viewBox="0 0 48 48" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                        <circle cx="21" cy="21" r="12" />
                        <path d="M40 40l-9.5-9.5" />
                        <path d="M17 17l8 8" />
                        <path d="M25 17l-8 8" />
                      </svg>
                      <h3>Ничего не найдено</h3>
                      <p>Попробуйте изменить запрос или сбросить фильтры.</p>
                      <button type="button" className="btn btn-primary btn-sm" style={{ marginTop: 12 }} onClick={resetAllFilters}>
                        Сбросить фильтры
                      </button>
                    </>
                  )}
                </div>
              </section>
            ) : (
              <VirtualizedResults
                items={results}
                query={debouncedQuery}
                sortMode={sort}
                viewMode={viewMode}
                showLoadersCol={showLoadersCol}
                activeLoadersIn={loadersIn}
                targetMcVersion={targetCompatMc}
                targetLoader={targetCompatLoader}
                availableCategories={availableCategories}
                quickBusySet={quickBusySet}
                installedMap={installedMap}
                isProfileInstallMode={Boolean(modInstallTarget)}
                resolveInstalledInfo={resolveInstalledForCard}
                isRefreshing={slowLoading && results.length > 0}
                ctxActiveGroupId={ctxMenu?.item.group_id || ""}
                onOpen={openCard}
                onPrimaryAction={handlePrimaryAction}
                onAuthorClick={handleAuthorFilter}
                onCategoryClick={handleCategoryTagAdd}
                onContextMenu={handleCardContextMenu}
              />
            )}
          </ErrorBoundary>

          <section className="infinite-footer">
            <div ref={sentinelRef} className="scroll-sentinel" />
            <div className={`infinite-loader${isLoadingMore ? "" : " hidden"}`}>
              <span className="spinner" />
              <span>Подгрузка карточек...</span>
            </div>
            <div className="infinite-status mono-num">
              {!isLoading && !isLoadingMore && results.length > 0
                ? hasMore
                  ? "Автопрогрузка активна"
                  : `Все доступные результаты загружены (${results.length})`
                : ""}
            </div>
            {!isLoading && !isLoadingMore && hasMore && results.length > 0 && (
              <button type="button" className="btn btn-ghost btn-sm" onClick={() => executeFetch(true)}>
                Загрузить ещё
              </button>
            )}
          </section>
        </section>
      </main>
          </>
        )}
      </div>

      <Suspense fallback={null}>
        {modModalEver && (
          <ModModal
            item={activeMod}
            preloadedDetails={activeModDetails}
            initialTab={activeModTab}
            historyPrev={modalHistory.length ? modalHistory[modalHistory.length - 1] : null}
            availableCategories={availableCategories}
            activeVersion={version}
            activeLoadersIn={loadersIn}
            activeLoadersOut={loadersOut}
            enabledSources={enabledSources}
            onClose={() => {
              setActiveMod(null);
              setActiveModDetails(null);
              setModalHistory([]);
            }}
            onBack={() => {
              if (!modalHistory.length) return;
              const prev = modalHistory[modalHistory.length - 1];
              void (async () => {
                const resolved = await resolveBeforeOpen(prev);
                if (!resolved) return;
                setModalHistory((h) => h.slice(0, -1));
                setActiveModTab("description");
                setActiveModDetails(resolved.details);
                setActiveMod(resolved.item);
              })();
            }}
            onOpenOther={(next, push) => {
              const cur = activeMod;
              void (async () => {
                const resolved = await resolveBeforeOpen(next);
                if (!resolved) return;
                if (push && cur) setModalHistory((h) => [...h.slice(-6), cur]);
                recordRecent(resolved.item);
                setActiveModTab("description");
                setActiveModDetails(resolved.details);
                setActiveMod(resolved.item);
              })();
            }}
            onItemMerged={(merged) => {
              if (activeMod) applyMergedItemUpdate(activeMod.group_id, merged);
            }}
            onQuickDownload={(it) => handlePrimaryAction(it)}
            onAuthorClick={handleAuthorFilter}
            onCategoryClick={handleCategoryTagAdd}
            onOpenDependentsCatalog={(it, opts) => activateDependentsMode(it, opts)}
            onOpenLightbox={(items, index) => setLightbox({ items, index })}
            modInstallTarget={modInstallTarget}
            isTargetProfileInstalled={Boolean(
              activeMod &&
                modInstallTarget &&
                resolveInstalledForCard(activeMod) &&
                !resolveInstalledForCard(activeMod)?.has_update
            )}
            onClearModInstall={() => setModInstallTarget(null)}
            onToast={pushToast}
          />
        )}

        {qdModalEver && (
          <QuickDownloadModal
            item={quickDownloadItem}
            preloadedDetails={quickDownloadDetails}
            initialMcVersion={version}
            initialLoader={loadersIn[0] || ""}
            onClose={() => {
              setQuickDownloadItem(null);
              setQuickDownloadDetails(null);
            }}
            onItemMerged={(merged) => {
              if (quickDownloadItem) applyMergedItemUpdate(quickDownloadItem.group_id, merged);
            }}
            onOpenFullFiles={(it) => openCard(it, "files")}
            onToast={pushToast}
            modInstallTarget={modInstallTarget}
            onClearModInstall={() => setModInstallTarget(null)}
          />
        )}

        {settingsEver && (
          <SettingsModal
            open={settingsOpen}
            config={config}
            accent={accent}
            bgPreset={bgPreset}
            onAccentChange={setAccent}
            onBgPresetChange={setBgPreset}
            onClose={() => setSettingsOpen(false)}
            onConfigSaved={handleConfigSaved}
            onToast={pushToast}
          />
        )}


        {lightboxEver && (
          <Lightbox
            items={lightbox.items}
            index={lightbox.index}
            onClose={() => setLightbox({ items: [], index: -1 })}
            onChangeIndex={(index) => setLightbox((prev) => ({ ...prev, index }))}
          />
        )}

        {aboutEver && (
          <AboutModal
            open={aboutOpen}
            onClose={() => setAboutOpen(false)}
            onOpenSettings={() => setSettingsOpen(true)}
          />
        )}

        <InstallToProfileModal
          open={Boolean(installToProfileItem)}
          item={installToProfileItem}
          instances={instanceMgr.instances}
          onClose={() => setInstallToProfileItem(null)}
          onArmProfile={(inst) => {
            setModInstallTarget(inst);
            if (inst.mc_version) setVersion(inst.mc_version);
            if (inst.instance_type && inst.instance_type !== "vanilla") {
              setLoadersIn([inst.instance_type]);
            }
          }}
          onNavigateToInstances={() => {
            setInstallToProfileItem(null);
            handleSelectSection("instances");
          }}
          onToast={pushToast}
          onInstalledSuccess={(inst) => {
            if (modInstallTarget?.id === inst.id) {
              setArmedContentTick((t) => t + 1);
            }
          }}
        />
      </Suspense>

      <ContextMenu
        menu={ctxMenu}
        onClose={() => setCtxMenu(null)}
        onOpen={openCard}
        onOpenDependents={(it) => activateDependentsMode(it)}
        onAuthorSearch={handleAuthorFilter}
        onPrimaryAction={(it) => handlePrimaryAction(it)}
      />

      <div
        className={`preload-gate-backdrop overlay-animated${preloadingTitle ? " is-open" : ""}`}
        aria-hidden={!preloadingTitle}
        onClick={cancelPreload}
      >
        {preloadingTitle && (
          <div className="preload-gate-pill" onClick={(e) => e.stopPropagation()}>
            <span className="spinner spinner-sm" />
            <span>Поиск версий и склейка: <strong>{preloadingTitle}</strong></span>
          </div>
        )}
      </div>

      <ScrollTopButton />

      <Suspense fallback={null}>
      </Suspense>
      <ToastHost />
      <TooltipHost />
      </div>
    </div>
  );
};
