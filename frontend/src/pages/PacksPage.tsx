import React, { useMemo, useState } from "react";
import { api } from "../api";
import type { AppSection } from "../components/AppNavRail";
import { triggerBlobDownload } from "../hooks/usePackCart";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type { ExportTarget } from "../components/ExportModpackModal";
import type { CartItem, ModpackImportProgress, ModpackView, SavedPack } from "../types";
import { cachedIconUrl, detectCartConflicts, fallbackIconDataUri, formatBytes, formatLoaderName } from "../utils";

interface PacksPageProps {
  cart: CartItem[];
  savedPacks: SavedPack[];
  modpacks: ModpackView[];
  modpackProgress: ModpackImportProgress | null;
  mcVersion: string;
  loader: string;
  onRemoveAt: (idx: number) => void;
  onRemoveByKey: (folder: string, filename: string) => void;
  onClearCart: () => void;
  onLoadPack: (pack: SavedPack) => void;
  onCreateInstanceFromPack: (packId: string) => void;
  onOpenImportWizard: (opts?: { filePath?: string; packId?: string }) => void;
  onOpenExportModal: (target: ExportTarget) => void;
  onOpenUpdateModal: (mp: ModpackView) => void;
  onDeleteModpackRecord: (id: string) => Promise<void>;
  onPacksUpdated: (packs: SavedPack[]) => void;
  onNavigate: (section: AppSection) => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

const FOLDER_ORDER = ["mods", "resourcepacks", "shaderpacks", "datapacks"];
const FOLDER_LABELS: Record<string, string> = {
  mods: "Моды (mods/)",
  resourcepacks: "Ресурспаки (resourcepacks/)",
  shaderpacks: "Шейдеры (shaderpacks/)",
  datapacks: "Датапаки (datapacks/)",
};

export const PacksPage: React.FC<PacksPageProps> = ({
  cart, savedPacks, modpacks, modpackProgress, mcVersion, loader,
  onRemoveAt, onRemoveByKey, onClearCart, onLoadPack, onCreateInstanceFromPack,
  onOpenImportWizard, onOpenExportModal, onOpenUpdateModal, onDeleteModpackRecord,
  onPacksUpdated, onNavigate, onToast,
}) => {
  const [packName, setPackName] = useState("My-Modpack");
  const [saving, setSaving] = useState(false);
  const [exporting, setExporting] = useState(false);
  const [zipPct, setZipPct] = useState(0);

  const conflicts = useMemo(() => detectCartConflicts(cart), [cart]);
  const totalBytes = useMemo(() => cart.reduce((acc, c) => acc + Number(c.file_size || 0), 0), [cart]);

  const groupedFolders = useMemo(() => {
    const map = new Map<string, Array<{ item: CartItem; index: number }>>();
    cart.forEach((item, index) => {
      const f = item.folder || "mods";
      const arr = map.get(f);
      if (arr) arr.push({ item, index });
      else map.set(f, [{ item, index }]);
    });
    const keys = Array.from(map.keys()).sort((a, b) => {
      const ia = FOLDER_ORDER.indexOf(a);
      const ib = FOLDER_ORDER.indexOf(b);
      return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib);
    });
    return keys.map((folder) => ({
      folder,
      label: FOLDER_LABELS[folder] || folder,
      entries: map.get(folder)!,
    }));
  }, [cart]);

  const handleSavePack = async () => {
    if (!cart.length || saving) return;
    setSaving(true);
    try {
      const packs = await api.savePack({
        id: `pack_${Date.now()}`,
        name: packName.trim() || "My-Modpack",
        mc_version: mcVersion,
        loader,
        created_at: new Date().toISOString(),
        items: cart,
      });
      onPacksUpdated(packs);
      onToast("Сборка сохранена", "ok");
    } catch (err) {
      onToast(`Ошибка сохранения: ${(err as Error).message}`, "err");
    } finally {
      setSaving(false);
    }
  };

  const handleDeletePack = async (id: string) => {
    try {
      const packs = await api.deletePack(id);
      onPacksUpdated(packs);
      onToast("Сборка удалена", "info");
    } catch (err) {
      onToast(`Ошибка удаления: ${(err as Error).message}`, "err");
    }
  };

  const handleExportZip = async () => {
    if (!cart.length || exporting) return;
    setExporting(true);
    setZipPct(10);
    try {
      const name = packName.trim() || "My-Modpack";
      // In Wails Desktop mode, stream directly to disk via native Windows Save File dialog (zero RAM Blob overhead!)
      const nativeRes = await desktopBridge.savePackZipNative(name, cart);
      if (nativeRes) {
        if (nativeRes.cancelled) {
          return;
        }
        if (nativeRes.status === "partial" || nativeRes.failed > 0) {
          const detail = (nativeRes.failed_reasons || []).slice(0, 2).join("; ");
          onToast(
            `ZIP сохранён частично (${nativeRes.downloaded} из ${nativeRes.total}): ${nativeRes.path}${detail ? ` — ${detail}` : ""}`,
            "err"
          );
        } else {
          onToast(`ZIP-сборка сохранена (${nativeRes.downloaded} файлов): ${nativeRes.path}`, "ok");
        }
        return;
      }

      const rep = await api.downloadPackZipWithReport(name, cart, (pct) => setZipPct(pct));
      const safeZipName = `${name.replace(/[^a-zA-Z0-9_-]+/g, "-") || "My-Modpack"}.zip`;
      triggerBlobDownload(rep.blob, safeZipName);
      if (rep.status === "partial" || rep.failed > 0) {
        const detail = rep.failedReasons.slice(0, 2).join("; ");
        onToast(`ZIP скачан частично (${rep.downloaded}/${rep.total})${detail ? ` — ${detail}` : ""}`, "err");
      } else {
        onToast(`Готовая .ZIP сборка скачана (${rep.downloaded} файлов)`, "ok");
      }
    } catch (err) {
      onToast(`Ошибка экспорта ZIP: ${(err as Error).message}`, "err");
    } finally {
      setExporting(false);
      setZipPct(0);
    }
  };

  return (
    <div className="mh-page mh-page-enter">
      <header className="mh-page-head-row">
        <div>
          <h1 className="mh-page-title">Сборки и модпаки</h1>
          <p className="mh-page-subtitle">
            Конструктор сборок ModHub (<code>packs.json</code>), а также импорт, экспорт и обновление модпаков (<code>.mrpack</code>, CurseForge <code>.zip</code>, <code>.modhub</code>).
          </p>
        </div>
        <div style={{ display: "inline-flex", gap: 8, flexWrap: "wrap" }}>
          <button type="button" className="btn btn-primary btn-sm" onClick={() => onOpenImportWizard()}>Импортировать сборку</button>
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onNavigate("catalog")}>+ Добавить моды из каталога</button>
        </div>
      </header>

      {modpackProgress && modpackProgress.stage !== "completed" && modpackProgress.stage !== "partial" && modpackProgress.stage !== "error" && (
        <section className="mh-panel mh-install-live-panel" role="status">
          <div className="mh-panel-head">
            <strong>Импорт сборки «{modpackProgress.name}»: {modpackProgress.message}</strong>
            <button type="button" className="btn btn-ghost btn-xs" onClick={() => onNavigate("downloads")}>Очередь загрузок →</button>
          </div>
          <div className="mh-dl-progress-track">
            <div className={`mh-dl-progress-fill${modpackProgress.progress < 0 ? " is-indeterminate" : ""}`} style={{ width: modpackProgress.progress < 0 ? "35%" : `${Math.max(3, Math.min(100, modpackProgress.progress))}%` }} />
          </div>
        </section>
      )}

      <div className="mh-packs-layout">
        <section className="mh-panel mh-packs-active">
          <div className="mh-panel-head">
            <h2>Текущая сборка ({cart.length})</h2>
            <span className="mono-num mh-muted-meta">
              {cart.length > 0 ? formatBytes(totalBytes) : "Пусто"}
            </span>
          </div>

          {conflicts.length > 0 && (
            <div className="cart-conflict-banner mh-inline-banner" role="status">
              <div className="cart-conflict-lines">
                <strong>Возможный конфликт версий / загрузчиков</strong>
                <div>{conflicts.join(" • ")}</div>
              </div>
            </div>
          )}

          <div className="mh-pack-controls">
            <input
              type="text"
              className="text-input"
              placeholder="Название сборки (например: NeoForge 1.21.1)"
              value={packName}
              onChange={(e) => setPackName(e.target.value)}
            />
            <button
              type="button"
              className={`btn btn-ghost btn-sm${saving ? " is-loading" : ""}`}
              disabled={!cart.length || saving}
              onClick={handleSavePack}
            >
              {saving ? "Сохранение…" : "Сохранить"}
            </button>
            <button
              type="button"
              className={`btn btn-primary btn-sm${exporting ? " is-loading" : ""}`}
              disabled={!cart.length || exporting}
              onClick={handleExportZip}
            >
              {exporting ? (zipPct > 0 ? `Сборка ${zipPct}%…` : "Сборка .ZIP…") : "Экспорт .ZIP"}
            </button>
            <button
              type="button"
              className="btn btn-danger-ghost btn-sm"
              disabled={!cart.length}
              onClick={onClearCart}
            >
              Очистить
            </button>
          </div>

          {cart.length === 0 ? (
            <div className="empty-state" style={{ marginTop: 12 }}>
              <h3>Текущая сборка пуста</h3>
              <p>Выберите версию Minecraft и загрузчик в каталоге, затем добавьте нужные моды.</p>
              <button type="button" className="btn btn-primary btn-sm" style={{ marginTop: 12 }} onClick={() => onNavigate("catalog")}>
                Перейти в каталог
              </button>
            </div>
          ) : (
            <div className="mh-pack-items-scroll">
              {groupedFolders.map(({ folder, label, entries }) => (
                <div key={folder} className="cart-folder-group">
                  <div className="cart-folder-title">
                    <span className="cart-folder-title-main">
                      <span className={`cart-comp-dot seg-${folder}`} />
                      <span>{label}</span>
                    </span>
                    <span className="mono-num">{entries.length}</span>
                  </div>
                  {entries.map(({ item: c, index }) => {
                    const fb = fallbackIconDataUri(c.title);
                    const isAutoDep = Boolean(c.auto_dep_for && c.auto_dep_for.length > 0);
                    return (
                      <div key={`${folder}:${c.filename}:${index}`} className={`cart-item${isAutoDep ? " is-auto-dep" : ""}`}>
                        <img
                          width={36}
                          height={36}
                          className="cart-item-icon"
                          src={c.icon_url ? cachedIconUrl(c.icon_url, 72) : fb}
                          alt=""
                          onError={(e) => {
                            e.currentTarget.src = fb;
                          }}
                        />
                        <div className="cart-item-info">
                          <div className="cart-item-title-row">
                            <span className="cart-item-title" title={c.title}>{c.title}</span>
                            {isAutoDep && <span className="cart-dep-tag">Завис.</span>}
                          </div>
                          <div className="cart-item-sub" title={`${c.folder}/${c.filename}`}>
                            {c.filename} • {formatBytes(c.file_size)}
                            {c.mc_version ? ` • MC ${c.mc_version}` : ""}
                          </div>
                        </div>
                        <button
                          type="button"
                          className="icon-close cart-item-del"
                          aria-label={`Удалить ${c.title}`}
                          onClick={() => {
                            if (c.filename) onRemoveByKey(c.folder || "mods", c.filename);
                            else onRemoveAt(index);
                          }}
                        >
                          <CloseIcon size={13} />
                        </button>
                      </div>
                    );
                  })}
                </div>
              ))}
            </div>
          )}
        </section>

        <section className="mh-panel mh-packs-saved">
          <div className="mh-panel-head">
            <h2>Сохранённые сборки ({savedPacks.length})</h2>
          </div>
          {savedPacks.length === 0 ? (
            <div className="mh-panel-empty">
              Сохранённых сборок пока нет. Введите название слева и нажмите «Сохранить», чтобы быстро возвращаться к сборке позже.
            </div>
          ) : (
            <div className="mh-saved-list">
              {savedPacks.map((pack) => (
                <div key={pack.id} className="mh-saved-card">
                  <div className="mh-saved-info">
                    <span className="mh-saved-name">{pack.name}</span>
                    <span className="mh-saved-meta mono-num">
                      {pack.items.length} файлов
                      {pack.mc_version ? ` • MC ${pack.mc_version}` : ""}
                      {pack.loader ? ` • ${pack.loader}` : ""}
                    </span>
                  </div>
                  <div className="mh-saved-actions">
                    <button
                      type="button"
                      className="btn btn-primary btn-xs"
                      onClick={() => {
                        setPackName(pack.name);
                        onLoadPack(pack);
                      }}
                    >
                      Загрузить
                    </button>
                    <button type="button" className="btn btn-ghost btn-xs" onClick={() => onOpenImportWizard({ packId: pack.id })}>Установить в профиль</button>
                    <button type="button" className="btn btn-ghost btn-xs" onClick={() => onOpenExportModal({ sourceType: "pack", sourceId: pack.id, defaultName: pack.name })}>Экспорт</button>
                    <button type="button" className="btn btn-ghost btn-xs" onClick={() => onCreateInstanceFromPack(pack.id)}>Привязать</button>
                    <button
                      type="button"
                      className="btn btn-danger-ghost btn-xs"
                      onClick={() => handleDeletePack(pack.id)}
                    >
                      Удалить
                    </button>
                  </div>
                </div>
              ))}
            </div>
          )}
        </section>
      </div>

      <section className="mh-panel">
        <div className="mh-panel-head">
          <div>
            <h2>Импортированные модпаки ({modpacks.length})</h2>
            <p className="mh-muted-meta" style={{ margin: "2px 0 0" }}>Перетащите <code>.mrpack</code>, CurseForge <code>.zip</code> или <code>.modhub</code> в окно ModHub или нажмите «Импортировать сборку».</p>
          </div>
          <button type="button" className="btn btn-primary btn-xs" onClick={() => onOpenImportWizard()}>+ Импорт модпака</button>
        </div>
        {modpacks.length === 0 ? (
          <div className="mh-panel-empty">Импортированных модпаков пока нет.</div>
        ) : (
          <div className="mh-java-list">
            {modpacks.map((mp) => (
              <div key={mp.id} className="mh-java-card">
                <div className="mh-java-card-main">
                  <div className="mh-java-title-row">
                    <strong className="mh-java-name">{mp.name}</strong>
                    <span className="pill mono-num">{mp.format}</span>
                    {mp.version && <span className="pill mono-num">v{mp.version}</span>}
                    {mp.mc_version && <span className="pill mono-num">MC {mp.mc_version}</span>}
                    {mp.loader && <span className="pill">{formatLoaderName(mp.loader)} {mp.loader_version || ""}</span>}
                    <span className={`mh-dl-status-pill ${mp.status === "installed" ? "is-ok" : mp.status === "partial" ? "is-paused" : "is-err"}`}>{mp.status === "installed" ? "Установлен" : mp.status === "partial" ? "Частично" : mp.status}</span>
                  </div>
                  <div className="mh-java-compat mono-num"><span>Источник: {mp.source_type} • Файлов: {mp.files_count} (управляемых: {mp.managed_count})</span>{mp.author && <span> • Автор: {mp.author}</span>}</div>
                  {mp.last_error && <div className="mh-dl-error-note">{mp.last_error}</div>}
                </div>
                <div className="mh-java-actions">
                  {mp.instance_id && <button type="button" className="btn btn-primary btn-xs" onClick={() => onNavigate("instances")}>Профиль →</button>}
                  <button type="button" className="btn btn-ghost btn-xs" onClick={() => onOpenUpdateModal(mp)}>Обновить</button>
                  {mp.instance_id && <button type="button" className="btn btn-ghost btn-xs" onClick={() => onOpenExportModal({ sourceType: "instance", sourceId: mp.instance_id!, defaultName: mp.name, defaultDescription: mp.description })}>Экспорт</button>}
                  <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => { void onDeleteModpackRecord(mp.id).catch((err) => onToast(String((err as Error).message || err), "err")); }}>Удалить запись</button>
                </div>
              </div>
            ))}
          </div>
        )}
      </section>
    </div>
  );
};
