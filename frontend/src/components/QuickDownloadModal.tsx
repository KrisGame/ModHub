import React, { useEffect, useMemo, useState } from "react";
import { api } from "../api";
import { useOverlayTransition } from "../hooks";
import { CloseIcon, LoaderIcon, PlatformPill } from "../icons";
import type { DepTreeNode, ModDetailsResponse, ProjectCard, VersionFile } from "../types";
import {
  cachedIconUrl,
  extractDominantRgbFromImg,
  fallbackIconDataUri,
  fallbackRgbFromTitle,
  formatBytes,
  getCachedAmbientRgb,
  formatDate,
  formatLoaderName,
  REAL_LOADERS,
} from "../utils";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";
import { DependencyTreeList } from "./DependencyTreeList";
import { useDependencyTree } from "../hooks/useDependencyTree";
import { installModWithDependencies } from "../utils/profileModInstall";

interface QuickDownloadModalProps {
  item: ProjectCard | null;
  preloadedDetails?: ModDetailsResponse | null;
  initialMcVersion: string;
  initialLoader: string;
  onClose: () => void;
  onItemMerged?: (merged: ProjectCard) => void;
  onOpenFullFiles: (item: ProjectCard) => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
  modInstallTarget?: { id: string; name: string } | null;
  onClearModInstall?: () => void;
}

function parseVerTuple(v: string): [number, number, number] {
  const nums = String(v || "")
    .split(".")
    .map((p) => Number.parseInt(p, 10) || 0);
  return [nums[0] || 0, nums[1] || 0, nums[2] || 0];
}

function compareMcDesc(a: string, b: string): number {
  const ta = parseVerTuple(a);
  const tb = parseVerTuple(b);
  if (ta[0] !== tb[0]) return tb[0] - ta[0];
  if (ta[1] !== tb[1]) return tb[1] - ta[1];
  return tb[2] - ta[2];
}

function releaseWeight(rt: string): number {
  const low = String(rt || "").toLowerCase();
  if (low === "release") return 3;
  if (low === "beta") return 2;
  return 1;
}

export const QuickDownloadModal: React.FC<QuickDownloadModalProps> = ({
  item,
  preloadedDetails = null,
  initialMcVersion,
  initialLoader,
  onClose,
  onItemMerged,
  onOpenFullFiles,
  onToast,
  modInstallTarget = null,
  onClearModInstall,
}) => {
  const isOpen = Boolean(item);
  const { mounted, shown } = useOverlayTransition(isOpen, 230);
  const [activeItem, setActiveItem] = useState<ProjectCard | null>(preloadedDetails?.merged_item || item);
  const [versions, setVersions] = useState<VersionFile[]>(() => preloadedDetails?.versions || []);
  const [loading, setLoading] = useState(() => Boolean(item && !preloadedDetails?.versions));
  const [error, setError] = useState("");
  const [selectedMc, setSelectedMc] = useState(initialMcVersion || "");
  const [selectedLoader, setSelectedLoader] = useState(initialLoader || "");
  const [selectedVersionKey, setSelectedVersionKey] = useState("");
  const [headerGlowRgb, setHeaderGlowRgb] = useState("140, 152, 170");
  const [dlStarted, setDlStarted] = useState(false);
  const [dlPhase, setDlPhase] = useState("");
  const [dlErrorMsg, setDlErrorMsg] = useState("");
  const [pendingMediumDeps, setPendingMediumDeps] = useState<DepTreeNode[]>([]);
  const [projectBody, setProjectBody] = useState(() => preloadedDetails?.body || "");

  useEffect(() => {
    if (!item) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [item, onClose]);

  useEffect(() => {
    if (!item) return;
    setError("");
    setSelectedMc(initialMcVersion || "");
    setSelectedLoader(initialLoader || "");
    setSelectedVersionKey("");
    setDlStarted(false);
    setDlPhase("");
    setDlErrorMsg("");
    setPendingMediumDeps([]);
    setProjectBody(preloadedDetails?.body || "");
    const rgbKey = item.icon_url || item.group_id;
    setHeaderGlowRgb(getCachedAmbientRgb(rgbKey) || fallbackRgbFromTitle(item.title));

    // Use preloaded unified details+merge+versions directly -> ZERO post-load!
    const ready = preloadedDetails || api.getCachedModDetails(item);
    if (ready && Array.isArray(ready.versions)) {
      setActiveItem(ready.merged_item || item);
      setProjectBody(ready.body || "");
      setVersions(ready.versions);
      setLoading(false);
      return;
    }

    const ctrl = new AbortController();
    setActiveItem(item);
    setVersions([]);
    setLoading(true);

    (async () => {
      try {
        const det = await api.getModDetails(item, ctrl.signal);
        const merged = det.merged_item || item;
        setActiveItem(merged);
        setProjectBody(det.body || "");
        if (det.merged_item && onItemMerged && (det.merged_item.sources?.length || 0) > (item.sources?.length || 0)) {
          onItemMerged(det.merged_item);
        }
        if (Array.isArray(det.versions)) {
          setVersions(det.versions);
          setLoading(false);
          return;
        }
        const vRes = await api.getModVersions(
          {
            item: merged,
            sources: merged.sources || [],
            content_type: merged.content_type || "mod",
            preload_all: true,
            sort: "mc_version_desc",
          },
          ctrl.signal
        );
        setVersions(vRes.items || []);
        setLoading(false);
      } catch (err) {
        if ((err as Error).name === "AbortError") return;
        setError(String((err as Error).message || err));
        setLoading(false);
      }
    })();

    return () => ctrl.abort();
  }, [item?.group_id, preloadedDetails]);

  const ct = activeItem?.content_type || "mod";
  const usesLoader = ct === "mod" || ct === "datapack";

  // ALL MC versions across ALL merged sources (sorted newest first)
  const allMcVersions = useMemo<string[]>(() => {
    const set = new Set<string>();
    for (const v of versions) {
      for (const gv of v.game_versions || []) {
        if (gv) set.add(gv);
      }
    }
    return Array.from(set).sort(compareMcDesc);
  }, [versions]);

  // ALL loaders supported across ALL merged sources so the user ALWAYS sees every platform's loaders!
  const allLoaders = useMemo<string[]>(() => {
    if (!usesLoader) return [];
    const set = new Set<string>();
    for (const v of versions) {
      for (const l of v.loaders || []) {
        const low = l.toLowerCase();
        if ((REAL_LOADERS as readonly string[]).includes(low)) set.add(low);
      }
    }
    return (REAL_LOADERS as readonly string[]).filter((kl) => set.has(kl));
  }, [versions, usesLoader]);

  // Helper: newest MC version that supports a given loader
  const newestMcForLoader = (ld: string): string => {
    const set = new Set<string>();
    for (const v of versions) {
      if (v.loaders?.some((l) => l.toLowerCase() === ld.toLowerCase())) {
        for (const gv of v.game_versions || []) {
          if (gv) set.add(gv);
        }
      }
    }
    const sorted = Array.from(set).sort(compareMcDesc);
    return sorted[0] || allMcVersions[0] || "";
  };

  // Helper: loaders supported for a specific MC version
  const loadersForMc = (mc: string): string[] => {
    const set = new Set<string>();
    for (const v of versions) {
      if (mc && v.game_versions?.length && !v.game_versions.includes(mc)) continue;
      for (const l of v.loaders || []) {
        const low = l.toLowerCase();
        if ((REAL_LOADERS as readonly string[]).includes(low)) set.add(low);
      }
    }
    return (REAL_LOADERS as readonly string[]).filter((kl) => set.has(kl));
  };

  // Smart initial selection once versions arrive
  useEffect(() => {
    if (!versions.length) return;

    let nextLoader = selectedLoader;
    if (usesLoader && allLoaders.length > 0) {
      if (!nextLoader || !allLoaders.includes(nextLoader)) {
        nextLoader = initialLoader && allLoaders.includes(initialLoader) ? initialLoader : allLoaders[0];
      }
    } else {
      nextLoader = "";
    }

    let nextMc = selectedMc;
    if (allMcVersions.length > 0) {
      const validMc = nextMc && allMcVersions.includes(nextMc);
      if (!validMc) {
        if (initialMcVersion && allMcVersions.includes(initialMcVersion)) {
          nextMc = initialMcVersion;
        } else if (nextLoader) {
          nextMc = newestMcForLoader(nextLoader);
        } else {
          nextMc = allMcVersions[0];
        }
      }
      // If both chosen but combo has zero files, align MC to newest for that loader
      if (nextLoader && nextMc && !loadersForMc(nextMc).includes(nextLoader)) {
        const fallbackLds = loadersForMc(nextMc);
        if (initialMcVersion && nextMc === initialMcVersion && fallbackLds.length > 0) {
          nextLoader = fallbackLds[0];
        } else {
          nextMc = newestMcForLoader(nextLoader);
        }
      }
    }

    if (nextMc !== selectedMc) setSelectedMc(nextMc);
    if (nextLoader !== selectedLoader) setSelectedLoader(nextLoader);
  }, [versions, allMcVersions, allLoaders, usesLoader, initialMcVersion, initialLoader]);

  const resetDownloadNotice = () => {
    setDlStarted(false);
    setDlErrorMsg("");
    setPendingMediumDeps([]);
  };

  const handleMcChange = (gv: string) => {
    setSelectedMc(gv);
    setSelectedVersionKey("");
    resetDownloadNotice();
    if (usesLoader && allLoaders.length > 0) {
      const supported = loadersForMc(gv);
      if (supported.length > 0 && (!selectedLoader || !supported.includes(selectedLoader))) {
        setSelectedLoader(supported[0]);
      }
    }
  };

  const handleLoaderChange = (ld: string) => {
    setSelectedLoader(ld);
    setSelectedVersionKey("");
    resetDownloadNotice();
    if (selectedMc && !loadersForMc(selectedMc).includes(ld)) {
      const bestMc = newestMcForLoader(ld);
      if (bestMc) setSelectedMc(bestMc);
    }
  };

  const matchingFiles = useMemo(() => {
    const filtered = versions.filter((v) => {
      if (selectedMc && v.game_versions?.length && !v.game_versions.includes(selectedMc)) return false;
      if (usesLoader && selectedLoader && allLoaders.length > 0) {
        if (!v.loaders?.some((l) => l.toLowerCase() === selectedLoader.toLowerCase())) return false;
      }
      return true;
    });
    return [...filtered].sort((a, b) => {
      if (a.is_available !== b.is_available) return a.is_available ? -1 : 1;
      const rw = releaseWeight(b.release_type) - releaseWeight(a.release_type);
      if (rw !== 0) return rw;
      return String(b.date_published || "").localeCompare(String(a.date_published || ""));
    });
  }, [versions, selectedMc, selectedLoader, usesLoader, allLoaders.length]);

  const chosenFile = useMemo(() => {
    if (!matchingFiles.length) return null;
    if (selectedVersionKey) {
      const exact = matchingFiles.find((v) => `${v.platform}:${v.version_id}` === selectedVersionKey);
      if (exact) return exact;
    }
    return matchingFiles[0];
  }, [matchingFiles, selectedVersionKey]);

  const mcOptions = useMemo<CustomSelectOption[]>(
    () => allMcVersions.map((gv) => ({ value: gv, label: `Minecraft ${gv}` })),
    [allMcVersions]
  );

  const loaderOptions = useMemo<CustomSelectOption[]>(
    () =>
      allLoaders.map((ld) => ({
        value: ld,
        label: formatLoaderName(ld),
        icon: <LoaderIcon loader={ld} size={13.5} />,
      })),
    [allLoaders]
  );

  const buildOptions = useMemo<CustomSelectOption[]>(
    () =>
      matchingFiles.slice(0, 24).map((f) => ({
        value: `${f.platform}:${f.version_id}`,
        label: f.filename,
        icon: <span className={`channel-dot dot-${f.release_type || "release"}`} />,
        sub: `${formatBytes(f.file_size)} • ${f.platform === "modrinth" ? "MR" : "CF"}`,
      })),
    [matchingFiles]
  );

  const chosenDeps = useMemo(
    () => (chosenFile?.dependencies || []).filter((d) => d.project_id || d.version_id),
    [chosenFile?.dependencies]
  );
  const qdOwnerSig = `${activeItem?.group_id || ""}|${chosenFile?.platform || ""}|${chosenFile?.version_id || ""}`;
  const depState = useDependencyTree({
    enabled: Boolean(isOpen && chosenFile),
    ownerKey: qdOwnerSig,
    dependencies: chosenDeps,
    mcVersion: selectedMc || chosenFile?.game_versions?.[0] || "",
    loader: selectedLoader || chosenFile?.loaders?.[0] || "",
    fileURL: chosenFile?.download_url || "",
    platform: chosenFile?.platform || "",
    projectID: chosenFile?.mod_id || "",
    title: activeItem?.title || "",
    summary: activeItem?.summary || "",
    body: projectBody,
    changelog: chosenFile?.changelog || "",
    maxDepth: 2,
  });
  const reqDepCount = depState.tree.filter((d) => d.type === "required").length;
  const showDepsBox =
    depState.loading ||
    depState.tree.length > 0 ||
    depState.unresolved.length > 0 ||
    depState.warnings.length > 0 ||
    Object.keys(depState.errors).length > 0 ||
    Boolean(depState.fatalError);

  const triggerDownload = async (file: VersionFile) => {
    if (!activeItem || !modInstallTarget) {
      onToast("Откройте профиль → «Установить из каталога», затем выберите мод", "info");
      return;
    }
    setDlErrorMsg("");
    setPendingMediumDeps([]);
    setDlPhase("Добавление в профиль…");
    try {
      const result = await installModWithDependencies({ target: modInstallTarget, project: activeItem, file, mcVersion: selectedMc, loader: selectedLoader, body: projectBody });
      setDlStarted(true);
      onToast(`В профиль «${modInstallTarget.name}» добавлено в очередь: ${result.queued} файлов, зависимостей: ${result.dependencies}`, "ok");
      window.setTimeout(() => { setDlStarted(false); onClose(); }, 700);
    } catch (err) {
      const msg = `Ошибка установки: ${(err as Error).message}`;
      setDlErrorMsg(msg);
      onToast(msg, "err");
    } finally {
      setDlPhase("");
    }
  };

  if (!mounted || !activeItem) return null;

  return (
    <div
      className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-hidden={!shown}
      onClick={onClose}
    >
      <div className="modal-window modal-qd" onClick={(e) => e.stopPropagation()}>
        <div
          className="modal-header qd-header"
          style={{ "--qd-glow-rgb": headerGlowRgb } as React.CSSProperties}
        >
          <div className="qd-header-glow" aria-hidden="true" />
          <div className="qd-header-left">
            <img
              width={44}
              height={44}
              className="qd-icon"
              src={activeItem.icon_url ? cachedIconUrl(activeItem.icon_url, 112) : fallbackIconDataUri(activeItem.title)}
              alt=""
              onLoad={(e) => {
                const key = activeItem.icon_url || activeItem.group_id;
                setHeaderGlowRgb(extractDominantRgbFromImg(e.currentTarget, key, activeItem.title));
              }}
              onError={(e) => {
                e.currentTarget.src = fallbackIconDataUri(activeItem.title);
              }}
            />
            <div style={{ minWidth: 0 }}>
              <h2 className="modal-title" style={{ fontSize: 16.5 }}>Установить {activeItem.title} в профиль</h2>
              <p className="modal-subtitle">Выберите версию Minecraft{usesLoader ? " и загрузчик" : ""}</p>
            </div>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <div className="modal-body qd-body">
          {modInstallTarget && (
            <div className="mh-info-callout" style={{ marginBottom: 10, position: "sticky", top: 0, zIndex: 3 }}>
              Установка в профиль «{modInstallTarget.name}»
              <button type="button" className="btn btn-ghost btn-xs" onClick={onClearModInstall}>Отменить</button>
            </div>
          )}
          {loading ? (
            <div className="empty-state" style={{ padding: "32px 16px" }}>
              <span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />
              Загрузка доступных версий...
            </div>
          ) : error ? (
            <div className="empty-state" style={{ padding: "28px 16px" }}>
              Ошибка загрузки версий: {error}
            </div>
          ) : !versions.length ? (
            <div className="empty-state" style={{ padding: "28px 16px" }}>
              У этого проекта нет доступных файлов для установки.
            </div>
          ) : (
            <>
              <div className="qd-selects-grid">
                <div className="qd-section">
                  <span className="qd-label">Версия Minecraft</span>
                  <CustomSelect
                    value={selectedMc}
                    options={mcOptions}
                    onChange={handleMcChange}
                    searchable
                    searchPlaceholder="Найти версию..."
                    mono
                    ariaLabel="Версия Minecraft"
                  />
                </div>

                {usesLoader && loaderOptions.length > 0 && (
                  <div className="qd-section">
                    <span className="qd-label">Загрузчик</span>
                    <CustomSelect
                      value={selectedLoader}
                      options={loaderOptions}
                      onChange={handleLoaderChange}
                      ariaLabel="Загрузчик"
                    />
                    <span className="qd-helper-text">Выберите тот же загрузчик, который указан в профиле вашей сборки Minecraft.</span>
                  </div>
                )}
              </div>

              {chosenFile ? (
                <div className="qd-file-card">
                  <div className="qd-file-top">
                    <div className="qd-file-title-row">
                      <span className="file-bracket-tag mono-num">
                        [{[selectedMc, selectedLoader ? formatLoaderName(selectedLoader) : ""].filter(Boolean).join(" • ")}]
                      </span>
                      <span className={`pill pill-${chosenFile.release_type || "release"}`}>
                        <span className={`channel-dot dot-${chosenFile.release_type || "release"}`} aria-hidden="true" />
                        <span>{chosenFile.release_type || "release"}</span>
                      </span>
                      <PlatformPill platform={chosenFile.platform} href={chosenFile.fallback_url || undefined} showArrow={Boolean(chosenFile.fallback_url)} size={12} />
                    </div>
                    <div className="qd-file-name" title={chosenFile.filename}>
                      {chosenFile.filename}
                    </div>
                    <div className="qd-summary-grid mono-num">
                      <div className="qd-summary-kv">
                        <span>Версия</span>
                        <strong>{chosenFile.version_number || chosenFile.name || "—"}</strong>
                      </div>
                      <div className="qd-summary-kv">
                        <span>Источник</span>
                        <strong>{chosenFile.platform === "modrinth" ? "Modrinth" : "CurseForge"}</strong>
                      </div>
                      <div className="qd-summary-kv">
                        <span>Размер</span>
                        <strong>{formatBytes(chosenFile.file_size)}</strong>
                      </div>
                      <div className="qd-summary-kv">
                        <span>Опубликован</span>
                        <strong>{formatDate(chosenFile.date_published)}</strong>
                      </div>
                    </div>
                  </div>

                  {showDepsBox && (
                    <div className="qd-deps-box">
                      <div className="qd-deps-head">
                        <span>Зависимости файла{reqDepCount > 0 ? ` (обязательных: ${reqDepCount})` : ""}</span>
                      </div>
                      <DependencyTreeList
                        tree={depState.tree}
                        unresolved={depState.unresolved}
                        warnings={depState.warnings}
                        errors={depState.errors}
                        status={depState.status}
                        loading={depState.loading}
                        fatalError={depState.fatalError}
                        compact
                        onRetry={depState.retry}
                      />
                    </div>
                  )}

                  {buildOptions.length > 1 && (
                    <CustomSelect
                      value={`${chosenFile.platform}:${chosenFile.version_id}`}
                      options={buildOptions}
                      onChange={(val) => {
                        setSelectedVersionKey(val);
                        resetDownloadNotice();
                      }}
                      mono
                      ariaLabel="Выбор версии файла"
                    />
                  )}

                  <div className="qd-origin-note">
                    {chosenFile.download_url
                      ? `Файл и обязательные зависимости будут добавлены в профиль «${modInstallTarget?.name || "…"}». Источник: ${chosenFile.platform === "modrinth" ? "Modrinth" : "CurseForge"}.`
                      : "Для этого файла недоступна прямая установка в профиль."}
                  </div>
                </div>
              ) : (
                <div className="empty-state" style={{ padding: "18px 12px" }}>
                  Нет файла для выбранной комбинации версии и загрузчика.
                </div>
              )}

              {pendingMediumDeps.length > 0 && chosenFile && (
                <div className="dep-confirm-box" role="region" aria-label="Подтверждение зависимостей из описания">
                  <div className="dep-confirm-title">
                    В описании найдены вероятные зависимости (средняя уверенность):
                  </div>
                  <div className="dep-confirm-list">
                    {pendingMediumDeps.map((d) => (
                      <div key={d.source_key} className="dep-confirm-item">
                        <strong>{d.title}</strong>
                        {d.evidence && <span className="dep-confirm-ev"> — «{d.evidence}»</span>}
                      </div>
                    ))}
                  </div>
                  <div className="dep-confirm-actions">
                    <button
                      type="button"
                      className="btn btn-primary btn-xs"
                      onClick={() => void triggerDownload(chosenFile)}
                    >
                      Добавить предложенные зависимости
                    </button>
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs"
                      onClick={() => void triggerDownload(chosenFile)}
                    >
                      Продолжить без подтверждения
                    </button>
                  </div>
                </div>
              )}

              {dlErrorMsg && (
                <div className="dep-warn-note" role="alert" style={{ marginBottom: 8 }}>
                  {dlErrorMsg}
                </div>
              )}

              <div className="qd-actions">
                <button
                  type="button"
                  className={`btn btn-primary qd-dl-btn${dlStarted ? " is-success" : ""}${dlPhase ? " is-loading" : ""}`}
                  disabled={!chosenFile || Boolean(dlPhase)}
                  aria-busy={Boolean(dlPhase)}
                  onClick={() => {
                    if (!chosenFile) return;
                    void triggerDownload(chosenFile);
                  }}
                >
                  {dlPhase ? (
                    <span className="spinner spinner-xs" aria-hidden="true" />
                  ) : (
                    <svg className="ui-ico dl-arrow-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round">
                      <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
                      <g className="dl-arrow-head">
                        <polyline points="7 10 12 15 17 10" />
                        <line x1="12" y1="15" x2="12" y2="3" />
                      </g>
                    </svg>
                  )}
                  <span>
                    {dlPhase
                      ? dlPhase
                      : dlStarted
                        ? "Добавлено в профиль ✓"
                        : chosenFile?.download_url
                          ? `Установить в профиль «${modInstallTarget?.name || "…"}»`
                          : "Нет прямого файла для установки"}
                  </span>
                </button>

                <div className="qd-actions-sub">
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    onClick={() => {
                      const target = activeItem;
                      onClose();
                      onOpenFullFiles(target);
                    }}
                  >
                    Все файлы →
                  </button>
                </div>
              </div>
            </>
          )}
        </div>
      </div>
    </div>
  );
};
