import React, { useEffect, useState } from "react";
import { api } from "../api";
import { useOverlayTransition } from "../hooks";
import { CloseIcon, LoaderIcon } from "../icons";
import type { GameInstance, ProjectCard, VersionFile } from "../types";
import {
  autoInstallModIntoInstance,
  checkInstanceModCompatibility,
  type InstallProfileModResult,
} from "../utils/profileModInstall";
import { cachedIconUrl, fallbackIconDataUri, formatLoaderName, formatNumber } from "../utils";

interface InstallToProfileModalProps {
  open: boolean;
  item: ProjectCard | null;
  instances: GameInstance[];
  onClose: () => void;
  onArmProfile?: (inst: GameInstance) => void;
  onNavigateToInstances?: () => void;
  onToast: (msg: string, type: "ok" | "err" | "info") => void;
  onInstalledSuccess?: (inst: GameInstance, result: InstallProfileModResult) => void;
}

export const InstallToProfileModal: React.FC<InstallToProfileModalProps> = ({
  open,
  item,
  instances,
  onClose,
  onArmProfile,
  onNavigateToInstances,
  onToast,
  onInstalledSuccess,
}) => {
  const { mounted, shown } = useOverlayTransition(open && Boolean(item), 230);
  const [versions, setVersions] = useState<VersionFile[]>([]);
  const [loadingVersions, setLoadingVersions] = useState(false);
  const [installedInstanceIds, setInstalledInstanceIds] = useState<Set<string>>(() => new Set());
  const [installingInstanceId, setInstallingInstanceId] = useState<string | null>(null);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  // Load versions & scan instances for already installed files
  useEffect(() => {
    if (!open || !item) {
      setVersions([]);
      setInstalledInstanceIds(new Set());
      setInstallingInstanceId(null);
      return;
    }

    let active = true;
    let resolvedVersions: VersionFile[] = [];
    const cached = api.getCachedModDetails(item);
    if (cached?.versions && cached.versions.length > 0) {
      resolvedVersions = cached.versions;
      setVersions(cached.versions);
    } else {
      setLoadingVersions(true);
      api
        .getModDetails(item)
        .then((det) => {
          if (!active) return;
          const vers = det.versions || [];
          setVersions(vers);
          void scanInstalledAcrossInstances(vers);
        })
        .catch((err) => {
          if (active) {
            onToast(`Не удалось получить список версий: ${(err as Error).message}`, "err");
          }
        })
        .finally(() => {
          if (active) setLoadingVersions(false);
        });
    }

    const scanInstalledAcrossInstances = async (vers: VersionFile[]) => {
      const ct = (item.content_type || "mod").toLowerCase();
      if (instances.length === 0) return;
      const knownFilenames = new Set(vers.map((v) => (v.filename || "").toLowerCase()).filter(Boolean));
      const slugLow = (item.slug || "").toLowerCase().trim();
      const titleSlug = item.title.toLowerCase().replace(/[^a-z0-9]/g, "");

      const matchesName = (rawName: string): boolean => {
        const clean = rawName.toLowerCase().replace(/\.disabled$/, "");
        if (knownFilenames.has(clean)) return true;
        if (slugLow.length >= 3 && (clean.startsWith(`${slugLow}-`) || clean.startsWith(`${slugLow}_`) || clean.startsWith(`${slugLow}+`) || clean === `${slugLow}.jar` || clean === `${slugLow}.zip`)) {
          return true;
        }
        const fnClean = clean.replace(/\.(jar|zip)$/, "").replace(/[^a-z0-9]/g, "");
        return Boolean(titleSlug.length >= 4 && (fnClean === titleSlug || fnClean.startsWith(titleSlug)));
      };

      const res = await Promise.all(
        instances.map(async (inst) => {
          try {
            if (ct === "resourcepack") {
              const list = await api.listInstanceResourcePacks(inst.id);
              return list.some((m) => matchesName(m.filename)) ? inst.id : null;
            }
            if (ct === "shader") {
              const list = await api.listInstanceShaderPacks(inst.id);
              return list.some((m) => matchesName(m.filename)) ? inst.id : null;
            }
            const list = await api.listInstanceMods(inst.id);
            return list.some((m) => matchesName(m.filename)) ? inst.id : null;
          } catch {
            return null;
          }
        })
      );
      if (!active) return;
      setInstalledInstanceIds(new Set(res.filter(Boolean) as string[]));
    };

    if (resolvedVersions.length > 0) {
      void scanInstalledAcrossInstances(resolvedVersions);
    }

    return () => {
      active = false;
    };
  }, [open, item, instances, onToast]);

  if (!mounted || !item) return null;

  const handleInstall = async (inst: GameInstance, bestVersion: VersionFile) => {
    setInstallingInstanceId(inst.id);
    try {
      const result = await autoInstallModIntoInstance({
        instance: inst,
        project: item,
        versions,
        onToast,
      });
      setInstalledInstanceIds((prev) => new Set(prev).add(inst.id));
      onToast(
        `✓ «${item.title}» (${bestVersion.version_number}) успешно установлен в профиль «${inst.name}»!`,
        "ok"
      );
      onInstalledSuccess?.(inst, result);
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      setInstallingInstanceId(null);
    }
  };

  const handleArmAndClose = (inst: GameInstance) => {
    onArmProfile?.(inst);
    onToast(`Профиль «${inst.name}» выбран целевым. Любой мод теперь устанавливается в него в один клик.`, "ok");
    onClose();
  };

  const ct = item.content_type || "mod";
  const rawIcon = item.icon_url || "";
  const fallback = fallbackIconDataUri(item.title);
  const primaryIcon = rawIcon ? cachedIconUrl(rawIcon, 96) : fallback;

  return (
    <div
      className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-labelledby="installToProfileTitle"
      aria-hidden={!shown}
      onClick={onClose}
    >
      <div
        className="modal-window modal-install-profile"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="modal-header">
          <div>
            <h2 className="modal-title" id="installToProfileTitle">
              Установить в профиль
            </h2>
            <p className="modal-subtitle">
              Выберите целевой профиль Minecraft для автоматической установки с зависимостями
            </p>
          </div>
          <button
            type="button"
            className="icon-close"
            aria-label="Закрыть"
            onClick={onClose}
          >
            <CloseIcon size={15} />
          </button>
        </div>

        <div className="modal-body">
          {/* Mod Summary Card */}
          <div className="install-profile-header-info">
            <img
              src={primaryIcon}
              alt=""
              className="install-profile-item-icon"
              onError={(e) => {
                e.currentTarget.src = fallback;
              }}
            />
            <div className="install-profile-item-details">
              <span className="install-profile-item-title">{item.title}</span>
              <div className="install-profile-item-meta">
                <span>от {(item.authors || []).join(", ") || "—"}</span>
                <span>•</span>
                <span>{formatNumber(item.downloads)} загрузок</span>
                <span>•</span>
                <span>
                  {ct === "mod" ? "Мод" : ct === "resourcepack" ? "Ресурспак" : ct === "shader" ? "Шейдер" : "Датапак"}
                </span>
              </div>
            </div>
          </div>

          {/* Instances List */}
          {loadingVersions ? (
            <div className="empty-state" style={{ padding: "32px 0" }}>
              <span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />
              Проверка совместимости версий и загрузчиков…
            </div>
          ) : instances.length === 0 ? (
            <div className="empty-state" style={{ padding: "32px 0" }}>
              <p style={{ marginBottom: 12 }}>У вас пока нет созданных профилей игры.</p>
              {onNavigateToInstances && (
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  onClick={() => {
                    onClose();
                    onNavigateToInstances();
                  }}
                >
                  Создать профиль в разделе «Профили»
                </button>
              )}
            </div>
          ) : (
            <div className="instances-compat-list">
              {instances.map((inst) => {
                const isInstalled = installedInstanceIds.has(inst.id);
                const isInstalling = installingInstanceId === inst.id;
                const compat = checkInstanceModCompatibility(inst, versions, ct);

                return (
                  <div
                    key={inst.id}
                    className={`instance-compat-card${isInstalled ? " is-installed" : ""}`}
                  >
                    <div className="instance-compat-card-left">
                      <div className="instance-compat-card-icon">
                        {inst.icon ? (
                          <img
                            src={inst.icon}
                            alt=""
                            style={{ width: 28, height: 28, borderRadius: 6, objectFit: "cover" }}
                            onError={(e) => {
                              e.currentTarget.style.display = "none";
                            }}
                          />
                        ) : (
                          <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
                            <polygon points="12 2 2 7 12 12 22 7 12 2" />
                            <polyline points="2 12 12 17 22 12" />
                            <polyline points="2 17 12 22 22 17" />
                          </svg>
                        )}
                      </div>
                      <div className="instance-compat-card-info">
                        <span className="instance-compat-card-name">{inst.name}</span>
                        <div className="instance-compat-card-tags">
                          <span className="pill pill-sm mono-num">MC {inst.mc_version}</span>
                          <span className="pill pill-sm">
                            <LoaderIcon loader={inst.instance_type} size={11} />
                            <span style={{ marginLeft: 4 }}>{formatLoaderName(inst.instance_type)}</span>
                          </span>

                          {isInstalled ? (
                            <span className="compat-badge is-installed">✓ Уже установлен</span>
                          ) : compat.compatible && compat.bestVersion ? (
                            <span className="compat-badge is-ok">
                              ✓ Совместим • v{compat.bestVersion.version_number}
                            </span>
                          ) : (
                            <span className="compat-badge is-incompat" title={compat.reason}>
                              ✕ {compat.reason}
                            </span>
                          )}
                        </div>
                      </div>
                    </div>

                    <div className="instance-compat-card-right">
                      {isInstalled ? (
                        <button type="button" className="btn btn-ghost btn-xs" disabled>
                          Установлен
                        </button>
                      ) : compat.compatible && compat.bestVersion ? (
                        <button
                          type="button"
                          className="btn btn-primary btn-xs"
                          disabled={installingInstanceId !== null}
                          onClick={() => handleInstall(inst, compat.bestVersion!)}
                        >
                          {isInstalling ? (
                            <>
                              <span className="spinner spinner-xs" aria-hidden="true" />
                              <span>Установка…</span>
                            </>
                          ) : (
                            "Установить"
                          )}
                        </button>
                      ) : (
                        <button type="button" className="btn btn-ghost btn-xs" disabled title={compat.reason}>
                          Недоступно
                        </button>
                      )}

                      {onArmProfile && (
                        <button
                          type="button"
                          className="btn btn-ghost btn-xs"
                          title="Сделать этот профиль активным для быстрого скачивания других модов"
                          onClick={() => handleArmAndClose(inst)}
                        >
                          Выбрать
                        </button>
                      )}
                    </div>
                  </div>
                );
              })}
            </div>
          )}
        </div>

        <div className="modal-footer" style={{ justifyContent: "flex-end", display: "flex", gap: 8 }}>
          <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>
            Закрыть
          </button>
        </div>
      </div>
    </div>
  );
};
