import React, { useEffect, useState } from "react";
import { api } from "../api";
import { desktopBridge, subscribeBackendEvents } from "../services/bridge";
import type { DesktopAppInfo } from "../services/wails";
import type { AccentPreset, BgPreset, NetworkCheckResponse, PublicConfig, ThemeMode } from "../types";
import type { DesktopIndexStatus } from "../services/wails";
import { ModpackUpdatesPanel } from "../components/ModpackUpdatesPanel";

interface SettingsPageProps {
  appInfo: DesktopAppInfo | null;
  config: PublicConfig;
  theme: ThemeMode;
  accent: AccentPreset;
  bgPreset: BgPreset;
  onToggleTheme: (e: React.MouseEvent<HTMLButtonElement>) => void;
  onAccentChange: (a: AccentPreset) => void;
  onBgPresetChange: (b: BgPreset) => void;
  onConfigSaved: (cfg: PublicConfig) => void;
  onOpenDownloads?: () => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

const ACCENTS: { id: AccentPreset; label: string }[] = [
  { id: "mono", label: "Монохром" },
  { id: "graphite", label: "Графит" },
  { id: "emerald", label: "Изумруд" },
  { id: "amber", label: "Янтарь" },
  { id: "indigo", label: "Индиго" },
];

const BGS: { id: BgPreset; label: string }[] = [
  { id: "dots", label: "Точки" },
  { id: "grid", label: "Сетка" },
  { id: "noise", label: "Шум" },
  { id: "clean", label: "Чистый" },
];

export const SettingsPage: React.FC<SettingsPageProps> = ({
  appInfo,
  config,
  theme,
  accent,
  bgPreset,
  onToggleTheme,
  onAccentChange,
  onBgPresetChange,
  onConfigSaved,
  onToast,
}) => {
  const [cfKey, setCfKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [statusMsg, setStatusMsg] = useState<{ text: string; kind: "" | "ok" | "err" }>({ text: "", kind: "" });
  const [busy, setBusy] = useState(false);
  const [proxyInput, setProxyInput] = useState(config.proxy_url || "");
  const [proxyBusy, setProxyBusy] = useState(false);
  const [netBusy, setNetBusy] = useState(false);
  const [cacheBusy, setCacheBusy] = useState(false);
  const [medDepBusy, setMedDepBusy] = useState(false);
  const [netResult, setNetResult] = useState<NetworkCheckResponse | null>(null);
  const [netError, setNetError] = useState("");
  const [indexStatus, setIndexStatus] = useState<DesktopIndexStatus | null>(null);
  const [indexBusy, setIndexBusy] = useState(false);
  const [actualDataDir, setActualDataDir] = useState("");

  useEffect(() => {
    let active = true;
    void desktopBridge.getAppInfo().then((info) => {
      if (active) setActualDataDir(info?.data_dir?.dir || "");
    }).catch(() => {});
    return () => { active = false; };
  }, []);

  useEffect(() => {
    let active = true;
    const load = () => api.getIndexStatus().then((status) => { if (active) setIndexStatus(status); }).catch(() => {});
    void load();
    const timer = window.setInterval(() => { void load(); }, 2000);
    const unsubscribe = subscribeBackendEvents((ev) => {
      if (ev.name.startsWith("index.sync.")) void load();
    });
    return () => { active = false; window.clearInterval(timer); unsubscribe(); };
  }, []);

  const syncIndex = async () => {
    setIndexBusy(true);
    try {
      const result = await api.triggerIndexSync(false);
      setIndexStatus(result.status);
      onToast(result.started ? "Синхронизация каталога запущена" : "Синхронизация уже выполняется", "info");
    } catch (err) { onToast(`Не удалось запустить синхронизацию: ${(err as Error).message}`, "err"); }
    finally { setIndexBusy(false); }
  };

  useEffect(() => {
    setProxyInput(config.proxy_url || "");
    setStatusMsg({
      text: config.curseforge_configured
        ? `Текущий ключ подключён: ${config.curseforge_key_masked}`
        : "Ключ не задан — поиск идёт только по Modrinth.",
      kind: config.curseforge_configured ? "ok" : "",
    });
  }, [config]);

  const runNetworkCheck = async () => {
    setNetBusy(true);
    setNetError("");
    try {
      const res = await api.checkNetwork();
      setNetResult(res);
    } catch (err) {
      setNetError(String((err as Error).message || err));
    } finally {
      setNetBusy(false);
    }
  };

  const handleSaveProxy = async (nextVal?: string) => {
    const val = (nextVal !== undefined ? nextVal : proxyInput).trim();
    setProxyBusy(true);
    try {
      const nextCfg = await api.saveConfig({ proxy_url: val });
      onConfigSaved(nextCfg);
      setProxyInput(nextCfg.proxy_url || "");
      onToast(val ? `Прокси сохранён: ${val}` : "Включён автоподхват прокси", "ok");
      await runNetworkCheck();
    } catch (err) {
      onToast(`Ошибка сохранения прокси: ${(err as Error).message}`, "err");
    } finally {
      setProxyBusy(false);
    }
  };

  const handleTestAndSaveKey = async () => {
    const val = cfKey.trim();
    if (!val) {
      setStatusMsg({ text: "Вставьте Core API Key", kind: "err" });
      return;
    }
    setBusy(true);
    setStatusMsg({ text: "Проверка ключа CurseForge...", kind: "" });
    try {
      const testRes = await api.testCurseForgeKey(val);
      if (!testRes.ok) {
        setStatusMsg({ text: testRes.error || "Ключ не подошёл", kind: "err" });
        return;
      }
      const nextCfg = await api.saveConfig({ curseforge_api_key: val });
      onConfigSaved(nextCfg);
      setCfKey("");
      setStatusMsg({ text: "Ключ проверен и сохранён!", kind: "ok" });
      onToast("CurseForge API-ключ сохранён", "ok");
    } catch (err) {
      setStatusMsg({ text: String((err as Error).message || err), kind: "err" });
    } finally {
      setBusy(false);
    }
  };

  const handleSaveRawKey = async () => {
    const val = cfKey.trim();
    if (!val) return;
    setBusy(true);
    try {
      const nextCfg = await api.saveConfig({ curseforge_api_key: val });
      onConfigSaved(nextCfg);
      setCfKey("");
      setStatusMsg({ text: "Ключ сохранён без проверки", kind: "ok" });
      onToast("Ключ сохранён", "ok");
    } catch (err) {
      setStatusMsg({ text: String((err as Error).message || err), kind: "err" });
    } finally {
      setBusy(false);
    }
  };

  const handleClearKey = async () => {
    setBusy(true);
    try {
      const nextCfg = await api.saveConfig({ curseforge_api_key: "" });
      onConfigSaved(nextCfg);
      setCfKey("");
      setStatusMsg({ text: "Ключ удалён", kind: "" });
      onToast("Ключ CurseForge удалён", "info");
    } catch (err) {
      setStatusMsg({ text: String(err), kind: "err" });
    } finally {
      setBusy(false);
    }
  };

  const detectedProxy = netResult?.proxy || config.proxy_detected;

  return (
    <div className="mh-page mh-page-enter">
      <header className="mh-page-head-row">
        <div>
          <h1 className="mh-page-title">Настройки</h1>
          <p className="mh-page-subtitle">Управление источниками модов, сетью, кэшем и оформлением.</p>
        </div>
      </header>

      <div className="mh-settings-stack">
        {/* Data directory */}
        <section className="mh-panel">
          <div className="mh-panel-head">
            <h2>Папка данных лаунчера</h2>
            {appInfo && (
              <button
                type="button"
                className="btn btn-ghost btn-xs"
                onClick={() => {
                  desktopBridge.openDataDir().catch((err) => onToast(String((err as Error).message || err), "err"));
                }}
              >
                Открыть папку
              </button>
            )}
          </div>
          <p className="help-text">
            Здесь сохраняются ваши профили, настройки и загруженные ресурсы.
          </p>
          <div className="mh-data-dir-box mono-num">
            <div>{actualDataDir || appInfo?.data_dir?.dir || "Путь недоступен в веб-режиме"}</div>
          </div>
        </section>
        <ModpackUpdatesPanel onToast={onToast} />

        <section className="mh-panel">
          <div className="mh-panel-head"><h2>Каталог и индекс</h2>
            <button type="button" className={`btn btn-secondary btn-xs${indexBusy ? " is-loading" : ""}`} disabled={indexBusy || !!indexStatus?.running} onClick={() => void syncIndex()}>
              {indexStatus?.running ? "Синхронизация…" : indexBusy ? "Запуск…" : "Синхронизировать"}
            </button>
          </div>
          <p className="help-text">{indexStatus ? `Состояние: ${indexStatus.phase || (indexStatus.running ? "выполняется" : "готово")}${indexStatus.total_steps > 0 ? ` • ${indexStatus.progress}/${indexStatus.total_steps} шагов` : ""} • Версия индекса ${indexStatus.index_version}` : "Состояние индекса недоступно."}</p>
          {indexStatus?.last_completed ? <p className="help-text">Последнее завершение: {new Date(indexStatus.last_completed * 1000).toLocaleString()}</p> : null}
          {indexStatus?.provider_outcomes && <p className="help-text">Источники: Modrinth — {indexStatus.provider_outcomes.modrinth || "нет данных"}; CurseForge — {indexStatus.provider_outcomes.curseforge === "not_configured" ? "ключ не задан" : indexStatus.provider_outcomes.curseforge || "нет данных"}</p>}
          {indexStatus?.last_error && <p className="inline-status is-err" role="alert">{indexStatus.last_error}</p>}
        </section>

        {/* CurseForge API */}
        <section className="mh-panel">
          <div className="mh-panel-head">
            <h2>CurseForge Core API v1</h2>
          </div>
          <p className="help-text">
            Откройте{" "}
            <a href="https://console.curseforge.com/" target="_blank" rel="noopener noreferrer">console.curseforge.com</a>,
            перейдите в <strong>API Keys</strong>, скопируйте <strong>Core API Key</strong> и сохраните его здесь.
          </p>
          <div className="input-action-row">
            <div className="key-input-wrap">
              <input
                type={showKey ? "text" : "password"}
                className="text-input"
                placeholder="Вставьте ключ доступа CurseForge"
                autoComplete="off"
                spellCheck={false}
                value={cfKey}
                onChange={(e) => setCfKey(e.target.value)}
              />
              <button type="button" className="key-vis-btn" onClick={() => setShowKey((s) => !s)}>
                {showKey ? "Скрыть" : "Показать"}
              </button>
            </div>
            <button type="button" className={`btn btn-primary btn-sm${busy ? " is-loading" : ""}`} disabled={busy} onClick={handleTestAndSaveKey}>
              {busy ? "Проверка…" : "Проверить и сохранить"}
            </button>
          </div>
          <div className="settings-sub-actions">
            <button type="button" className="btn btn-ghost btn-xs" disabled={busy || !cfKey.trim()} onClick={handleSaveRawKey}>
              Сохранить без проверки
            </button>
            <button type="button" className="btn btn-danger-ghost btn-xs" disabled={busy || !config.curseforge_configured} onClick={handleClearKey}>
              Удалить ключ
            </button>
          </div>
          {config.curseforge_key_unreadable && <div className="inline-status is-err" role="alert">Сохранённый ключ не удалось расшифровать. Он не используется; введите новый ключ или удалите старый. Другие настройки сохраняются без потери зашифрованного значения.</div>}
          {statusMsg.text && (
            <div className={`inline-status${statusMsg.kind ? ` is-${statusMsg.kind}` : ""}`}>{statusMsg.text}</div>
          )}
        </section>

        {/* Network & Proxy & Cache */}
        <section className="mh-panel">
          <div className="net-section-head">
            <h2>Сеть, прокси и кэш</h2>
            <div style={{ display: "inline-flex", gap: 6, flexWrap: "wrap" }}>
              <button
                type="button"
                className={`btn btn-ghost btn-xs${cacheBusy ? " is-loading" : ""}`}
                disabled={cacheBusy}
                onClick={async () => {
                  setCacheBusy(true);
                  try {
                    await api.clearAllCaches();
                    onToast("Кэш ModHub очищен", "ok");
                  } catch (err) {
                    onToast(`Ошибка очистки кэша: ${(err as Error).message}`, "err");
                  } finally {
                    setCacheBusy(false);
                  }
                }}
              >
                {cacheBusy ? "Очистка…" : "Очистить кэш"}
              </button>
              <button type="button" className={`btn btn-primary btn-xs${netBusy ? " is-loading" : ""}`} disabled={netBusy} onClick={runNetworkCheck}>
                {netBusy ? "Проверка…" : "Проверить API"}
              </button>
            </div>
          </div>
          <p className="help-text">
            Поддерживаются системный прокси Windows, переменные среды и ручной адрес (<code>http://...</code>, <code>socks5://...</code>, <code>direct</code>).
          </p>
          <div className="input-action-row">
            <input
              type="text"
              className="text-input mono-num"
              placeholder="Авто (или http://127.0.0.1:7890 / socks5://... / direct)"
              autoComplete="off"
              spellCheck={false}
              value={proxyInput}
              onChange={(e) => setProxyInput(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") handleSaveProxy();
              }}
            />
            <button type="button" className={`btn btn-ghost btn-sm${proxyBusy ? " is-loading" : ""}`} disabled={proxyBusy} onClick={() => handleSaveProxy()}>
              {proxyBusy ? "Сохранение…" : "Сохранить"}
            </button>
          </div>
          {proxyInput && (
            <div className="settings-sub-actions">
              <button type="button" className="btn btn-ghost btn-xs" disabled={proxyBusy} onClick={() => handleSaveProxy("")}>
                Сбросить на авто
              </button>
            </div>
          )}
          {detectedProxy && (
            <div className="net-proxy-line mono-num">
              <span className={`status-dot ${detectedProxy.mode === "direct" ? "warn" : "ok"}`} />
              <span>{detectedProxy.label}</span>
            </div>
          )}
          {netError && <div className="inline-status is-err">{netError}</div>}
          {netResult && (
            <div className="net-probes-grid">
              <div className={`net-probe-card${netResult.modrinth.ok ? " is-ok" : " is-err"}`}>
                <div className="net-probe-top">
                  <span className="net-probe-name">Modrinth API</span>
                  <span className="mono-num net-probe-badge">
                    {netResult.modrinth.ok ? `OK • ${netResult.modrinth.latency_ms} мс` : "Ошибка"}
                  </span>
                </div>
                {netResult.modrinth.error && <div className="net-probe-err mono-num">{netResult.modrinth.error}</div>}
              </div>
              <div className={`net-probe-card${netResult.curseforge.ok ? " is-ok" : netResult.curseforge.status === "no_key" ? " is-warn" : " is-err"}`}>
                <div className="net-probe-top">
                  <span className="net-probe-name">CurseForge API</span>
                  <span className="mono-num net-probe-badge">
                    {netResult.curseforge.ok
                      ? `OK • ${netResult.curseforge.latency_ms} мс`
                      : netResult.curseforge.status === "no_key"
                        ? "Нет ключа"
                        : "Ошибка"}
                  </span>
                </div>
                {netResult.curseforge.error && netResult.curseforge.status !== "no_key" && (
                  <div className="net-probe-err mono-num">{netResult.curseforge.error}</div>
                )}
              </div>
            </div>
          )}
        </section>

        {/* Dependency Policy */}
        <section className="mh-panel">
          <div className="mh-panel-head">
            <h2>Установка зависимостей</h2>
          </div>
          <p className="help-text">
            Обязательные библиотеки для работы модов добавляются автоматически при установке.
          </p>
          <label className="chip-toggle chip-block">
            <input
              type="checkbox"
              checked={Boolean(config.auto_install_medium_deps)}
              disabled={medDepBusy}
              onChange={async (e) => {
                const nextVal = e.target.checked;
                setMedDepBusy(true);
                try {
                  const nextCfg = await api.saveConfig({ auto_install_medium_deps: nextVal });
                  onConfigSaved(nextCfg);
                  onToast(
                    nextVal
                      ? "Автоустановка рекомендуемых зависимостей включена"
                      : "Рекомендуемые зависимости требуют подтверждения",
                    "ok"
                  );
                } catch (err) {
                  onToast(`Ошибка сохранения: ${(err as Error).message}`, "err");
                } finally {
                  setMedDepBusy(false);
                }
              }}
            />
            <span>Автоматически устанавливать рекомендуемые зависимости</span>
          </label>
        </section>

        {/* Appearance */}
        <section className="mh-panel">
          <div className="mh-panel-head">
            <h2>Оформление интерфейса</h2>
            <button type="button" className="btn btn-ghost btn-xs" onClick={onToggleTheme}>
              Тема: {theme === "light" ? "Светлая" : "Тёмная"}
            </button>
          </div>
          <p className="help-text">Цветовой акцент активных элементов и текстура фона.</p>
          <div className="accent-presets" role="radiogroup" aria-label="Цветовой акцент">
            {ACCENTS.map((a) => (
              <button
                key={a.id}
                type="button"
                role="radio"
                aria-checked={accent === a.id}
                className={`accent-swatch-btn${accent === a.id ? " active" : ""}`}
                onClick={() => onAccentChange(a.id)}
              >
                <span className={`accent-dot accent-dot-${a.id}`} />
                <span>{a.label}</span>
              </button>
            ))}
          </div>
          <div style={{ marginTop: 14 }}>
            <div className="bg-presets" role="radiogroup" aria-label="Текстура фона">
              {BGS.map((b) => (
                <button
                  key={b.id}
                  type="button"
                  role="radio"
                  aria-checked={bgPreset === b.id}
                  className={`bg-swatch-btn${bgPreset === b.id ? " active" : ""}`}
                  onClick={() => onBgPresetChange(b.id)}
                >
                  <span className={`bg-preview bg-preview-${b.id}`} />
                  <span>{b.label}</span>
                </button>
              ))}
            </div>
          </div>
        </section>
      </div>
    </div>
  );
};
