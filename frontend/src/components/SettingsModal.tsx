import React, { useEffect, useRef, useState } from "react";
import { api } from "../api";
import { useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import type { AccentPreset, BgPreset, NetworkCheckResponse, PublicConfig } from "../types";

interface SettingsModalProps {
  open: boolean;
  config: PublicConfig;
  accent: AccentPreset;
  bgPreset: BgPreset;
  onAccentChange: (a: AccentPreset) => void;
  onBgPresetChange: (b: BgPreset) => void;
  onClose: () => void;
  onConfigSaved: (cfg: PublicConfig) => void;
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

export const SettingsModal: React.FC<SettingsModalProps> = ({
  open,
  config,
  accent,
  bgPreset,
  onAccentChange,
  onBgPresetChange,
  onClose,
  onConfigSaved,
  onToast,
}) => {
  const { mounted, shown } = useOverlayTransition(open, 230);
  const [cfKey, setCfKey] = useState("");
  const [showKey, setShowKey] = useState(false);
  const [statusMsg, setStatusMsg] = useState<{ text: string; kind: "" | "ok" | "err" }>({ text: "", kind: "" });
  const [busy, setBusy] = useState(false);
  const [keyBtnFb, setKeyBtnFb] = useState<"" | "success" | "error">("");

  // Network / Proxy state
  const [proxyInput, setProxyInput] = useState("");
  const [proxyBusy, setProxyBusy] = useState(false);
  const [proxyBtnFb, setProxyBtnFb] = useState<"" | "success" | "error">("");
  const [netBusy, setNetBusy] = useState(false);
  const [cacheBusy, setCacheBusy] = useState(false);
  const [medDepBusy, setMedDepBusy] = useState(false);
  const [netBtnFb, setNetBtnFb] = useState<"" | "success" | "error">("");
  const [netResult, setNetResult] = useState<NetworkCheckResponse | null>(null);
  const [netError, setNetError] = useState("");
  const fbTimersRef = useRef<number[]>([]);

  const flashFb = (setter: React.Dispatch<React.SetStateAction<"" | "success" | "error">>, kind: "success" | "error") => {
    setter(kind);
    const tid = window.setTimeout(() => setter(""), 1800);
    fbTimersRef.current.push(tid);
  };

  useEffect(() => {
    return () => {
      for (const tid of fbTimersRef.current) window.clearTimeout(tid);
      fbTimersRef.current = [];
    };
  }, []);

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

  useEffect(() => {
    if (!open) return;
    setCfKey("");
    setShowKey(false);
    setProxyInput(config.proxy_url || "");
    setNetError("");
    setStatusMsg({
      text: config.curseforge_configured
        ? `Текущий ключ подключён: ${config.curseforge_key_masked}`
        : "Ключ не задан — работает только Modrinth.",
      kind: config.curseforge_configured ? "ok" : "",
    });
  }, [open, config]);

  const runNetworkCheck = async () => {
    setNetBusy(true);
    setNetBtnFb("");
    setNetError("");
    try {
      const res = await api.checkNetwork();
      setNetResult(res);
      flashFb(setNetBtnFb, res.modrinth.ok ? "success" : "error");
    } catch (err) {
      setNetError(String((err as Error).message || err));
      flashFb(setNetBtnFb, "error");
    } finally {
      setNetBusy(false);
    }
  };

  const handleSaveProxy = async (nextVal?: string) => {
    const val = (nextVal !== undefined ? nextVal : proxyInput).trim();
    setProxyBusy(true);
    setProxyBtnFb("");
    try {
      const nextCfg = await api.saveConfig({ proxy_url: val });
      onConfigSaved(nextCfg);
      setProxyInput(nextCfg.proxy_url || "");
      flashFb(setProxyBtnFb, "success");
      onToast(val ? `Прокси сохранён: ${val}` : "Включён автоподхват прокси", "ok");
      await runNetworkCheck();
    } catch (err) {
      flashFb(setProxyBtnFb, "error");
      onToast(`Ошибка сохранения прокси: ${(err as Error).message}`, "err");
    } finally {
      setProxyBusy(false);
    }
  };

  const handleTestAndSave = async () => {
    const val = cfKey.trim();
    if (!val) {
      setStatusMsg({ text: "Вставьте Core API Key", kind: "err" });
      flashFb(setKeyBtnFb, "error");
      return;
    }
    setBusy(true);
    setKeyBtnFb("");
    setStatusMsg({ text: "Проверка ключа CurseForge...", kind: "" });
    try {
      const testRes = await api.testCurseForgeKey(val);
      if (!testRes.ok) {
        setStatusMsg({ text: testRes.error || "Ключ не подошёл", kind: "err" });
        flashFb(setKeyBtnFb, "error");
        return;
      }
      const nextCfg = await api.saveConfig({ curseforge_api_key: val });
      onConfigSaved(nextCfg);
      setCfKey("");
      setStatusMsg({ text: "Ключ проверен и сохранён!", kind: "ok" });
      flashFb(setKeyBtnFb, "success");
      onToast("CurseForge API-ключ сохранён", "ok");
    } catch (err) {
      setStatusMsg({ text: String((err as Error).message || err), kind: "err" });
      flashFb(setKeyBtnFb, "error");
    } finally {
      setBusy(false);
    }
  };

  const handleSaveRaw = async () => {
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

  if (!mounted) return null;

  const detectedProxy = netResult?.proxy || config.proxy_detected;

  return (
    <div className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`} role="dialog" aria-modal="true" aria-hidden={!shown} onClick={onClose}>
      <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title">Настройки API, сети и оформления</h2>
            <p className="modal-subtitle">Ключ и прокси хранятся локально в <code>data/config.json</code>.</p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}><CloseIcon size={15} /></button>
        </div>

        <div className="modal-body">
          {/* Network & Proxy Section */}
          <div className="settings-section">
            <div className="net-section-head">
              <h3>Сеть и прокси</h3>
              <div style={{ display: "inline-flex", gap: 6, flexWrap: "wrap" }}>
                <button
                  type="button"
                  className={`btn btn-ghost btn-xs${cacheBusy ? " is-loading" : ""}`}
                  disabled={cacheBusy}
                  aria-busy={cacheBusy}
                  title="Очистить локальный и серверный кэш поиска, версий и зависимостей"
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
              <button
                type="button"
                className={`btn btn-primary btn-xs${netBusy ? " is-loading" : ""}${netBtnFb ? ` is-${netBtnFb}` : ""}`}
                disabled={netBusy}
                aria-busy={netBusy}
                onClick={runNetworkCheck}
              >
                {netBusy ? (
                  <><span className="spinner spinner-xs" aria-hidden="true" /><span>Проверка…</span></>
                ) : netBtnFb === "success" ? (
                  "API доступен ✓"
                ) : netBtnFb === "error" ? (
                  "Ошибка сети"
                ) : (
                  "Проверить API"
                )}
              </button>
              </div>
            </div>
            <p className="help-text">
              Автоматически читает системный прокси Windows и <code>HTTP_PROXY</code> / <code>HTTPS_PROXY</code>. Оставьте поле пустым для автоопределения или введите адрес вручную (<code>http://...</code>, <code>socks5://...</code>, <code>direct</code>).
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
              <button
                type="button"
                className={`btn btn-ghost btn-sm${proxyBusy ? " is-loading" : ""}${proxyBtnFb ? ` is-${proxyBtnFb}` : ""}`}
                disabled={proxyBusy}
                aria-busy={proxyBusy}
                onClick={() => handleSaveProxy()}
              >
                {proxyBusy ? (
                  <><span className="spinner spinner-xs" aria-hidden="true" /><span>Сохранение…</span></>
                ) : proxyBtnFb === "success" ? (
                  "Сохранено ✓"
                ) : proxyBtnFb === "error" ? (
                  "Ошибка"
                ) : (
                  "Сохранить"
                )}
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
                  {netResult.modrinth.error && (
                    <div className="net-probe-err mono-num" title={netResult.modrinth.error}>
                      {netResult.modrinth.error}
                    </div>
                  )}
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
                    <div className="net-probe-err mono-num" title={netResult.curseforge.error}>
                      {netResult.curseforge.error}
                    </div>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* CurseForge API Key Section */}
          <div className="settings-section">
            <h3>CurseForge Core API v1</h3>
            <p className="help-text">
              Чтобы включить поиск по CurseForge, откройте{" "}
              <a href="https://console.curseforge.com/" target="_blank" rel="noopener noreferrer">console.curseforge.com</a>,
              перейдите в <strong>API Keys</strong>, скопируйте <strong>Core API Key</strong> и вставьте его сюда.
              Затем нажмите «Проверить и сохранить».
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
              <button
                type="button"
                className={`btn btn-primary${busy ? " is-loading" : ""}${keyBtnFb ? ` is-${keyBtnFb}` : ""}`}
                disabled={busy}
                aria-busy={busy}
                onClick={handleTestAndSave}
              >
                {busy ? (
                  <><span className="spinner spinner-xs" aria-hidden="true" /><span>Проверка…</span></>
                ) : keyBtnFb === "success" ? (
                  "Сохранено ✓"
                ) : keyBtnFb === "error" ? (
                  "Не подошёл"
                ) : (
                  "Проверить и сохранить"
                )}
              </button>
            </div>
            <div className="settings-sub-actions">
              <button type="button" className="btn btn-ghost btn-xs" disabled={busy || !cfKey.trim()} onClick={handleSaveRaw}>
                Сохранить без проверки
              </button>
              <button type="button" className="btn btn-danger-ghost btn-xs" disabled={busy || !config.curseforge_configured} onClick={handleClearKey}>
                Удалить ключ
              </button>
            </div>
            {statusMsg.text && (
              <div className={`inline-status${statusMsg.kind ? ` is-${statusMsg.kind}` : ""}`}>
                {statusMsg.text}
              </div>
            )}
          </div>

          {/* Intelligent Dependency Auto-Install Policy */}
          <div className="settings-section">
            <h3>Интеллектуальный поиск зависимостей</h3>
            <p className="help-text">
              Зависимости из API, манифеста .jar и однозначных требований в описании (высокая уверенность) добавляются автоматически.
              Зависимости со средней уверенностью по умолчанию требуют подтверждения.
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
                        ? "Автоустановка зависимостей со средней уверенностью включена"
                        : "Зависимости со средней уверенностью требуют подтверждения",
                      "ok"
                    );
                  } catch (err) {
                    onToast(`Ошибка сохранения: ${(err as Error).message}`, "err");
                  } finally {
                    setMedDepBusy(false);
                  }
                }}
              />
              <span>Автоматически устанавливать зависимости со средней уверенностью (из описания)</span>
            </label>
          </div>

          <div className="settings-section">
            <h3>Цветовой акцент</h3>
            <p className="help-text">Сдержанная цветовая палитра для активных кнопок, вкладок и подсветки карточек.</p>
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
          </div>

          <div className="settings-section">
            <h3>Текстура фона</h3>
            <p className="help-text">Фоновый паттерн страницы под каталогом.</p>
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
        </div>
      </div>
    </div>
  );
};
