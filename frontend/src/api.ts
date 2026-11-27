import {
  clearPersistentCache,
  loadPersistentEntries,
  loadSyncSessionEntries,
  persistEntry,
} from "./persistCache";
import { getWailsBridge } from "./services/bridge";
import type { DesktopIndexStatus } from "./services/wails";
import type {
  AccountsSnapshot,
  AccountView,
  CartItem,
  ContentType,
  CrashReportInfo,
  CrashDiagnosis,
  CreateInstanceInput,
  DeleteInstanceInput,
  DependencyRef,
  DepResolveResponse,
  DepTreeResponse,
  DependentsResponse,
  DownloadRequestInput,
  DownloadsSnapshot,
  DownloadTaskView,
  DuplicateInstanceInput,
  FilterCategory,
  FullVersionVerificationReport,
  GameInstance,
  GameLogLine,
  GameSessionInfo,
  InstalledModInfo,
  InstalledModRef,
  ResourcePackRef,
  ShaderPackRef,
  WorldRef,
  ExportWorldResult,
  ModUpdateInfo,
  ApplyModUpdateInput,
  InstalledProfileVersion,
  InstallInstanceModInput,
  InstanceModInstallResult,
  InstallProfileVersionInput,
  InstallProgressInfo,
  InstanceFilesSummary,
  InstanceIntegrityReport,
  InstancesSnapshot,
  JavaRecommendationResponse,
  JavaRuntimeInfo,
  JavaStateSnapshot,
  LauncherSnapshot,
  LaunchInstanceInput,
  LoaderVersionsResult,
  MCVersionManifestResult,
  ModDetailsResponse,
  PreflightReport,
  ModVersionsResponse,
  NetworkCheckResponse,
  PackDownloadReport,
  ProjectCard,
  PublicConfig,
  QuickFileResponse,
  SavedPack,
  SearchResponse,
  ApplyUpdateInput,
  ExecuteImportInput,
  ExportModpackInput,
  ExportModpackResult,
  ImportPreview,
  ModpackImportProgress,
  ModpackView,
  PreviewImportInput,
  UpdateDiffPreview,
  UpdateInstanceInput,
} from "./types";

interface CacheRecord<T> {
  data: T;
  freshUntil: number;
  staleUntil: number;
}

class ClientSWRCache {
  private map = new Map<string, CacheRecord<unknown>>();
  private inflight = new Map<string, Promise<unknown>>();
  private maxSize: number;
  private epoch = 0;

  constructor(maxSize = 180) {
    this.maxSize = maxSize;
    const bootEpoch = this.epoch;
    // 1. Synchronous hydration of hot metadata from sessionStorage
    for (const rec of loadSyncSessionEntries()) {
      this.map.set(rec.key, {
        data: rec.data,
        freshUntil: rec.freshUntil,
        staleUntil: rec.staleUntil,
      });
    }
    // 2. Async hydration from IndexedDB — guarded by epoch & never overwrites fresher live entries
    void loadPersistentEntries().then((entries) => {
      if (this.epoch !== bootEpoch) return;
      const now = Date.now();
      for (const rec of entries) {
        if (rec.staleUntil <= now) continue;
        if (!this.map.has(rec.key)) {
          this.map.set(rec.key, {
            data: rec.data,
            freshUntil: rec.freshUntil,
            staleUntil: rec.staleUntil,
          });
        }
      }
    });
  }

  getEpoch(): number {
    return this.epoch;
  }

  getFresh<T>(key: string): T | null {
    const rec = this.map.get(key);
    if (!rec) return null;
    if (Date.now() <= rec.freshUntil) {
      this.map.delete(key);
      this.map.set(key, rec);
      return rec.data as T;
    }
    return null;
  }

  getStale<T>(key: string): T | null {
    const rec = this.map.get(key);
    if (!rec) return null;
    if (Date.now() <= rec.staleUntil) {
      return rec.data as T;
    }
    this.map.delete(key);
    return null;
  }

  set<T>(key: string, data: T, freshMs: number, staleMs = freshMs * 4, expectedEpoch?: number) {
    if (expectedEpoch !== undefined && expectedEpoch !== this.epoch) {
      return;
    }
    const now = Date.now();
    const rec = { data, freshUntil: now + freshMs, staleUntil: now + staleMs };
    this.map.delete(key);
    this.map.set(key, rec);
    while (this.map.size > this.maxSize) {
      const oldest = this.map.keys().next().value;
      if (oldest === undefined) break;
      this.map.delete(oldest);
    }
    persistEntry({ key, data, freshUntil: rec.freshUntil, staleUntil: rec.staleUntil, savedAt: now });
  }

  dedupe<T>(
    key: string,
    fetcher: () => Promise<T>,
    freshMs: number,
    shouldCache?: (res: T) => boolean,
    onRevalidate?: (fresh: T) => void
  ): Promise<T> {
    const fresh = this.getFresh<T>(key);
    if (fresh !== null) return Promise.resolve(fresh);

    const existing = this.inflight.get(key);
    if (existing) return existing as Promise<T>;

    const startEpoch = this.epoch;
    const stale = this.getStale<T>(key);

    let p: Promise<T>;
    p = fetcher()
      .then((res) => {
        if (this.epoch === startEpoch && (!shouldCache || shouldCache(res))) {
          this.set(key, res, freshMs, freshMs * 4, startEpoch);
          if (stale !== null && onRevalidate) {
            onRevalidate(res);
          }
        }
        return res;
      })
      .finally(() => {
        // Only delete inflight slot if it still belongs to THIS promise (prevents older request deleting newer one)
        if (this.inflight.get(key) === p) {
          this.inflight.delete(key);
        }
      });
    this.inflight.set(key, p);

    if (stale !== null) {
      p.catch(() => {});
      return Promise.resolve(stale);
    }
    return p;
  }

  clear() {
    this.epoch++;
    this.map.clear();
    this.inflight.clear();
    clearPersistentCache();
  }
}

const swrCache = new ClientSWRCache(200);

async function reqJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const res = await fetch(url, {
    ...init,
    headers: {
      Accept: "application/json",
      ...(init?.body ? { "Content-Type": "application/json" } : {}),
      ...(init?.headers || {}),
    },
  });
  const data = await res.json().catch(() => ({ ok: false, error: `HTTP ${res.status}` }));
  if (!res.ok || data?.ok === false) {
    throw new Error(data?.error || `HTTP ${res.status}`);
  }
  return data as T;
}

function detailsCacheKey(item: ProjectCard): string {
  const sks = (item.sources || []).map((s) => s.source_key).sort().join("+");
  return `details::${item.group_id}::${sks}`;
}

export const api = {
  clearClientCache() {
    swrCache.clear();
  },

  async clearAllCaches(): Promise<void> {
    swrCache.clear();
    const b = getWailsBridge();
    if (b) {
      await b.ClearAllCaches();
      return;
    }
    await reqJSON<{ ok: true }>("/api/cache/clear", { method: "POST", body: JSON.stringify({}) });
  },

  getConfig(): Promise<PublicConfig> {
    const b = getWailsBridge();
    if (b) return b.GetConfig();
    return reqJSON<PublicConfig>("/api/config");
  },

  async saveConfig(payload: { curseforge_api_key?: string; merge_threshold?: number; proxy_url?: string; auto_install_medium_deps?: boolean; max_concurrent_downloads?: number; microsoft_client_id?: string }): Promise<PublicConfig> {
    const b = getWailsBridge();
    const res = b
      ? await b.UpdateConfig(payload)
      : await reqJSON<PublicConfig>("/api/config", {
          method: "POST",
          body: JSON.stringify(payload),
        });
    swrCache.clear();
    return res;
  },

  async checkNetwork(): Promise<NetworkCheckResponse> {
    const b = getWailsBridge();
    if (b) return b.CheckNetwork();
    const res = await fetch("/api/network/check", {
      headers: { Accept: "application/json" },
    });
    const data = await res.json().catch(() => null);
    if (!data) {
      throw new Error(`HTTP ${res.status}`);
    }
    return data as NetworkCheckResponse;
  },

  async testCurseForgeKey(curseforge_api_key: string): Promise<{ ok: boolean; message?: string; error?: string }> {
    const b = getWailsBridge();
    if (b) return b.TestCurseForgeKey(curseforge_api_key);
    const res = await fetch("/api/config/test-cf", {
      method: "POST",
      headers: { "Content-Type": "application/json", Accept: "application/json" },
      body: JSON.stringify({ curseforge_api_key }),
    });
    return res.json().catch(() => ({ ok: false, error: `HTTP ${res.status}` }));
  },

  getIndexStatus(): Promise<DesktopIndexStatus> {
    const b = getWailsBridge();
    if (b) return b.GetIndexStatus();
    return reqJSON<DesktopIndexStatus>("/api/index/status");
  },

  triggerIndexSync(full = false): Promise<{ ok: boolean; started: boolean; status: DesktopIndexStatus }> {
    const b = getWailsBridge();
    if (b) return b.TriggerIndexSync(full);
    return reqJSON("/api/index/sync", { method: "POST", body: JSON.stringify({ full }) });
  },

  getMcVersions(): Promise<{ ok: true; versions: string[]; by_loader: Record<string, string[]> }> {
    return swrCache.dedupe(
      "mc_versions",
      () => {
        const b = getWailsBridge();
        return b ? b.GetMCVersions() : reqJSON("/api/mc-versions");
      },
      30 * 60_000
    );
  },

  getFilters(type: ContentType): Promise<FilterCategory[]> {
    return swrCache.dedupe(
      `filters::${type}`,
      async () => {
        const b = getWailsBridge();
        const r = b
          ? await b.GetFilters(type)
          : await reqJSON<{ ok: true; categories: FilterCategory[] }>(
              `/api/filters?type=${encodeURIComponent(type)}`
            );
        return r.categories || [];
      },
      10 * 60_000
    );
  },

  search(
    params: Record<string, string | number | boolean | undefined>,
    signal?: AbortSignal
  ): Promise<SearchResponse> {
    const qs = new URLSearchParams();
    for (const [k, v] of Object.entries(params)) {
      if (v !== undefined && v !== "" && v !== false) {
        qs.set(k, typeof v === "boolean" ? "1" : String(v));
      }
    }
    const queryStr = qs.toString();
    const key = `search::${queryStr}`;
    const fresh = swrCache.getFresh<SearchResponse>(key);
    if (fresh !== null) return Promise.resolve(fresh);

    const startEpoch = swrCache.getEpoch();
    return reqJSON<SearchResponse>(`/api/search?${queryStr}`, { signal }).then((res) => {
      if (!res.errors || Object.keys(res.errors).length === 0) {
        swrCache.set(key, res, 60_000, 300_000, startEpoch);
      }
      return res;
    });
  },

  async resolveUrl(url: string): Promise<ProjectCard> {
    const b = getWailsBridge();
    if (b) return b.ResolveURL(url);
    const r = await reqJSON<{ ok: true; item: ProjectCard }>("/api/resolve-url", {
      method: "POST",
      body: JSON.stringify({ url }),
    });
    return r.item;
  },

  getCachedModDetails(item: ProjectCard): ModDetailsResponse | null {
    if (!item || !item.group_id) return null;
    const key = detailsCacheKey(item);
    const cached = swrCache.getFresh<ModDetailsResponse>(key);
    if (cached && (!cached.errors || Object.keys(cached.errors).length === 0)) {
      return cached;
    }
    return null;
  },

  getModDetails(item: ProjectCard, signal?: AbortSignal): Promise<ModDetailsResponse> {
    const key = detailsCacheKey(item);
    const fresh = swrCache.getFresh<ModDetailsResponse>(key);
    if (fresh !== null && (!fresh.errors || Object.keys(fresh.errors).length === 0)) {
      return Promise.resolve(fresh);
    }

    const startEpoch = swrCache.getEpoch();
    const doFetch = async () => {
      const res = await reqJSON<ModDetailsResponse>("/api/mod/details", {
        method: "POST",
        body: JSON.stringify({
          item,
          sources: item.sources || [],
          content_type: item.content_type || "mod",
        }),
        signal,
      });
      const clean = !res.errors || Object.keys(res.errors).length === 0;
      if (clean) {
        swrCache.set(key, res, 180_000, 360_000, startEpoch);
        if (res.merged_item) {
          const mKey = detailsCacheKey(res.merged_item);
          if (mKey !== key) {
            swrCache.set(mKey, res, 180_000, 360_000, startEpoch);
          }
        }
      }
      return res;
    };

    if (signal) {
      return doFetch();
    }
    return swrCache.dedupe(key, doFetch, 180_000, (res) => !res.errors || Object.keys(res.errors).length === 0);
  },

  prefetchModDetails(item: ProjectCard): void {
    if (!item || !item.group_id || item.content_type === "modpack") return;
    const key = detailsCacheKey(item);
    if (swrCache.getFresh<ModDetailsResponse>(key) !== null) return;
    this.getModDetails(item).catch(() => {});
  },

  getModVersions(
    payload: {
      item?: ProjectCard;
      sources: ProjectCard["sources"];
      content_type: string;
      loader?: string;
      mc_version?: string;
      channel?: string;
      platform?: string;
      sort?: string;
      preload_all?: boolean;
      page?: number;
      limit?: number;
    },
    signal?: AbortSignal
  ): Promise<ModVersionsResponse> {
    const sks = (payload.sources || []).map((s) => s.source_key).sort().join("+");
    const gid = payload.item?.group_id || "";
    const key = [
      "versions_v2",
      gid,
      sks,
      payload.content_type || "mod",
      payload.loader || "",
      payload.mc_version || "",
      payload.channel || "",
      payload.platform || "",
      payload.sort || "",
      payload.preload_all ? "1" : "0",
      String(payload.page || 1),
      String(payload.limit || 0),
    ].join("::");
    const fresh = swrCache.getFresh<ModVersionsResponse>(key);
    if (fresh !== null) return Promise.resolve(fresh);

    const startEpoch = swrCache.getEpoch();
    const doFetch = async () => {
      const res = await reqJSON<ModVersionsResponse>("/api/mod/versions", {
        method: "POST",
        body: JSON.stringify(payload),
        signal,
      });
      if (res.ok) {
        swrCache.set(key, res, 180_000, 360_000, startEpoch);
      }
      return res;
    };
    if (signal) return doFetch();
    return swrCache.dedupe(key, doFetch, 180_000, (res) => Boolean(res.ok));
  },

  getQuickFile(payload: {
    item: ProjectCard;
    mc_version: string;
    loader: string;
    include_deps: boolean;
    existing_source_keys: string[];
    existing_filenames: string[];
  }): Promise<QuickFileResponse> {
    return reqJSON<QuickFileResponse>("/api/mod/quick-file", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  getDependents(
    payload: {
      item: ProjectCard;
      relation: string;
      project_filter: string;
      content_type?: string;
      mc_version?: string;
      loaders_in?: string[];
      loaders_out?: string[];
      categories?: string[];
      categories_out?: string[];
      cat_mode?: string;
      env?: string;
      updated_within?: number;
      hide_abandoned?: boolean;
      q?: string;
      sort?: string;
      sources?: string[];
      cf_index?: number;
      page_size?: number;
    },
    signal?: AbortSignal
  ): Promise<DependentsResponse> {
    const sks = (payload.item?.sources || []).map((s) => s.source_key).sort().join("+");
    const key = [
      "rdeps_v2",
      payload.item?.group_id || "",
      sks,
      payload.relation || "required",
      payload.project_filter || "no_modpacks",
      payload.content_type || "",
      payload.mc_version || "",
      [...(payload.loaders_in || [])].sort().join(","),
      [...(payload.loaders_out || [])].sort().join(","),
      [...(payload.categories || [])].sort().join(","),
      [...(payload.categories_out || [])].sort().join(","),
      payload.cat_mode || "and",
      payload.env || "",
      String(payload.updated_within || 0),
      payload.hide_abandoned ? "1" : "0",
      (payload.q || "").trim(),
      payload.sort || "default",
      [...(payload.sources || [])].sort().join(","),
      String(payload.cf_index || 0),
      String(payload.page_size || 50),
    ].join("|");

    const fresh = swrCache.getFresh<DependentsResponse>(key);
    if (fresh !== null) return Promise.resolve(fresh);

    const startEpoch = swrCache.getEpoch();
    return reqJSON<DependentsResponse>("/api/mod/dependents", {
      method: "POST",
      body: JSON.stringify(payload),
      signal,
    }).then((res) => {
      const hasErr = Boolean(res.errors && Object.keys(res.errors).length > 0);
      if (res.ok && !hasErr && !(res.cf_web_blocked && (res.items || []).length === 0)) {
        swrCache.set(key, res, 90_000, 180_000, startEpoch);
      }
      return res;
    });
  },

  getDependencyTree(
    dependencies: DependencyRef[],
    optsOrSignal?:
      | AbortSignal
      | {
          mc_version?: string;
          loader?: string;
          file_url?: string;
          platform?: string;
          project_id?: string;
          title?: string;
          summary?: string;
          body?: string;
          changelog?: string;
          max_depth?: number;
          signal?: AbortSignal;
        }
  ): Promise<DepTreeResponse> {
    const isSig = optsOrSignal && typeof (optsOrSignal as AbortSignal).aborted === "boolean";
    const opts = isSig ? {} : (optsOrSignal as Exclude<typeof optsOrSignal, AbortSignal | undefined>) || {};
    const signal = isSig ? (optsOrSignal as AbortSignal) : opts.signal;

    const depSig = (dependencies || [])
      .map((d) => `${d.platform}:${d.project_id}:${d.version_id}:${d.type}`)
      .sort()
      .join(";");
    const textSig = `${opts.title || ""}|${(opts.summary || "").length}|${(opts.body || "").length}|${(opts.changelog || "").length}`;
    const key = [
      "dtree_v4",
      opts.platform || "",
      opts.project_id || "",
      opts.mc_version || "",
      opts.loader || "",
      String(opts.max_depth || 3),
      opts.file_url || "",
      textSig,
      depSig,
    ].join("|");

    const fresh = swrCache.getFresh<DepTreeResponse>(key);
    if (fresh !== null) return Promise.resolve(fresh);

    const startEpoch = swrCache.getEpoch();
    return reqJSON<DepTreeResponse>("/api/mod/dependency-tree", {
      method: "POST",
      body: JSON.stringify({
        dependencies: dependencies || [],
        mc_version: opts.mc_version || "",
        loader: opts.loader || "",
        file_url: opts.file_url || "",
        platform: opts.platform || "",
        project_id: opts.project_id || "",
        title: opts.title || "",
        summary: opts.summary || "",
        body: opts.body || "",
        changelog: opts.changelog || "",
        max_depth: opts.max_depth || 3,
      }),
      signal,
    }).then((res) => {
      const clean = res.status === "complete" || res.status === "none";
      if (res.ok && clean && (!res.errors || Object.keys(res.errors).length === 0)) {
        swrCache.set(key, res, 120_000, 240_000, startEpoch);
      }
      return res;
    });
  },

  getFileChangelog(payload: {
    platform: string;
    mod_id: string;
    file_id: string;
  }): Promise<{ ok: true; changelog: string; format: "html" | "markdown" }> {
    return reqJSON("/api/mod/file-changelog", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  resolveDependencies(payload: {
    dependencies: DependencyRef[];
    mc_version: string;
    loader: string;
    file_url?: string;
    platform?: string;
    project_id?: string;
    title?: string;
    summary?: string;
    body?: string;
    changelog?: string;
    allow_medium_confidence?: boolean;
    existing_source_keys: string[];
    existing_filenames: string[];
  }): Promise<DepResolveResponse> {
    return reqJSON<DepResolveResponse>("/api/resolve-dependencies", {
      method: "POST",
      body: JSON.stringify(payload),
    });
  },

  checkInstalledHashes(
    hashes: string[],
    mc_version = "",
    loader = ""
  ): Promise<{ ok: true; installed: Record<string, InstalledModInfo>; matched_count: number }> {
    return reqJSON("/api/mods/check-installed", {
      method: "POST",
      body: JSON.stringify({ hashes, mc_version, loader }),
    });
  },

  async getPacks(): Promise<SavedPack[]> {
    const b = getWailsBridge();
    if (b) return (await b.GetPacks()) || [];
    const r = await reqJSON<{ ok: true; packs: SavedPack[] }>("/api/packs");
    return r.packs || [];
  },

  async savePack(pack: SavedPack): Promise<SavedPack[]> {
    const b = getWailsBridge();
    if (b) return (await b.SavePack(pack)) || [];
    const r = await reqJSON<{ ok: true; packs: SavedPack[] }>("/api/packs/save", {
      method: "POST",
      body: JSON.stringify(pack),
    });
    return r.packs || [];
  },

  async deletePack(id: string): Promise<SavedPack[]> {
    const b = getWailsBridge();
    if (b) return (await b.DeletePack(id)) || [];
    const r = await reqJSON<{ ok: true; packs: SavedPack[] }>("/api/packs/delete", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.packs || [];
  },

  async downloadPackZipWithReport(
    packName: string,
    items: CartItem[],
    onProgress?: (pct: number) => void
  ): Promise<PackDownloadReport> {
    const res = await fetch("/api/download-pack", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ pack_name: packName, items }),
    });
    if (!res.ok) {
      const errObj = await res.json().catch(() => ({ error: `HTTP ${res.status}` }));
      throw new Error(errObj?.error || `HTTP ${res.status}`);
    }
    const downloadedHdr = Number(res.headers.get("X-ModHub-Pack-Downloaded") || items.length);
    const totalHdr = Number(res.headers.get("X-ModHub-Pack-Total") || items.length);
    const failedHdr = Number(res.headers.get("X-ModHub-Pack-Failed") || 0);
    const statusHdr = res.headers.get("X-ModHub-Pack-Status") === "partial" || failedHdr > 0 ? "partial" : "complete";
    const rawErrs = res.headers.get("X-ModHub-Pack-Errors") || "";
    let failedReasons: string[] = [];
    if (rawErrs) {
      try {
        failedReasons = decodeURIComponent(rawErrs).split(" | ").filter(Boolean);
      } catch {
        failedReasons = rawErrs.split(" | ").filter(Boolean);
      }
    }

    let blob: Blob;
    if (!res.body || typeof res.body.getReader !== "function" || !onProgress) {
      blob = await res.blob();
    } else {
      const estTotal =
        Number(res.headers.get("Content-Length") || 0) ||
        items.reduce((acc, it) => acc + Number(it.file_size || 0), 0);
      const reader = res.body.getReader();
      const chunks: Uint8Array[] = [];
      let received = 0;
      while (true) {
        const { done, value } = await reader.read();
        if (done) break;
        if (value && value.byteLength) {
          chunks.push(value);
          received += value.byteLength;
          const pct = estTotal > 0 ? Math.min(98, Math.round((received / estTotal) * 100)) : 50;
          onProgress(pct);
        }
      }
      onProgress(100);
      blob = new Blob(chunks as BlobPart[], { type: "application/zip" });
    }
    return {
      blob,
      downloaded: downloadedHdr,
      total: totalHdr,
      failed: failedHdr,
      status: statusHdr,
      failedReasons,
    };
  },

  async downloadPackZip(
    packName: string,
    items: CartItem[],
    onProgress?: (pct: number) => void
  ): Promise<Blob> {
    const rep = await this.downloadPackZipWithReport(packName, items, onProgress);
    return rep.blob;
  },

  buildDirectDownloadUrl(downloadUrl: string, filename: string, platform: string): string {
    if (platform === "curseforge") {
      const qs = new URLSearchParams({ url: downloadUrl, filename: filename || "mod.jar" });
      return `/api/download-file?${qs.toString()}`;
    }
    return downloadUrl;
  },

  // --- Centralized Download Manager API ---

  async getDownloads(): Promise<DownloadsSnapshot> {
    const b = getWailsBridge();
    if (b) return b.GetDownloads();
    const r = await reqJSON<{ ok: true; tasks: DownloadTaskView[]; summary: DownloadsSnapshot["summary"] }>("/api/downloads");
    return { tasks: r.tasks || [], summary: r.summary };
  },

  async addDownload(req: DownloadRequestInput): Promise<DownloadTaskView> {
    const b = getWailsBridge();
    if (b) return b.AddDownload(req);
    const r = await reqJSON<{ ok: true; task: DownloadTaskView }>("/api/downloads/add", {
      method: "POST",
      body: JSON.stringify(req),
    });
    return r.task;
  },

  async pauseDownload(id: string): Promise<DownloadTaskView> {
    const b = getWailsBridge();
    if (b) return b.PauseDownload(id);
    const r = await reqJSON<{ ok: true; task: DownloadTaskView }>("/api/downloads/pause", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.task;
  },

  async resumeDownload(id: string): Promise<DownloadTaskView> {
    const b = getWailsBridge();
    if (b) return b.ResumeDownload(id);
    const r = await reqJSON<{ ok: true; task: DownloadTaskView }>("/api/downloads/resume", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.task;
  },

  async cancelDownload(id: string): Promise<DownloadTaskView> {
    const b = getWailsBridge();
    if (b) return b.CancelDownload(id);
    const r = await reqJSON<{ ok: true; task: DownloadTaskView }>("/api/downloads/cancel", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.task;
  },

  async retryDownload(id: string): Promise<DownloadTaskView> {
    const b = getWailsBridge();
    if (b) return b.RetryDownload(id);
    const r = await reqJSON<{ ok: true; task: DownloadTaskView }>("/api/downloads/retry", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.task;
  },

  async removeDownload(id: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.RemoveDownload(id);
      return;
    }
    await reqJSON<{ ok: true }>("/api/downloads/remove", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
  },

  async clearFinishedDownloads(): Promise<number> {
    const b = getWailsBridge();
    if (b) return b.ClearFinishedDownloads();
    const r = await reqJSON<{ ok: true; removed: number }>("/api/downloads/clear-finished", {
      method: "POST",
      body: JSON.stringify({}),
    });
    return r.removed || 0;
  },

  async setMaxConcurrentDownloads(n: number): Promise<PublicConfig> {
    const b = getWailsBridge();
    if (b) return b.SetMaxConcurrentDownloads(n);
    return this.saveConfig({ max_concurrent_downloads: n });
  },

  // --- Java Runtime Manager API ---

  async getJavaRuntimes(refreshRemote = false): Promise<JavaStateSnapshot> {
    const b = getWailsBridge();
    if (b) return b.GetJavaRuntimes(refreshRemote);
    return reqJSON<JavaStateSnapshot>(`/api/java?refresh=${refreshRemote ? "1" : "0"}`);
  },

  async scanJavaRuntimes(): Promise<JavaStateSnapshot> {
    const b = getWailsBridge();
    if (b) return b.ScanJavaRuntimes();
    return reqJSON<JavaStateSnapshot>("/api/java/scan", {
      method: "POST",
      body: JSON.stringify({}),
    });
  },

  async addCustomJavaPath(path: string): Promise<JavaRuntimeInfo> {
    const b = getWailsBridge();
    if (b) return b.AddCustomJavaPath(path);
    const r = await reqJSON<{ ok: true; runtime: JavaRuntimeInfo }>("/api/java/add-custom", {
      method: "POST",
      body: JSON.stringify({ path }),
    });
    return r.runtime;
  },

  async verifyJavaRuntime(id: string): Promise<JavaRuntimeInfo> {
    const b = getWailsBridge();
    if (b) return b.VerifyJavaRuntime(id);
    const r = await reqJSON<{ ok: true; runtime: JavaRuntimeInfo }>("/api/java/verify", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.runtime;
  },

  async installJavaRuntime(component: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.InstallJavaRuntime(component);
      return;
    }
    await reqJSON<{ ok: true }>("/api/java/install", {
      method: "POST",
      body: JSON.stringify({ component }),
    });
  },

  async cancelJavaInstall(component: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.CancelJavaInstall(component);
      return;
    }
    await reqJSON<{ ok: true }>("/api/java/cancel-install", {
      method: "POST",
      body: JSON.stringify({ component }),
    });
  },

  async reinstallJavaRuntime(id: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.ReinstallJavaRuntime(id);
      return;
    }
    await reqJSON<{ ok: true }>("/api/java/reinstall", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
  },

  async removeJavaRuntime(id: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.RemoveJavaRuntime(id);
      return;
    }
    await reqJSON<{ ok: true }>("/api/java/remove", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
  },

  async recommendJavaForMC(mcVersion: string, loader = ""): Promise<JavaRecommendationResponse> {
    const b = getWailsBridge();
    if (b) return b.RecommendJavaForMC(mcVersion, loader);
    const qs = new URLSearchParams({ mc: mcVersion, loader });
    return reqJSON<JavaRecommendationResponse>(`/api/java/recommend?${qs.toString()}`);
  },

  // --- Instance Manager API ---

  async getInstances(): Promise<InstancesSnapshot> {
    const b = getWailsBridge();
    if (b) return b.GetInstances();
    const r = await reqJSON<{ ok: true; instances: GameInstance[]; paths: InstancesSnapshot["paths"] }>("/api/instances");
    return { instances: r.instances || [], paths: r.paths };
  },

  async getInstance(id: string): Promise<GameInstance> {
    const b = getWailsBridge();
    if (b) return b.GetInstance(id);
    const r = await reqJSON<{ ok: true; instance: GameInstance }>(`/api/instances/${encodeURIComponent(id)}`);
    return r.instance;
  },

  async createInstance(req: CreateInstanceInput): Promise<GameInstance> {
    const b = getWailsBridge();
    if (b) return b.CreateInstance(req);
    const r = await reqJSON<{ ok: true; instance: GameInstance }>("/api/instances/create", {
      method: "POST",
      body: JSON.stringify(req),
    });
    return r.instance;
  },

  async updateInstance(req: UpdateInstanceInput): Promise<GameInstance> {
    const b = getWailsBridge();
    if (b) return b.UpdateInstance(req);
    const r = await reqJSON<{ ok: true; instance: GameInstance }>("/api/instances/update", {
      method: "POST",
      body: JSON.stringify(req),
    });
    return r.instance;
  },

  async duplicateInstance(req: DuplicateInstanceInput): Promise<GameInstance> {
    const b = getWailsBridge();
    if (b) return b.DuplicateInstance(req);
    const r = await reqJSON<{ ok: true; instance: GameInstance }>("/api/instances/duplicate", {
      method: "POST",
      body: JSON.stringify(req),
    });
    return r.instance;
  },

  async deleteInstance(req: DeleteInstanceInput): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.DeleteInstance(req);
      return;
    }
    await reqJSON<{ ok: true }>("/api/instances/delete", {
      method: "POST",
      body: JSON.stringify(req),
    });
  },

  async verifyInstance(id: string): Promise<InstanceIntegrityReport> {
    const b = getWailsBridge();
    if (b) return b.VerifyInstance(id);
    const r = await reqJSON<{ ok: true; report: InstanceIntegrityReport }>("/api/instances/verify", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.report;
  },

  async repairInstance(id: string): Promise<InstanceIntegrityReport> {
    const b = getWailsBridge();
    if (b) return b.RepairInstance(id);
    const r = await reqJSON<{ ok: true; report: InstanceIntegrityReport }>("/api/instances/repair", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.report;
  },

  async getInstanceFiles(id: string): Promise<InstanceFilesSummary> {
    const b = getWailsBridge();
    if (b) return b.GetInstanceFiles(id);
    const r = await reqJSON<{ ok: true; files: InstanceFilesSummary }>(`/api/instances/${encodeURIComponent(id)}/files`);
    return r.files;
  },

  async listInstanceMods(id: string): Promise<InstalledModRef[]> {
    const b = getWailsBridge();
    if (b) return b.ListInstanceMods(id);
    const r = await reqJSON<{ ok: true; mods: InstalledModRef[] }>(`/api/instances/${encodeURIComponent(id)}/mods`);
    return r.mods || [];
  },

  async setInstanceModEnabled(id: string, filename: string, enabled: boolean): Promise<InstalledModRef[]> {
    const b = getWailsBridge();
    const req = { id, filename, enabled };
    if (b) return b.SetInstanceModEnabled(req);
    const r = await reqJSON<{ ok: true; mods: InstalledModRef[] }>("/api/instances/mods/toggle", { method: "POST", body: JSON.stringify(req) });
    return r.mods || [];
  },

  async deleteInstanceMod(id: string, filename: string): Promise<InstalledModRef[]> {
    const b = getWailsBridge();
    const req = { id, filename };
    if (b) return b.DeleteInstanceMod(req);
    const r = await reqJSON<{ ok: true; mods: InstalledModRef[] }>("/api/instances/mods/delete", { method: "POST", body: JSON.stringify(req) });
    return r.mods || [];
  },

  async setAllInstanceModsEnabled(id: string, enabled: boolean): Promise<InstalledModRef[]> {
    const b = getWailsBridge();
    if (b) return b.SetAllInstanceModsEnabled(id, enabled);
    const r = await reqJSON<{ ok: true; mods: InstalledModRef[] }>("/api/instances/mods/toggle-all", { method: "POST", body: JSON.stringify({ id, enabled }) });
    return r.mods || [];
  },

  async deleteMultipleInstanceMods(id: string, filenames: string[]): Promise<InstalledModRef[]> {
    const b = getWailsBridge();
    const req = { id, filenames };
    if (b) return b.DeleteMultipleInstanceMods(req);
    const r = await reqJSON<{ ok: true; mods: InstalledModRef[] }>("/api/instances/mods/delete-batch", { method: "POST", body: JSON.stringify(req) });
    return r.mods || [];
  },

  async checkInstanceModUpdates(id: string): Promise<ModUpdateInfo[]> {
    const b = getWailsBridge();
    if (b) return b.CheckInstanceModUpdates(id);
    const r = await reqJSON<{ ok: true; updates: ModUpdateInfo[] }>(`/api/instances/${encodeURIComponent(id)}/mods/updates`);
    return r.updates || [];
  },

  async applyInstanceModUpdate(req: ApplyModUpdateInput): Promise<InstanceModInstallResult> {
    const b = getWailsBridge();
    if (b) return b.ApplyInstanceModUpdate(req);
    const r = await reqJSON<{ ok: true; result: InstanceModInstallResult }>("/api/instances/mods/update", { method: "POST", body: JSON.stringify(req) });
    return r.result;
  },

  async installInstanceMod(req: InstallInstanceModInput): Promise<InstanceModInstallResult> {
    const b = getWailsBridge();
    if (b) return b.InstallInstanceMod(req);
    const r = await reqJSON<{ ok: true; result: InstanceModInstallResult }>("/api/instances/mods/install", { method: "POST", body: JSON.stringify(req) });
    return r.result;
  },

  async listInstanceResourcePacks(id: string): Promise<ResourcePackRef[]> {
    const b = getWailsBridge();
    if (b) return b.ListInstanceResourcePacks(id);
    const r = await reqJSON<{ ok: true; resourcepacks: ResourcePackRef[] }>(`/api/instances/${encodeURIComponent(id)}/resourcepacks`);
    return r.resourcepacks || [];
  },

  async setInstanceResourcePackEnabled(id: string, filename: string, enabled: boolean): Promise<ResourcePackRef[]> {
    const b = getWailsBridge();
    const req = { id, filename, enabled };
    if (b) return b.SetInstanceResourcePackEnabled(req);
    const r = await reqJSON<{ ok: true; resourcepacks: ResourcePackRef[] }>("/api/instances/resourcepacks/toggle", { method: "POST", body: JSON.stringify(req) });
    return r.resourcepacks || [];
  },

  async deleteInstanceResourcePack(id: string, filename: string): Promise<ResourcePackRef[]> {
    const b = getWailsBridge();
    const req = { id, filename };
    if (b) return b.DeleteInstanceResourcePack(req);
    const r = await reqJSON<{ ok: true; resourcepacks: ResourcePackRef[] }>("/api/instances/resourcepacks/delete", { method: "POST", body: JSON.stringify(req) });
    return r.resourcepacks || [];
  },

  async listInstanceShaderPacks(id: string): Promise<ShaderPackRef[]> {
    const b = getWailsBridge();
    if (b) return b.ListInstanceShaderPacks(id);
    const r = await reqJSON<{ ok: true; shaders: ShaderPackRef[] }>(`/api/instances/${encodeURIComponent(id)}/shaders`);
    return r.shaders || [];
  },

  async setInstanceShaderPackEnabled(id: string, filename: string, enabled: boolean): Promise<ShaderPackRef[]> {
    const b = getWailsBridge();
    const req = { id, filename, enabled };
    if (b) return b.SetInstanceShaderPackEnabled(req);
    const r = await reqJSON<{ ok: true; shaders: ShaderPackRef[] }>("/api/instances/shaders/toggle", { method: "POST", body: JSON.stringify(req) });
    return r.shaders || [];
  },

  async deleteInstanceShaderPack(id: string, filename: string): Promise<ShaderPackRef[]> {
    const b = getWailsBridge();
    const req = { id, filename };
    if (b) return b.DeleteInstanceShaderPack(req);
    const r = await reqJSON<{ ok: true; shaders: ShaderPackRef[] }>("/api/instances/shaders/delete", { method: "POST", body: JSON.stringify(req) });
    return r.shaders || [];
  },

  async listInstanceWorlds(id: string): Promise<WorldRef[]> {
    const b = getWailsBridge();
    if (b) return b.ListInstanceWorlds(id);
    const r = await reqJSON<{ ok: true; worlds: WorldRef[] }>(`/api/instances/${encodeURIComponent(id)}/worlds`);
    return r.worlds || [];
  },

  async exportInstanceWorld(id: string, folder: string): Promise<ExportWorldResult> {
    const b = getWailsBridge();
    if (b) return b.ExportInstanceWorld(id, folder);
    const r = await reqJSON<{ ok: true; export: ExportWorldResult }>("/api/instances/worlds/export", { method: "POST", body: JSON.stringify({ id, folder }) });
    if (r.export.download_url) {
      const link = document.createElement("a");
      link.href = r.export.download_url;
      link.download = r.export.filename;
      document.body.appendChild(link);
      link.click();
      link.remove();
    }
    return r.export;
  },

  async getMinecraftManifest(forceRefresh = false): Promise<MCVersionManifestResult> {
    const b = getWailsBridge();
    if (b) return b.GetMinecraftManifest(forceRefresh);
    return reqJSON<MCVersionManifestResult>(`/api/minecraft/versions?refresh=${forceRefresh ? "1" : "0"}`);
  },

  async getModloaderVersions(loader: string, mcVersion: string): Promise<LoaderVersionsResult> {
    const b = getWailsBridge();
    if (b) return b.GetModloaderVersions(loader, mcVersion);
    const qs = new URLSearchParams({ loader, mc: mcVersion });
    return reqJSON<LoaderVersionsResult>(`/api/modloaders/versions?${qs.toString()}`);
  },

  async installProfileVersion(req: InstallProfileVersionInput): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.InstallProfileVersion(req);
      return;
    }
    await reqJSON<{ ok: true }>("/api/instances/install-version", { method: "POST", body: JSON.stringify(req) });
  },

  async cancelProfileInstall(instanceId: string): Promise<boolean> {
    const b = getWailsBridge();
    if (b) return b.CancelProfileInstall(instanceId);
    const r = await reqJSON<{ ok: true; cancelled: boolean }>("/api/instances/cancel-install", {
      method: "POST",
      body: JSON.stringify({ instance_id: instanceId }),
    });
    return Boolean(r.cancelled);
  },

  async getActiveProfileInstall(instanceId: string): Promise<InstallProgressInfo | null> {
    const b = getWailsBridge();
    if (b) return b.GetActiveProfileInstall(instanceId);
    const r = await reqJSON<{ ok: true; progress: InstallProgressInfo | null }>(`/api/instances/${encodeURIComponent(instanceId)}/active-install`);
    return r.progress || null;
  },

  async listProfileInstalledVersions(instanceId: string): Promise<InstalledProfileVersion[]> {
    const b = getWailsBridge();
    if (b) return (await b.ListProfileInstalledVersions(instanceId)) || [];
    const r = await reqJSON<{ ok: true; versions: InstalledProfileVersion[] }>(`/api/instances/${encodeURIComponent(instanceId)}/installed-versions`);
    return r.versions || [];
  },

  async activateProfileVersion(instanceId: string, installedId: string, createBackup: boolean): Promise<InstalledProfileVersion> {
    const b = getWailsBridge();
    if (b) return b.ActivateProfileVersion(instanceId, installedId, createBackup);
    const r = await reqJSON<{ ok: true; version: InstalledProfileVersion }>("/api/instances/activate-version", {
      method: "POST",
      body: JSON.stringify({ instance_id: instanceId, installed_id: installedId, create_backup: createBackup }),
    });
    return r.version;
  },

  async verifyProfileVersion(instanceId: string, installedId: string): Promise<FullVersionVerificationReport> {
    const b = getWailsBridge();
    if (b) return b.VerifyProfileVersion(instanceId, installedId);
    const r = await reqJSON<{ ok: true; report: FullVersionVerificationReport }>("/api/instances/verify-version", {
      method: "POST",
      body: JSON.stringify({ instance_id: instanceId, installed_id: installedId }),
    });
    return r.report;
  },

  async deleteProfileInstalledVersion(instanceId: string, installedId: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.DeleteProfileInstalledVersion(instanceId, installedId);
      return;
    }
    await reqJSON<{ ok: true }>("/api/instances/delete-version", {
      method: "POST",
      body: JSON.stringify({ instance_id: instanceId, installed_id: installedId }),
    });
  },

  // --- Microsoft Account Manager API (strictly token-free) ---

  async getAccounts(): Promise<AccountsSnapshot> {
    const b = getWailsBridge();
    if (b) return b.GetAccounts();
    return reqJSON<AccountsSnapshot>("/api/accounts");
  },

  async startMicrosoftLogin(reauthAccountId = ""): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.StartMicrosoftLogin(reauthAccountId);
      return;
    }
    await reqJSON<{ ok: true }>("/api/accounts/login/start", {
      method: "POST",
      body: JSON.stringify({ reauth_account_id: reauthAccountId }),
    });
  },

  async addOfflineAccount(username: string): Promise<AccountView> {
    const b = getWailsBridge();
    if (b) {
      return b.AddOfflineAccount(username);
    }
    const r = await reqJSON<{ ok: true; account: AccountView }>("/api/accounts/offline", {
      method: "POST",
      body: JSON.stringify({ username }),
    });
    return r.account;
  },

  async cancelMicrosoftLogin(): Promise<boolean> {
    const b = getWailsBridge();
    if (b) return b.CancelMicrosoftLogin();
    const r = await reqJSON<{ ok: true; cancelled: boolean }>("/api/accounts/login/cancel", {
      method: "POST",
      body: JSON.stringify({}),
    });
    return Boolean(r.cancelled);
  },

  async setActiveAccount(id: string): Promise<AccountView> {
    const b = getWailsBridge();
    if (b) return b.SetActiveAccount(id);
    const r = await reqJSON<{ ok: true; account: AccountView }>("/api/accounts/active", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.account;
  },

  async refreshAccountAuth(id: string, force = true): Promise<AccountView> {
    const b = getWailsBridge();
    if (b) return b.RefreshAccountAuth(id, force);
    const r = await reqJSON<{ ok: true; account: AccountView }>("/api/accounts/refresh", {
      method: "POST",
      body: JSON.stringify({ id, force }),
    });
    return r.account;
  },

  async checkAccountLicense(id: string): Promise<AccountView> {
    const b = getWailsBridge();
    if (b) return b.CheckAccountLicense(id);
    const r = await reqJSON<{ ok: boolean; account: AccountView; error?: string }>("/api/accounts/check-license", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    if (!r.ok && r.error) throw new Error(r.error);
    return r.account;
  },

  async signOutAccount(id: string): Promise<AccountView> {
    const b = getWailsBridge();
    if (b) return b.SignOutAccount(id);
    const r = await reqJSON<{ ok: true; account: AccountView }>("/api/accounts/signout", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
    return r.account;
  },

  async deleteAccount(id: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.DeleteAccount(id);
      return;
    }
    await reqJSON<{ ok: true }>("/api/accounts/delete", {
      method: "POST",
      body: JSON.stringify({ id }),
    });
  },

  async updateAccountSkin(id: string, skinURL: string, skinVariant: string, capeURL = "", capeAlias = ""): Promise<AccountView> {
    const b = getWailsBridge();
    if (b) return b.UpdateAccountSkin(id, skinURL, skinVariant, capeURL, capeAlias);
    const r = await reqJSON<{ ok: true; account: AccountView }>(`/api/accounts/${encodeURIComponent(id)}/skin`, {
      method: "POST",
      body: JSON.stringify({ skin_url: skinURL, skin_variant: skinVariant, cape_url: capeURL, cape_alias: capeAlias }),
    });
    return r.account;
  },

  // --- Game Launcher & Process Monitoring API ---

  async getLauncherSnapshot(): Promise<LauncherSnapshot> {
    const b = getWailsBridge();
    if (b) return b.GetLauncherSnapshot();
    return reqJSON<LauncherSnapshot>("/api/launcher/snapshot");
  },

  async preflightLaunch(req: LaunchInstanceInput): Promise<PreflightReport> {
    const b = getWailsBridge();
    if (b) return b.PreflightLaunch(req);
    return reqJSON<PreflightReport>("/api/launcher/preflight", {
      method: "POST",
      body: JSON.stringify(req),
    });
  },

  async launchInstance(req: LaunchInstanceInput): Promise<GameSessionInfo> {
    const b = getWailsBridge();
    if (b) return b.LaunchInstance(req);
    const r = await reqJSON<{ ok: true; session: GameSessionInfo }>("/api/launcher/launch", {
      method: "POST",
      body: JSON.stringify(req),
    });
    return r.session;
  },

  async stopInstance(instanceId: string, force = false): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.StopInstance(instanceId, force);
      return;
    }
    await reqJSON<{ ok: true }>("/api/launcher/stop", {
      method: "POST",
      body: JSON.stringify({ instance_id: instanceId, force }),
    });
  },

  async getInstanceSession(instanceId: string): Promise<GameSessionInfo | null> {
    const b = getWailsBridge();
    if (b) return b.GetInstanceSession(instanceId);
    const r = await reqJSON<{ ok: true; session: GameSessionInfo | null }>(
      `/api/launcher/instances/${encodeURIComponent(instanceId)}/session`
    );
    return r.session || null;
  },

  async getInstanceSessionHistory(instanceId: string, limit = 15): Promise<GameSessionInfo[]> {
    const b = getWailsBridge();
    if (b) return (await b.GetInstanceSessionHistory(instanceId, limit)) || [];
    const r = await reqJSON<{ ok: true; sessions: GameSessionInfo[] }>(
      `/api/launcher/instances/${encodeURIComponent(instanceId)}/history?limit=${limit}`
    );
    return r.sessions || [];
  },

  async getInstanceLogs(instanceId: string, limit = 1500): Promise<GameLogLine[]> {
    const b = getWailsBridge();
    if (b) return (await b.GetInstanceLogs(instanceId, limit)) || [];
    const r = await reqJSON<{ ok: true; lines: GameLogLine[] }>(
      `/api/launcher/instances/${encodeURIComponent(instanceId)}/logs?limit=${limit}`
    );
    return r.lines || [];
  },

  async listInstanceCrashReports(instanceId: string): Promise<CrashReportInfo[]> {
    const b = getWailsBridge();
    if (b) return (await b.ListInstanceCrashReports(instanceId)) || [];
    const r = await reqJSON<{ ok: true; crash_reports: CrashReportInfo[] }>(
      `/api/launcher/instances/${encodeURIComponent(instanceId)}/crashes`
    );
    return r.crash_reports || [];
  },

  async analyzeInstanceCrash(instanceId: string): Promise<CrashDiagnosis | null> {
    const b = getWailsBridge();
    if (b) return (await b.AnalyzeInstanceCrash(instanceId)) || null;
    const r = await reqJSON<{ ok: true; diagnosis: CrashDiagnosis | null }>(
      `/api/launcher/instances/${encodeURIComponent(instanceId)}/crash-diagnosis`
    );
    return r.diagnosis || null;
  },

  async listModpacks(): Promise<ModpackView[]> {
    const b = getWailsBridge();
    if (b) return (await b.ListModpacks()) || [];
    const r = await reqJSON<{ ok: true; modpacks: ModpackView[] }>("/api/modpacks");
    return r.modpacks || [];
  },

  async deleteModpackRecord(id: string): Promise<void> {
    const b = getWailsBridge();
    if (b) { await b.DeleteModpackRecord(id); return; }
    await reqJSON<{ ok: true }>("/api/modpacks/delete", { method: "POST", body: JSON.stringify({ id }) });
  },

  async browseModpackArchive(): Promise<string> {
    const b = getWailsBridge();
    if (b) return b.BrowseModpackArchive();
    return "";
  },

  async previewModpackImport(req: PreviewImportInput): Promise<ImportPreview> {
    const b = getWailsBridge();
    if (b) return b.PreviewModpackImport(req);
    return reqJSON<ImportPreview>("/api/modpacks/preview-import", { method: "POST", body: JSON.stringify(req) });
  },

  async uploadModpackArchive(file: File): Promise<ImportPreview> {
    if (getWailsBridge()) throw new Error("Archive upload is available in web mode only");
    const form = new FormData();
    form.append("archive", file);
    const res = await fetch("/api/modpacks/upload-import", { method: "POST", body: form });
    if (!res.ok) {
      const body = await res.json().catch(() => ({})) as { error?: string };
      throw new Error(body.error || `Upload failed (${res.status})`);
    }
    return res.json() as Promise<ImportPreview>;
  },

  async executeModpackImport(req: ExecuteImportInput): Promise<void> {
    const b = getWailsBridge();
    if (b) { await b.ExecuteModpackImport(req); return; }
    await reqJSON<{ ok: true }>("/api/modpacks/execute-import", { method: "POST", body: JSON.stringify(req) });
  },

  async getActiveModpackProgress(): Promise<ModpackImportProgress | null> {
    const b = getWailsBridge();
    if (b) return b.GetActiveModpackProgress();
    const r = await reqJSON<{ ok: true; progress: ModpackImportProgress | null }>("/api/modpacks/progress");
    return r.progress || null;
  },

  async exportModpack(req: ExportModpackInput): Promise<ExportModpackResult> {
    const b = getWailsBridge();
    if (b) return b.ExportModpack(req);
    const result = await reqJSON<ExportModpackResult>("/api/modpacks/export", { method: "POST", body: JSON.stringify(req) });
    if (result.download_url) {
      const link = document.createElement("a");
      link.href = result.download_url;
      link.download = result.output_path?.split(/[\\/]/).pop() || "modhub-pack.zip";
      document.body.appendChild(link);
      link.click();
      link.remove();
    }
    return result;
  },

  async previewModpackUpdate(modpackId: string, newArchivePath = ""): Promise<UpdateDiffPreview> {
    const b = getWailsBridge();
    if (b) return b.PreviewModpackUpdate(modpackId, newArchivePath);
    return reqJSON<UpdateDiffPreview>("/api/modpacks/preview-update", {
      method: "POST",
      body: JSON.stringify({ modpack_id: modpackId, new_archive_path: newArchivePath }),
    });
  },

  async applyModpackUpdate(req: ApplyUpdateInput): Promise<ModpackView> {
    const b = getWailsBridge();
    if (b) return b.ApplyModpackUpdate(req);
    const r = await reqJSON<{ ok: true; modpack: ModpackView }>("/api/modpacks/apply-update", { method: "POST", body: JSON.stringify(req) });
    return r.modpack;
  },
};
