import React, { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { desktopBridge, subscribeBackendEvents } from "../services/bridge";
import type { AvailableJavaRuntime, JavaRuntimeInfo, JavaStateSnapshot } from "../types";

interface JavaSettingsSectionProps {
  onOpenDownloads: () => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

function sourceBadgeLabel(rt: JavaRuntimeInfo): string {
  if (rt.managed) return "Установлена ModHub";
  switch (rt.source) {
    case "path":
      return "PATH / JAVA_HOME";
    case "custom":
      return "Указана вручную";
    default:
      return "Системная Java";
  }
}

export const JavaSettingsSection: React.FC<JavaSettingsSectionProps> = ({
  onOpenDownloads,
  onToast,
}) => {
  const [snap, setSnap] = useState<JavaStateSnapshot | null>(null);
  const [loading, setLoading] = useState(true);
  const [scanning, setScanning] = useState(false);
  const [verifyingId, setVerifyingId] = useState("");
  const [removingId, setRemovingId] = useState("");
  const [customPath, setCustomPath] = useState("");
  const [addingCustom, setAddingCustom] = useState(false);

  const loadSnapshot = useCallback(async (refreshRemote: boolean) => {
    try {
      const data = await api.getJavaRuntimes(refreshRemote);
      setSnap(data);
    } catch (err) {
      onToast(`Ошибка загрузки списка Java: ${(err as Error).message}`, "err");
    } finally {
      setLoading(false);
    }
  }, [onToast]);

  useEffect(() => {
    void loadSnapshot(true);
  }, [loadSnapshot]);

  useEffect(() => {
    return subscribeBackendEvents((ev) => {
      if (ev.name === "java.updated" && ev.payload && typeof ev.payload === "object") {
        setSnap(ev.payload as JavaStateSnapshot);
      }
    });
  }, []);

  const handleScan = async () => {
    setScanning(true);
    try {
      const data = await api.scanJavaRuntimes();
      setSnap(data);
      onToast(`Найдено установок Java: ${(data.runtimes || []).length}`, "ok");
    } catch (err) {
      onToast(`Ошибка поиска Java: ${(err as Error).message}`, "err");
    } finally {
      setScanning(false);
    }
  };

  const handleInstall = async (av: AvailableJavaRuntime) => {
    try {
      await api.installJavaRuntime(av.component);
      onToast(`Запущена установка ${av.display_name} через менеджер загрузок`, "info");
      void loadSnapshot(false);
    } catch (err) {
      onToast(`Не удалось запустить установку: ${(err as Error).message}`, "err");
    }
  };

  const handleCancelInstall = async (component: string) => {
    try {
      await api.cancelJavaInstall(component);
      onToast("Установка Java отменена", "info");
      void loadSnapshot(false);
    } catch (err) {
      onToast(`Ошибка отмены: ${(err as Error).message}`, "err");
    }
  };

  const handleVerify = async (rt: JavaRuntimeInfo) => {
    setVerifyingId(rt.id);
    try {
      const res = await api.verifyJavaRuntime(rt.id);
      if (res.verified && res.runnable) {
        onToast(`Java ${res.version} успешно проверена`, "ok");
      } else {
        onToast(`Проверка Java завершилась с ошибкой: ${res.error || "не запускается"}`, "err");
      }
      void loadSnapshot(false);
    } catch (err) {
      onToast(`Ошибка проверки Java: ${(err as Error).message}`, "err");
    } finally {
      setVerifyingId("");
    }
  };

  const handleReinstall = async (rt: JavaRuntimeInfo) => {
    try {
      await api.reinstallJavaRuntime(rt.id);
      onToast(`Запущена переустановка ${rt.name}`, "info");
      void loadSnapshot(false);
    } catch (err) {
      onToast(`Ошибка переустановки: ${(err as Error).message}`, "err");
    }
  };

  const handleRemove = async (rt: JavaRuntimeInfo) => {
    setRemovingId(rt.id);
    try {
      await api.removeJavaRuntime(rt.id);
      onToast(`Удалена ${rt.name}`, "info");
      void loadSnapshot(false);
    } catch (err) {
      onToast(`Не удалось удалить: ${(err as Error).message}`, "err");
    } finally {
      setRemovingId("");
    }
  };

  const handleAddCustom = async () => {
    const val = customPath.trim();
    if (!val) return;
    setAddingCustom(true);
    try {
      const rt = await api.addCustomJavaPath(val);
      setCustomPath("");
      onToast(`Добавлена Java ${rt.version} (${rt.arch})`, "ok");
      void loadSnapshot(false);
    } catch (err) {
      onToast(`Ошибка добавления Java: ${(err as Error).message}`, "err");
    } finally {
      setAddingCustom(false);
    }
  };

  const handleBrowseCustom = async () => {
    try {
      const rt = await desktopBridge.browseCustomJavaPath();
      if (!rt) return;
      onToast(`Добавлена Java ${rt.version} (${rt.arch})`, "ok");
      void loadSnapshot(false);
    } catch (err) {
      onToast(`Ошибка выбора файла Java: ${(err as Error).message}`, "err");
    }
  };

  const runtimes = snap?.runtimes || [];
  const available = snap?.available || [];
  const installingComps = snap?.installing_components || [];

  return (
    <section className="mh-panel mh-java-section" aria-label="Управление Java Runtime">
      <div className="net-section-head">
        <h2>Java Runtime</h2>
        <div style={{ display: "inline-flex", gap: 6, flexWrap: "wrap" }}>
          {desktopBridge.isDesktop() && (
            <button
              type="button"
              className="btn btn-ghost btn-xs"
              onClick={() => {
                desktopBridge.openJavaRuntimeDir("").catch((err) => onToast(String((err as Error).message || err), "err"));
              }}
            >
              Папка runtimes
            </button>
          )}
          <button
            type="button"
            className={`btn btn-primary btn-xs${scanning ? " is-loading" : ""}`}
            disabled={scanning}
            onClick={handleScan}
          >
            {scanning ? "Поиск Java…" : "Найти установленные версии"}
          </button>
        </div>
      </div>

      <p className="help-text">
        Поиск установленной Java на компьютере и установка официальных версий от Mojang.
      </p>

      {installingComps.length > 0 && (
        <div className="mh-java-installing-banner" role="status">
          <span>
            Идёт установка Java ({installingComps.join(", ")})…
          </span>
          <button type="button" className="btn btn-primary btn-xs" onClick={onOpenDownloads}>
            Открыть «Загрузки» →
          </button>
        </div>
      )}

      {/* Installed & Detected Java Runtimes */}
      <div className="mh-java-subhead">
        <span>Найденные и установленные версии ({runtimes.length})</span>
      </div>

      {loading ? (
        <div className="mh-panel-empty">Загрузка информации о Java…</div>
      ) : runtimes.length === 0 ? (
        <div className="mh-panel-empty">
          Установленные версии Java пока не обнаружены. Нажмите «Найти установленные версии» или установите официальную версию ниже.
        </div>
      ) : (
        <div className="mh-java-list">
          {runtimes.map((rt) => (
            <div key={rt.id} className={`mh-java-card${rt.verified && rt.runnable ? "" : " is-broken"}`}>
              <div className="mh-java-card-main">
                <div className="mh-java-title-row">
                  <strong className="mh-java-name">{rt.name}</strong>
                  <span className="pill mono-num">Java {rt.major_version || "?"}</span>
                  {rt.version && <span className="pill mono-num">{rt.version}</span>}
                  <span className="pill mono-num">{rt.arch || "?"} ({rt.bitness || 64}-bit)</span>
                  <span className={`pill ${rt.managed ? "pill-installed" : ""}`}>{sourceBadgeLabel(rt)}</span>
                  {!rt.verified && <span className="pill pill-alpha">Требует проверки</span>}
                </div>
                <div className="mh-java-path mono-num" title={rt.executable_path}>
                  {rt.executable_path}
                </div>
                <div className="mh-java-compat">
                  <span>Совместимость: <strong>{rt.compatible_mc || "Не определено"}</strong></span>
                  {rt.vendor && <span> • Производитель: {rt.vendor}</span>}
                </div>
                {rt.error && <div className="mh-dl-error-note">{rt.error}</div>}
              </div>

              <div className="mh-java-actions">
                <button
                  type="button"
                  className={`btn btn-ghost btn-xs${verifyingId === rt.id ? " is-loading" : ""}`}
                  disabled={verifyingId === rt.id}
                  onClick={() => handleVerify(rt)}
                >
                  {verifyingId === rt.id ? "Проверка…" : "Проверить"}
                </button>
                {desktopBridge.isDesktop() && (
                  <button
                    type="button"
                    className="btn btn-ghost btn-xs"
                    onClick={() => {
                      desktopBridge.openJavaRuntimeDir(rt.id).catch((err) => onToast(String((err as Error).message || err), "err"));
                    }}
                  >
                    Папка
                  </button>
                )}
                {rt.managed && rt.component && (
                  <button
                    type="button"
                    className="btn btn-ghost btn-xs"
                    disabled={rt.in_use}
                    onClick={() => handleReinstall(rt)}
                  >
                    Переустановить
                  </button>
                )}
                {rt.managed && (
                  <button
                    type="button"
                    className={`btn btn-danger-ghost btn-xs${removingId === rt.id ? " is-loading" : ""}`}
                    disabled={rt.in_use || removingId === rt.id}
                    title={rt.in_use ? "Runtime используется другой операцией" : "Удалить Java, установленную ModHub"}
                    onClick={() => handleRemove(rt)}
                  >
                    Удалить
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Official Mojang Runtimes Available to Install */}
      <div className="mh-java-subhead" style={{ marginTop: 6 }}>
        <span>Доступные версии Java от Mojang</span>
      </div>

      {snap?.metadata_error && (
        <div className="mh-dl-error-note">
          Не удалось обновить каталог Java: {snap.metadata_error}
        </div>
      )}

      {available.length > 0 && (
        <div className="mh-java-avail-grid">
          {available.map((av) => (
            <div key={av.component} className="mh-java-avail-card">
              <div className="mh-java-avail-top">
                <strong>Java {av.major_version}</strong>
                <span className="pill mono-num">{av.version}</span>
              </div>
              <div className="mh-java-avail-sub">{av.compatible_mc}</div>
              <div className="mh-java-avail-foot">
                {av.installing ? (
                  <>
                    <button type="button" className="btn btn-ghost btn-xs" onClick={onOpenDownloads}>
                      Прогресс →
                    </button>
                    <button
                      type="button"
                      className="btn btn-danger-ghost btn-xs"
                      onClick={() => handleCancelInstall(av.component)}
                    >
                      Отмена
                    </button>
                  </>
                ) : av.installed ? (
                  <span className="pill pill-installed">Установлена ✓</span>
                ) : (
                  <button
                    type="button"
                    className="btn btn-primary btn-xs"
                    onClick={() => handleInstall(av)}
                  >
                    Установить
                  </button>
                )}
              </div>
            </div>
          ))}
        </div>
      )}

      {/* Manual Custom Java Path */}
      <div className="mh-java-subhead" style={{ marginTop: 6 }}>
        <span>Добавить установленную Java вручную</span>
      </div>
      <div className="input-action-row">
        <input
          type="text"
          className="text-input mono-num"
          placeholder="Путь к java.exe или корневой папке JDK..."
          value={customPath}
          onChange={(e) => setCustomPath(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter") void handleAddCustom();
          }}
        />
        {desktopBridge.isDesktop() && (
          <button type="button" className="btn btn-ghost btn-sm" onClick={handleBrowseCustom}>
            Обзор…
          </button>
        )}
        <button
          type="button"
          className={`btn btn-ghost btn-sm${addingCustom ? " is-loading" : ""}`}
          disabled={addingCustom || !customPath.trim()}
          onClick={handleAddCustom}
        >
          Добавить
        </button>
      </div>
    </section>
  );
};
