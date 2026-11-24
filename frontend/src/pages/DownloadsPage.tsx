import React, { useMemo } from "react";
import type { AppSection } from "../components/AppNavRail";
import { CustomSelect, type CustomSelectOption } from "../components/CustomSelect";
import type { DownloadQueueSummary, DownloadStatus, DownloadTaskView } from "../types";
import { formatBytes } from "../utils";

interface DownloadsPageProps {
  tasks: DownloadTaskView[];
  summary: DownloadQueueSummary;
  loading: boolean;
  onPause: (id: string) => void;
  onResume: (id: string) => void;
  onCancel: (id: string) => void;
  onRetry: (id: string) => void;
  onRemove: (id: string) => void;
  onClearFinished: () => void;
  onSetMaxConcurrent: (n: number) => void;
  onNavigate: (section: AppSection) => void;
}

const CONCURRENCY_OPTS: CustomSelectOption[] = [
  { value: "1", label: "1 поток" },
  { value: "2", label: "2 потока" },
  { value: "4", label: "4 потока (стандарт)" },
  { value: "6", label: "6 потоков" },
  { value: "8", label: "8 потоков" },
  { value: "12", label: "12 потоков" },
  { value: "16", label: "16 потоков (быстро)" },
  { value: "24", label: "24 потока" },
  { value: "32", label: "32 потока (максимум)" },
];

function formatSpeed(bps: number): string {
  if (!bps || bps < 1) return "—";
  if (bps >= 1_048_576) return `${(bps / 1_048_576).toFixed(1)} МБ/с`;
  if (bps >= 1_024) return `${(bps / 1_024).toFixed(0)} КБ/с`;
  return `${Math.round(bps)} Б/с`;
}

function formatETA(secs: number): string {
  if (secs < 0) return "";
  if (secs < 60) return `~${secs} с`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  if (m < 60) return `~${m} мин ${s} с`;
  const h = Math.floor(m / 60);
  return `~${h} ч ${m % 60} мин`;
}

function categoryLabel(cat: string): string {
  switch (String(cat || "").toLowerCase()) {
    case "java":
      return "Java Runtime";
    case "minecraft":
      return "Minecraft";
    case "modloader":
      return "Загрузчик";
    case "mod":
      return "Мод";
    case "resourcepack":
      return "Ресурспак";
    case "shader":
      return "Шейдер";
    case "datapack":
      return "Датапак";
    default:
      return "Файл";
  }
}

function statusMeta(st: DownloadStatus): { label: string; cls: string } {
  switch (st) {
    case "downloading":
      return { label: "Загружается", cls: "is-active" };
    case "verifying":
      return { label: "Проверка файла", cls: "is-verifying" };
    case "queued":
      return { label: "В очереди", cls: "is-queued" };
    case "pending":
      return { label: "Ожидает", cls: "is-queued" };
    case "paused":
      return { label: "Приостановлена", cls: "is-paused" };
    case "completed":
      return { label: "Завершена", cls: "is-ok" };
    case "cancelled":
      return { label: "Отменена", cls: "is-muted" };
    case "error":
      return { label: "Ошибка", cls: "is-err" };
    default:
      return { label: st, cls: "is-muted" };
  }
}

export const DownloadsPage: React.FC<DownloadsPageProps> = ({
  tasks,
  summary,
  loading,
  onPause,
  onResume,
  onCancel,
  onRetry,
  onRemove,
  onClearFinished,
  onSetMaxConcurrent,
  onNavigate,
}) => {
  const groups = useMemo(() => {
    const active: DownloadTaskView[] = [];
    const queued: DownloadTaskView[] = [];
    const paused: DownloadTaskView[] = [];
    const failed: DownloadTaskView[] = [];
    const finished: DownloadTaskView[] = [];

    for (const t of tasks) {
      if (t.status === "downloading" || t.status === "verifying") active.push(t);
      else if (t.status === "queued" || t.status === "pending") queued.push(t);
      else if (t.status === "paused") paused.push(t);
      else if (t.status === "error") failed.push(t);
      else finished.push(t);
    }
    return {
      active,
      queued,
      paused,
      failed,
      finished: finished.reverse(),
    };
  }, [tasks]);

  const hasFinished = groups.failed.length + groups.finished.length > 0;
  const hasRunning = summary.active_count > 0 || summary.queued_count > 0;

  const renderTaskRow = (t: DownloadTaskView) => {
    const sm = statusMeta(t.status);
    const pct = Math.max(0, Math.min(100, Math.round(t.progress)));
    const eta = t.status === "downloading" ? formatETA(t.eta_seconds) : "";
    const isTerminal = t.status === "completed" || t.status === "cancelled" || t.status === "error";

    return (
      <div key={t.id} className={`mh-dl-row ${sm.cls}`}>
        <div className="mh-dl-row-top">
          <div className="mh-dl-row-main">
            <div className="mh-dl-title-line">
              <span className="mh-dl-name" title={t.name}>{t.name}</span>
              <span className="pill mh-dl-cat-pill">{categoryLabel(t.category)}</span>
              <span className={`mh-dl-status-pill ${sm.cls}`}>{sm.label}</span>
            </div>
            <div className="mh-dl-meta mono-num">
              <span>
                {formatBytes(t.downloaded_bytes)}
                {t.total_bytes > 0 ? ` / ${formatBytes(t.total_bytes)}` : ""}
              </span>
              {t.status === "downloading" && t.speed_bytes_per_sec > 0 && (
                <span>• {formatSpeed(t.speed_bytes_per_sec)}</span>
              )}
              {eta && <span>• осталось {eta}</span>}
              {t.total_bytes > 0 && <span>• {pct}%</span>}
            </div>
          </div>

          <div className="mh-dl-actions">
            {(t.status === "downloading" || t.status === "queued" || t.status === "pending") && (
              <button type="button" className="btn btn-ghost btn-xs" onClick={() => onPause(t.id)}>
                Пауза
              </button>
            )}
            {t.status === "paused" && (
              <button type="button" className="btn btn-primary btn-xs" onClick={() => onResume(t.id)}>
                Продолжить
              </button>
            )}
            {!isTerminal && (
              <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => onCancel(t.id)}>
                Отменить
              </button>
            )}
            {(t.status === "error" || t.status === "cancelled") && (
              <button type="button" className="btn btn-primary btn-xs" onClick={() => onRetry(t.id)}>
                Повторить
              </button>
            )}
            {isTerminal && (
              <button type="button" className="btn btn-ghost btn-xs" onClick={() => onRemove(t.id)}>
                Удалить
              </button>
            )}
          </div>
        </div>

        {(t.status === "downloading" || t.status === "verifying" || t.status === "paused") && (
          <div className="mh-dl-progress-track" aria-hidden="true">
            <div
              className={`mh-dl-progress-fill ${sm.cls}`}
              style={{ width: `${t.status === "verifying" ? 100 : pct}%` }}
            />
          </div>
        )}

        {t.error && (
          <div className="mh-dl-error-note" role="alert">
            {t.error}
          </div>
        )}
      </div>
    );
  };

  return (
    <div className="mh-page mh-page-enter">
      <header className="mh-page-head-row">
        <div>
          <h1 className="mh-page-title">Загрузки</h1>
          <p className="mh-page-subtitle">
            Очередь загрузки модов, сборок и компонентов игры.
          </p>
        </div>
        <div className="mh-dl-head-controls">
          <div className="mh-dl-conc-wrap">
            <span className="mh-muted-meta">Потоков:</span>
            <CustomSelect
              value={String(summary.max_concurrent || 4)}
              options={CONCURRENCY_OPTS}
              onChange={(v) => onSetMaxConcurrent(Number(v || 4))}
              className="cs-xs"
              ariaLabel="Одновременных загрузок"
            />
          </div>
          {hasFinished && (
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClearFinished}>
              Очистить завершённые
            </button>
          )}
        </div>
      </header>

      <section className="mh-home-stats-grid" aria-label="Сводка загрузок">
        <div className="mh-stat-card">
          <span className="mh-stat-label">Активные / В очереди</span>
          <div className="mh-stat-value mono-num">
            {summary.active_count} / {summary.queued_count}
          </div>
          <span className="mh-stat-sub">На паузе: {summary.paused_count}</span>
        </div>
        <div className="mh-stat-card">
          <span className="mh-stat-label">Общая скорость</span>
          <div className="mh-stat-value mono-num">
            {hasRunning ? formatSpeed(summary.total_speed_bytes_per_sec) : "—"}
          </div>
          <span className="mh-stat-sub mono-num">
            Лимит: {summary.max_concurrent} паралл.
          </span>
        </div>
        <div className="mh-stat-card">
          <span className="mh-stat-label">Общий прогресс</span>
          <div className="mh-stat-value mono-num">
            {summary.total_count > 0 ? `${Math.round(summary.overall_progress)}%` : "—"}
          </div>
          <span className="mh-stat-sub mono-num">
            {summary.total_bytes > 0
              ? `${formatBytes(summary.downloaded_bytes)} / ${formatBytes(summary.total_bytes)}`
              : "Нет активного объёма"}
          </span>
        </div>
        <div className="mh-stat-card">
          <span className="mh-stat-label">Завершено / Ошибки</span>
          <div className="mh-stat-value mono-num">
            {summary.completed_count} / {summary.error_count}
          </div>
          <span className="mh-stat-sub">Отменено: {summary.cancelled_count}</span>
        </div>
      </section>

      {hasRunning && (
        <div className="mh-dl-overall-bar">
          <div
            className="mh-dl-overall-fill"
            style={{ width: `${Math.max(2, Math.min(100, summary.overall_progress))}%` }}
          />
        </div>
      )}

      {loading && tasks.length === 0 ? (
        <div className="empty-state">
          <span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />
          Загрузка очереди…
        </div>
      ) : tasks.length === 0 ? (
        <section className="mh-panel mh-placeholder-card">
          <h2>Очередь загрузок пуста</h2>
          <p className="mh-placeholder-text">
            Здесь отображаются активные загрузки модов, сборок и компонентов игры. Завершённые загрузки удаляются автоматически.
          </p>
        </section>
      ) : (
        <div className="mh-dl-sections">
          {groups.active.length > 0 && (
            <section className="mh-panel">
              <div className="mh-panel-head">
                <h2>Активные загрузки ({groups.active.length})</h2>
              </div>
              <div className="mh-dl-list">{groups.active.map(renderTaskRow)}</div>
            </section>
          )}

          {groups.queued.length > 0 && (
            <section className="mh-panel">
              <div className="mh-panel-head">
                <h2>Очередь ({groups.queued.length})</h2>
              </div>
              <div className="mh-dl-list">{groups.queued.map(renderTaskRow)}</div>
            </section>
          )}

          {groups.paused.length > 0 && (
            <section className="mh-panel">
              <div className="mh-panel-head">
                <h2>Приостановленные ({groups.paused.length})</h2>
              </div>
              <div className="mh-dl-list">{groups.paused.map(renderTaskRow)}</div>
            </section>
          )}

          {groups.failed.length > 0 && (
            <section className="mh-panel">
              <div className="mh-panel-head">
                <h2>Ошибки ({groups.failed.length})</h2>
              </div>
              <div className="mh-dl-list">{groups.failed.map(renderTaskRow)}</div>
            </section>
          )}

          {groups.finished.length > 0 && (
            <section className="mh-panel">
              <div className="mh-panel-head">
                <h2>Отменённые ({groups.finished.length})</h2>
              </div>
              <div className="mh-dl-list">{groups.finished.map(renderTaskRow)}</div>
            </section>
          )}
        </div>
      )}
    </div>
  );
};
