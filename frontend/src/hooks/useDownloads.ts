import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api";
import { isWailsDesktop, subscribeBackendEvents } from "../services/bridge";
import { pushToast } from "../toastBus";
import type { DownloadQueueSummary, DownloadTaskView } from "../types";

const EMPTY_SUMMARY: DownloadQueueSummary = {
  active_count: 0,
  queued_count: 0,
  paused_count: 0,
  completed_count: 0,
  error_count: 0,
  cancelled_count: 0,
  total_count: 0,
  total_bytes: 0,
  downloaded_bytes: 0,
  overall_progress: 0,
  total_speed_bytes_per_sec: 0,
  max_concurrent: 4,
};

function upsertTask(list: DownloadTaskView[], task: DownloadTaskView): DownloadTaskView[] {
  const idx = list.findIndex((t) => t.id === task.id);
  if (idx >= 0) {
    const next = list.slice();
    next[idx] = task;
    return next;
  }
  return [...list, task];
}

export function useDownloads() {
  const [tasks, setTasks] = useState<DownloadTaskView[]>([]);
  const [summary, setSummary] = useState<DownloadQueueSummary>(EMPTY_SUMMARY);
  const [loading, setLoading] = useState(true);
  const completedTimersRef = useRef<Map<string, number>>(new Map());
  const isMountedRef = useRef(true);

  const scheduleCompletedCleanup = useCallback((taskId: string, delayMs = 800) => {
    if (!taskId || completedTimersRef.current.has(taskId)) return;
    const tid = window.setTimeout(() => {
      completedTimersRef.current.delete(taskId);
      void api
        .removeDownload(taskId)
        .catch(() => {})
        .finally(() => {
          if (!isMountedRef.current) return;
          setTasks((prev) => prev.filter((t) => t.id !== taskId));
        });
    }, delayMs);
    completedTimersRef.current.set(taskId, tid);
  }, []);

  const refresh = useCallback(async () => {
    try {
      const snap = await api.getDownloads();
      if (!isMountedRef.current) return;
      const rawTasks = snap.tasks || [];
      const completed = rawTasks.filter((t) => t.status === "completed");
      if (completed.length > 0) {
        // Remove already-completed tasks cleanly in background (safe after Wait callers finish)
        for (const t of completed) {
          scheduleCompletedCleanup(t.id, 250);
        }
      }
      setTasks(rawTasks.filter((t) => t.status !== "completed" || completedTimersRef.current.has(t.id)));
      if (snap.summary) setSummary(snap.summary);
    } catch {
      // Ignore transient errors on initial load
    } finally {
      if (isMountedRef.current) {
        setLoading(false);
      }
    }
  }, [scheduleCompletedCleanup]);

  useEffect(() => {
    isMountedRef.current = true;
    void refresh();
    return () => {
      isMountedRef.current = false;
      for (const tid of completedTimersRef.current.values()) {
        window.clearTimeout(tid);
      }
      completedTimersRef.current.clear();
    };
  }, [refresh]);

  // Real-time Go -> React EventBus subscription via Wails Runtime Events
  useEffect(() => {
    return subscribeBackendEvents((ev) => {
      switch (ev.name) {
        case "download.created":
        case "download.progress":
        case "download.status":
        case "download.paused":
        case "download.resumed":
        case "download.cancelled": {
          if (ev.payload && typeof ev.payload === "object" && "id" in ev.payload) {
            const tv = ev.payload as DownloadTaskView;
            if (tv.status === "completed") {
              setTasks((prev) => upsertTask(prev, tv));
              scheduleCompletedCleanup(tv.id, 800);
            } else {
              setTasks((prev) => upsertTask(prev, tv));
            }
          }
          break;
        }
        case "download.completed": {
          if (ev.payload && typeof ev.payload === "object" && "id" in ev.payload) {
            const tv = ev.payload as DownloadTaskView;
            setTasks((prev) => upsertTask(prev, tv));
            scheduleCompletedCleanup(tv.id, 800);
          }
          break;
        }
        case "download.error": {
          if (ev.payload && typeof ev.payload === "object" && "id" in ev.payload) {
            const tv = ev.payload as DownloadTaskView;
            setTasks((prev) => upsertTask(prev, tv));
            if (tv.error) {
              pushToast(`Ошибка загрузки «${tv.name}»: ${tv.error}`, "err");
            }
          }
          break;
        }
        case "download.removed": {
          const rec = ev.payload as { id?: string } | undefined;
          if (rec?.id) {
            setTasks((prev) => prev.filter((t) => t.id !== rec.id));
          }
          break;
        }
        case "download.summary": {
          if (ev.payload && typeof ev.payload === "object" && "total_count" in ev.payload) {
            setSummary(ev.payload as DownloadQueueSummary);
          }
          break;
        }
      }
    });
  }, []);

  // Graceful polling fallback in --web browser mode (where Wails Runtime Events are absent)
  const hasRunning = summary.active_count > 0 || summary.queued_count > 0;
  useEffect(() => {
    if (isWailsDesktop() || !hasRunning) return;
    const tid = window.setInterval(() => {
      void refresh();
    }, 500);
    return () => window.clearInterval(tid);
  }, [hasRunning, refresh]);

  const pause = useCallback(async (id: string) => {
    try {
      const tv = await api.pauseDownload(id);
      setTasks((prev) => upsertTask(prev, tv));
    } catch (err) {
      pushToast(`Не удалось приостановить: ${(err as Error).message}`, "err");
    }
  }, []);

  const resume = useCallback(async (id: string) => {
    try {
      const tv = await api.resumeDownload(id);
      setTasks((prev) => upsertTask(prev, tv));
    } catch (err) {
      pushToast(`Не удалось возобновить: ${(err as Error).message}`, "err");
    }
  }, []);

  const cancel = useCallback(async (id: string) => {
    try {
      const tv = await api.cancelDownload(id);
      setTasks((prev) => upsertTask(prev, tv));
      void refresh();
    } catch (err) {
      pushToast(`Не удалось отменить: ${(err as Error).message}`, "err");
    }
  }, [refresh]);

  const retry = useCallback(async (id: string) => {
    try {
      const tv = await api.retryDownload(id);
      setTasks((prev) => upsertTask(prev, tv));
    } catch (err) {
      pushToast(`Не удалось повторить: ${(err as Error).message}`, "err");
    }
  }, []);

  const remove = useCallback(async (id: string) => {
    try {
      await api.removeDownload(id);
      setTasks((prev) => prev.filter((t) => t.id !== id));
      void refresh();
    } catch (err) {
      pushToast(`Не удалось удалить запись: ${(err as Error).message}`, "err");
    }
  }, [refresh]);

  const clearFinished = useCallback(async () => {
    try {
      const n = await api.clearFinishedDownloads();
      await refresh();
      if (n > 0) pushToast(`Очищено записей: ${n}`, "info");
    } catch (err) {
      pushToast(`Ошибка очистки: ${(err as Error).message}`, "err");
    }
  }, [refresh]);

  const setMaxConcurrent = useCallback(async (n: number) => {
    try {
      await api.setMaxConcurrentDownloads(n);
      await refresh();
      pushToast(`Максимум одновременных загрузок: ${n}`, "ok");
    } catch (err) {
      pushToast(`Ошибка сохранения: ${(err as Error).message}`, "err");
    }
  }, [refresh]);

  return {
    tasks,
    summary,
    loading,
    refresh,
    pause,
    resume,
    cancel,
    retry,
    remove,
    clearFinished,
    setMaxConcurrent,
  };
}
