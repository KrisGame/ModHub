import React, { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { isSessionActive } from "../hooks/useLauncher";
import { desktopBridge, isWailsDesktop } from "../services/bridge";
import type { CrashDiagnosis, CrashReportInfo, GameInstance, GameLogLine, GameProcessState, GameSessionInfo } from "../types";
import { formatBytes, formatDate } from "../utils";

interface InstanceConsoleTabProps {
  instance: GameInstance;
  session: GameSessionInfo | null;
  logs: GameLogLine[];
  onLoadLogs: (instanceId: string) => Promise<void>;
  onClearDisplay: (instanceId: string) => void;
  onLaunch: () => void;
  onStop: (force: boolean) => void;
  onOpenMods?: () => void;
  onOpenSettings?: () => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

export function processStateMeta(st?: GameProcessState, gameLoaded?: boolean): { label: string; cls: string } {
  switch (st) {
    case "checking_profile":
      return { label: "Проверка профиля", cls: "is-active" };
    case "checking_auth":
      return { label: "Проверка авторизации", cls: "is-active" };
    case "preparing_files":
      return { label: "Подготовка файлов и Java", cls: "is-active" };
    case "building_command":
      return { label: "Формирование команды", cls: "is-active" };
    case "starting":
      return { label: "Запуск процесса", cls: "is-active" };
    case "running":
      return { label: gameLoaded ? "Игра запущена" : "Процесс запущен", cls: "is-ok" };
    case "stopping":
      return { label: "Игра завершается", cls: "is-paused" };
    case "exited":
      return { label: "Игра завершена", cls: "is-muted" };
    case "error":
      return { label: "Ошибка запуска / сбой", cls: "is-err" };
    default:
      return { label: "Не запущена", cls: "is-muted" };
  }
}

export function formatUptime(startedAt: number, endedAt: number, active: boolean, nowSec: number): string {
  if (!startedAt || startedAt <= 0) return "—";
  const end = active ? nowSec : endedAt || nowSec;
  const diff = Math.max(0, end - startedAt);
  const h = Math.floor(diff / 3600);
  const m = Math.floor((diff % 3600) / 60);
  const s = diff % 60;
  if (h > 0) return `${h} ч ${String(m).padStart(2, "0")} мин`;
  if (m > 0) return `${m} мин ${String(s).padStart(2, "0")} с`;
  return `${s} с`;
}

export const InstanceConsoleTab: React.FC<InstanceConsoleTabProps> = ({
  instance,
  session,
  logs,
  onLoadLogs,
  onClearDisplay,
  onLaunch,
  onStop,
  onOpenMods,
  onOpenSettings,
  onToast,
}) => {
  const [autoScroll, setAutoScroll] = useState(true);
  const [levelFilter, setLevelFilter] = useState<"ALL" | "WARN" | "ERROR">("ALL");
  const [crashes, setCrashes] = useState<CrashReportInfo[]>([]);
  const [fetchedDiagnosis, setFetchedDiagnosis] = useState<CrashDiagnosis | null>(null);
  const [nowSec, setNowSec] = useState(() => Math.floor(Date.now() / 1000));
  const viewportRef = useRef<HTMLDivElement | null>(null);

  const active = isSessionActive(session?.state);
  const sm = processStateMeta(session?.state, session?.game_loaded);

  useEffect(() => {
    void onLoadLogs(instance.id);
    api
      .listInstanceCrashReports(instance.id)
      .then((list) => {
        setCrashes(list);
        if (!active && (session?.state === "error" || list.length > 0)) {
          api.analyzeInstanceCrash(instance.id).then(setFetchedDiagnosis).catch(() => {});
        } else {
          setFetchedDiagnosis(null);
        }
      })
      .catch(() => setCrashes([]));
  }, [instance.id, session?.state, session?.ended_at, active, onLoadLogs]);

  const diagnosis = session?.crash_diagnosis || fetchedDiagnosis;

  // Live uptime tick while game is running
  useEffect(() => {
    if (!active) return;
    const tid = window.setInterval(() => setNowSec(Math.floor(Date.now() / 1000)), 1000);
    return () => window.clearInterval(tid);
  }, [active]);

  // Web-mode log polling fallback while active
  useEffect(() => {
    if (isWailsDesktop() || !active) return;
    const tid = window.setInterval(() => {
      void onLoadLogs(instance.id);
    }, 900);
    return () => window.clearInterval(tid);
  }, [active, instance.id, onLoadLogs]);

  const visibleLogs = useMemo(() => {
    if (levelFilter === "ALL") return logs;
    if (levelFilter === "WARN") return logs.filter((l) => l.level === "WARN" || l.level === "ERROR" || l.level === "FATAL");
    return logs.filter((l) => l.level === "ERROR" || l.level === "FATAL");
  }, [logs, levelFilter]);

  useEffect(() => {
    if (!autoScroll || !viewportRef.current) return;
    viewportRef.current.scrollTop = viewportRef.current.scrollHeight;
  }, [visibleLogs.length, autoScroll]);

  const handleCopyLogs = async () => {
    if (!visibleLogs.length) return;
    const text = visibleLogs.map((l) => `[${l.timestamp}] [${l.stream.toUpperCase()}/${l.level}] ${l.text}`).join("\n");
    try {
      await navigator.clipboard.writeText(text);
      onToast(`Скопировано строк: ${visibleLogs.length}`, "ok");
    } catch {
      onToast("Не удалось скопировать логи в буфер обмена", "err");
    }
  };

  return (
    <div className="mh-inst-overview">
      {/* Live Process Header & Controls */}
      <section className="mh-panel">
        <div className="mh-panel-head">
          <div style={{ display: "flex", alignItems: "center", gap: 10, flexWrap: "wrap" }}>
            <h2>Консоль игры</h2>
            <span className={`mh-dl-status-pill ${sm.cls}`}>{sm.label}</span>
            {session?.started_at ? (
              <span className="pill mono-num">
                Время: {formatUptime(session.started_at, session.ended_at, active, nowSec)}
              </span>
            ) : null}
          </div>

          <div style={{ display: "inline-flex", gap: 6, flexWrap: "wrap" }}>
            {!active ? (
              <button type="button" className="btn btn-primary btn-xs" onClick={onLaunch}>
                ▶ Играть
              </button>
            ) : (
              <>
                <button type="button" className="btn btn-ghost btn-xs" onClick={() => onStop(false)}>
                  Остановить
                </button>
                <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => onStop(true)}>
                  Принудительно
                </button>
              </>
            )}
          </div>
        </div>

        {session?.stage_message && (
          <div className="mh-muted-meta mono-num">
            {session.stage_message}
            {!active && session.ended_at > 0 ? ` • Код выхода: ${session.exit_code}` : ""}
          </div>
        )}

        {diagnosis && !active && (
          <div className="mh-crash-diag-card" role="alert">
            <div className="mh-crash-diag-head">
              <div>
                <span className="mh-crash-diag-badge">Умный разбор причины сбоя</span>
                <h3 className="mh-crash-diag-title">{diagnosis.title}</h3>
              </div>
              <div style={{ display: "inline-flex", gap: 6, flexWrap: "wrap" }}>
                {diagnosis.action_hint === "settings" && onOpenSettings && (
                  <button type="button" className="btn btn-primary btn-xs" onClick={onOpenSettings}>
                    Открыть настройки запуска
                  </button>
                )}
                {diagnosis.action_hint === "mods" && onOpenMods && (
                  <button type="button" className="btn btn-primary btn-xs" onClick={onOpenMods}>
                    Открыть моды профиля
                  </button>
                )}
              </div>
            </div>
            <p className="mh-crash-diag-cause">{diagnosis.cause}</p>
            {diagnosis.related_mod && (
              <div className="mh-crash-diag-mod mono-num">
                Связанный компонент: <strong>{diagnosis.related_mod}</strong>
              </div>
            )}
            <div className="mh-crash-diag-fix">
              <strong>Что сделать:</strong> {diagnosis.suggestion}
            </div>
          </div>
        )}

        {session?.state === "error" && session.error && !diagnosis && (
          <div className="mh-dl-error-note" role="alert">
            <strong>Ошибка:</strong> {session.error}
          </div>
        )}

        {/* Console Toolbar */}
        <div className="mh-console-toolbar">
          <div className="mh-console-filters">
            {(["ALL", "WARN", "ERROR"] as const).map((lv) => (
              <button
                key={lv}
                type="button"
                className={`btn btn-xs ${levelFilter === lv ? "btn-primary" : "btn-ghost"}`}
                onClick={() => setLevelFilter(lv)}
              >
                {lv === "ALL" ? `Все (${logs.length})` : lv === "WARN" ? "Warn+" : "Ошибки"}
              </button>
            ))}
          </div>

          <div className="mh-console-actions">
            <label className="chip-toggle">
              <input type="checkbox" checked={autoScroll} onChange={(e) => setAutoScroll(e.target.checked)} />
              <span>Автопрокрутка</span>
            </label>
            <button type="button" className="btn btn-ghost btn-xs" disabled={!visibleLogs.length} onClick={handleCopyLogs}>
              Копировать
            </button>
            <button type="button" className="btn btn-ghost btn-xs" disabled={!logs.length} onClick={() => onClearDisplay(instance.id)}>
              Очистить экран
            </button>
            {desktopBridge.isDesktop() && (
              <button
                type="button"
                className="btn btn-ghost btn-xs"
                onClick={() => {
                  desktopBridge.openInstanceLogFile(instance.id).catch((err) => onToast(String((err as Error).message || err), "err"));
                }}
              >
                Открыть лог-файл
              </button>
            )}
          </div>
        </div>

        {/* Log Output Viewport */}
        <div ref={viewportRef} className="mh-console-viewport mono-num" role="log" aria-live="polite">
          {visibleLogs.length === 0 ? (
            <div className="mh-console-empty">
              {active
                ? "Ожидание вывода Java / Minecraft…"
                : "Логи текущей сессии отсутствуют. Нажмите «Играть», чтобы запустить Minecraft и видеть вывод в реальном времени."}
            </div>
          ) : (
            visibleLogs.map((ln) => (
              <div key={`${ln.session_id}-${ln.seq}`} className={`mh-log-line is-${ln.level.toLowerCase()}`}>
                <span className="mh-log-ts">[{ln.timestamp}]</span>
                <span className="mh-log-lvl">[{ln.level}]</span>
                <span className="mh-log-txt">{ln.text}</span>
              </div>
            ))
          )}
        </div>
      </section>

      {/* Crash Reports Section */}
      {crashes.length > 0 && (
        <section className="mh-panel">
          <div className="mh-panel-head">
            <h2>Отчёты о сбоях (Crash Reports: {crashes.length})</h2>
          </div>
          <div className="mh-saved-list">
            {crashes.slice(0, 8).map((cr) => (
              <div key={cr.filename} className="mh-saved-row">
                <div className="mh-saved-info">
                  <span className="mh-saved-name mono-num">{cr.filename}</span>
                  <span className="mh-saved-meta mono-num">
                    {formatDate(new Date(cr.mod_time * 1000).toISOString())} • {formatBytes(cr.size)}
                    {cr.summary ? ` • ${cr.summary}` : ""}
                  </span>
                </div>
                {desktopBridge.isDesktop() && (
                  <button
                    type="button"
                    className="btn btn-ghost btn-xs"
                    onClick={() => {
                      desktopBridge.openCrashReport(instance.id, cr.filename).catch((err) => onToast(String((err as Error).message || err), "err"));
                    }}
                  >
                    Открыть
                  </button>
                )}
              </div>
            ))}
          </div>
        </section>
      )}
    </div>
  );
};
