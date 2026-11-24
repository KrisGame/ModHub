import React, { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { isWailsDesktop, subscribeBackendEvents } from "../services/bridge";
import type {
  FullVersionVerificationReport,
  GameInstance,
  InstalledProfileVersion,
  InstallProfileVersionInput,
  InstallProgressInfo,
  ModloaderKind,
} from "../types";
import { formatDate, formatLoaderName } from "../utils";
import { InstallVersionWizard } from "./InstallVersionWizard";

interface InstanceVersionTabProps {
  instance: GameInstance;
  onRefreshInstances: () => Promise<void>;
  onOpenDownloads: () => void;
  onOpenJavaSettings: () => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

function stageLabel(stage: string): string {
  switch (stage) {
    case "metadata": return "Метаданные версии";
    case "java": return "Проверка Java";
    case "libraries": return "Библиотеки Minecraft";
    case "client": return "Клиент Minecraft";
    case "assets": return "Игровые ресурсы (Assets)";
    case "natives": return "Нативные библиотеки";
    case "modloader": return "Загрузчик модов";
    case "processors": return "Процессоры установщика";
    case "verifying": return "Проверка целостности";
    case "completed": return "Завершено";
    case "cancelled": return "Отменено";
    case "error": return "Ошибка установки";
    default: return stage;
  }
}

export const InstanceVersionTab: React.FC<InstanceVersionTabProps> = ({
  instance,
  onRefreshInstances,
  onOpenDownloads,
  onOpenJavaSettings,
  onToast,
}) => {
  const [installed, setInstalled] = useState<InstalledProfileVersion[]>([]);
  const [loadingList, setLoadingList] = useState(true);
  const [activeProg, setActiveProg] = useState<InstallProgressInfo | null>(null);
  const [lastError, setLastError] = useState("");
  const [wizardOpen, setWizardOpen] = useState(false);
  const [wizardInitMC, setWizardInitMC] = useState("");
  const [wizardInitLoader, setWizardInitLoader] = useState<ModloaderKind>("vanilla");
  const [busyId, setBusyId] = useState("");
  const [lastVerifyRep, setLastVerifyRep] = useState<FullVersionVerificationReport | null>(null);

  const loadData = useCallback(async () => {
    try {
      const [vers, prog] = await Promise.all([
        api.listProfileInstalledVersions(instance.id),
        api.getActiveProfileInstall(instance.id),
      ]);
      setInstalled(Array.isArray(vers) ? vers : []);
      setActiveProg(prog);
    } catch {
      // Ignore transient error
    } finally {
      setLoadingList(false);
    }
  }, [instance.id]);

  useEffect(() => {
    setLoadingList(true);
    setLastVerifyRep(null);
    setLastError("");
    void loadData();
  }, [loadData]);

  useEffect(() => {
    return subscribeBackendEvents((ev) => {
      if (ev.name === "install.progress" && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as InstallProgressInfo;
        if (p.instance_id === instance.id) {
          setActiveProg(p);
          setLastError("");
        }
      } else if (ev.name === "install.completed" && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as InstallProgressInfo;
        if (p.instance_id === instance.id) {
          setActiveProg(null);
          setLastError("");
          onToast(p.message || "Установка завершена", "ok");
          void loadData();
          void onRefreshInstances();
        }
      } else if ((ev.name === "install.error" || ev.name === "install.cancelled") && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as InstallProgressInfo;
        if (p.instance_id === instance.id) {
          setActiveProg(null);
          if (ev.name === "install.error") {
            setLastError(p.error || p.message || "Ошибка установки");
            onToast(`Ошибка установки: ${p.error || p.message}`, "err");
          } else {
            onToast("Установка отменена", "info");
          }
          void loadData();
          void onRefreshInstances();
        }
      } else if (ev.name === "versions.updated") {
        void loadData();
      }
    });
  }, [instance.id, loadData, onRefreshInstances, onToast]);

  useEffect(() => {
    if (isWailsDesktop() || !activeProg) return;
    const tid = window.setInterval(() => { void loadData(); }, 600);
    return () => window.clearInterval(tid);
  }, [activeProg, loadData]);

  const handleStartInstall = async (req: InstallProfileVersionInput) => {
    setLastError("");
    await api.installProfileVersion(req);
    setActiveProg({
      instance_id: instance.id, mc_version: req.mc_version, loader: req.loader, loader_version: req.loader_version,
      stage: "metadata", message: "Запуск установки...", completed_files: 0, total_files: 0, progress: -1, speed_bps: 0,
      updated_at: Math.floor(Date.now() / 1000),
    });
    onToast(`Запущена установка Minecraft ${req.mc_version}`, "info");
  };

  const handleCancel = async () => {
    await api.cancelProfileInstall(instance.id);
    onToast("Отмена установки...", "info");
  };

  const handleActivate = async (iv: InstalledProfileVersion) => {
    setBusyId(iv.id);
    try {
      await api.activateProfileVersion(instance.id, iv.id, true);
      onToast(`Активирована версия ${iv.version_id} (моды и сохранения не затронуты)`, "ok");
      await loadData();
      await onRefreshInstances();
    } catch (err) {
      onToast(`Ошибка активации: ${(err as Error).message}`, "err");
    } finally {
      setBusyId("");
    }
  };

  const handleVerify = async (iv: InstalledProfileVersion) => {
    setBusyId(iv.id);
    try {
      const rep = await api.verifyProfileVersion(instance.id, iv.id);
      setLastVerifyRep(rep);
      onToast(rep.valid ? `Версия ${iv.version_id} прошла проверку целостности` : `Обнаружены проблемы в ${iv.version_id}`, rep.valid ? "ok" : "err");
      await loadData();
      await onRefreshInstances();
    } catch (err) {
      onToast(`Ошибка проверки: ${(err as Error).message}`, "err");
    } finally {
      setBusyId("");
    }
  };

  const handleReinstall = async (iv: InstalledProfileVersion) => {
    await handleStartInstall({ instance_id: instance.id, mc_version: iv.mc_version, loader: iv.install_type, loader_version: iv.loader_version, create_backup: false });
  };

  const handleDelete = async (iv: InstalledProfileVersion) => {
    setBusyId(iv.id);
    try {
      await api.deleteProfileInstalledVersion(instance.id, iv.id);
      onToast(`Запись установки ${iv.version_id} удалена (общие библиотеки сохранены)`, "info");
      await loadData();
      await onRefreshInstances();
    } catch (err) {
      onToast(`Ошибка удаления: ${(err as Error).message}`, "err");
    } finally {
      setBusyId("");
    }
  };

  return (
    <div className="mh-inst-overview">
      {activeProg && (
        <section className="mh-panel mh-install-live-panel" role="status">
          <div className="mh-panel-head">
            <div>
              <h2>Установка: {stageLabel(activeProg.stage)}</h2>
              <p className="mh-muted-meta" style={{ margin: "2px 0 0" }}>
                Minecraft {activeProg.mc_version}
                {activeProg.loader && activeProg.loader !== "vanilla" ? ` • ${formatLoaderName(activeProg.loader)}` : " • Vanilla"}
                {activeProg.loader_version ? ` ${activeProg.loader_version}` : ""}
              </p>
            </div>
            <div style={{ display: "inline-flex", gap: 6 }}>
              <button type="button" className="btn btn-ghost btn-xs" onClick={onOpenDownloads}>Очередь загрузок →</button>
              <button type="button" className="btn btn-danger-ghost btn-xs" onClick={handleCancel}>Отменить</button>
            </div>
          </div>
          <div className="mh-dl-meta mono-num">
            <span>{activeProg.message}</span>
            {activeProg.component && <span>• {activeProg.component}</span>}
            {activeProg.total_files > 0 && <span>• Файлов: {activeProg.completed_files} / {activeProg.total_files}</span>}
            {activeProg.progress >= 0 && <span>• {Math.round(activeProg.progress)}%</span>}
            {activeProg.speed_bps > 0 && (
              <span>• {activeProg.speed_bps >= 1_048_576 ? `${(activeProg.speed_bps / 1_048_576).toFixed(1)} МБ/с` : `${Math.round(activeProg.speed_bps / 1_024)} КБ/с`}</span>
            )}
          </div>
          <div className="mh-dl-progress-track">
            <div className={`mh-dl-progress-fill${activeProg.progress < 0 ? " is-indeterminate" : ""}`} style={{ width: activeProg.progress < 0 ? "35%" : `${Math.max(2, Math.min(100, activeProg.progress))}%` }} />
          </div>
        </section>
      )}

      {lastError && <div className="mh-dl-error-note" role="alert"><strong>Установка прервана:</strong> {lastError}</div>}

      <section className="mh-panel">
        <div className="mh-panel-head">
          <div>
            <h2>Установленные версии в профиле ({installed.length})</h2>
            <p className="mh-muted-meta" style={{ margin: "2px 0 0" }}>Храните несколько установок (Vanilla, Fabric, Forge, NeoForge, Quilt) и переключайтесь без потери модов и сохранений.</p>
          </div>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={Boolean(activeProg)}
            onClick={() => {
              setWizardInitMC(instance.mc_version || "");
              setWizardInitLoader(instance.instance_type && instance.instance_type !== "empty" ? (instance.instance_type as ModloaderKind) : "vanilla");
              setWizardOpen(true);
            }}
          >
            + Установить версию / загрузчик
          </button>
        </div>

        {loadingList ? (
          <div className="mh-panel-empty">Загрузка установленных версий…</div>
        ) : installed.length === 0 ? (
          <div className="mh-panel-empty">
            В этом профиле ещё нет установленных версий Minecraft. Нажмите <strong>«+ Установить версию / загрузчик»</strong>, чтобы установить Vanilla или Fabric, Forge, NeoForge, Quilt.
          </div>
        ) : (
          <div className="mh-java-list">
            {installed.map((iv) => (
              <div key={iv.id} className={`mh-java-card${iv.active ? " is-active-ver" : ""}${!iv.verified ? " is-broken" : ""}`}>
                <div className="mh-java-card-main">
                  <div className="mh-java-title-row">
                    <strong className="mh-java-name mono-num">{iv.version_id}</strong>
                    {iv.active && <span className="pill pill-installed">Активна</span>}
                    <span className="pill">MC {iv.mc_version}</span>
                    <span className="pill">{iv.install_type === "vanilla" ? "Vanilla" : `${formatLoaderName(iv.install_type)}${iv.loader_version ? ` ${iv.loader_version}` : ""}`}</span>
                    <span className={`pill ${iv.verified ? "pill-installed" : "pill-alpha"}`}>{iv.verified ? "Проверена ✓" : "Требует проверки"}</span>
                  </div>
                  <div className="mh-java-compat mono-num">
                    <span>Java {iv.java_major || "?"}</span>
                    {iv.installed_at > 0 && <span> • Установлена: {formatDate(new Date(iv.installed_at * 1000).toISOString())}</span>}
                  </div>
                  <div className="mh-java-path mono-num" title={iv.metadata_path}>{iv.metadata_path}</div>
                  {iv.verify_error && <div className="mh-dl-error-note">{iv.verify_error}</div>}
                </div>
                <div className="mh-java-actions">
                  {!iv.active && <button type="button" className="btn btn-primary btn-xs" disabled={busyId === iv.id || Boolean(activeProg)} onClick={() => handleActivate(iv)}>Выбрать</button>}
                  <button type="button" className={`btn btn-ghost btn-xs${busyId === iv.id ? " is-loading" : ""}`} disabled={busyId === iv.id || Boolean(activeProg)} onClick={() => handleVerify(iv)}>Проверить</button>
                  <button type="button" className="btn btn-ghost btn-xs" disabled={busyId === iv.id || Boolean(activeProg)} onClick={() => handleReinstall(iv)}>Переустановить</button>
                  <button type="button" className="btn btn-danger-ghost btn-xs" disabled={busyId === iv.id || Boolean(activeProg)} onClick={() => handleDelete(iv)}>Удалить</button>
                </div>
              </div>
            ))}
          </div>
        )}

        {lastVerifyRep && (
          <div className={`mh-inst-issue ${lastVerifyRep.valid ? "" : "is-err"}`} style={{ marginTop: 4 }}>
            <strong>Отчёт проверки ({lastVerifyRep.version_id}):</strong>{" "}
            <span>
              Client JAR={lastVerifyRep.vanilla.client_jar_ok ? "OK" : "НЕТ"} • Библиотеки MC={lastVerifyRep.vanilla.libraries_ok}/{lastVerifyRep.vanilla.libraries_total} • Ресурсы={lastVerifyRep.vanilla.assets_ok}/{lastVerifyRep.vanilla.assets_total} • Java={lastVerifyRep.vanilla.java_summary}
              {lastVerifyRep.loader_check ? ` • Библиотеки загрузчика=${lastVerifyRep.loader_check.libraries_ok}/${lastVerifyRep.loader_check.libraries_total}` : ""}
            </span>
          </div>
        )}
      </section>

      <InstallVersionWizard open={wizardOpen} instance={instance} initialMCVersion={wizardInitMC} initialLoader={wizardInitLoader} onClose={() => setWizardOpen(false)} onStartInstall={handleStartInstall} onOpenJavaSettings={onOpenJavaSettings} onToast={onToast} />
    </div>
  );
};
