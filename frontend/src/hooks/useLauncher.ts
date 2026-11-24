import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { isWailsDesktop, subscribeBackendEvents } from "../services/bridge";
import { pushToast } from "../toastBus";
import type {
  GameLogLine,
  GameProcessState,
  GameSessionInfo,
  LaunchInstanceInput,
  SystemMemoryInfo,
} from "../types";

/**
 * Максимальное число строк лога, сохраняемое в оперативной памяти для каждого инстанса.
 * Ограничено для исключения переполнения кучи V8 / WebView2 при длительной работе клиента.
 */
const MAX_UI_LOG_LINES = 2500;

/**
 * Период троттлинга батчинга логов в миллисекундах.
 * Коалесцирует десятки событий game.log от бэкенда в один плавный кадр обновления UI.
 */
const LOG_FLUSH_INTERVAL_MS = 120;

/**
 * Проверка активности игровой сессии (состояния, при которых процесс занят или запускается).
 */
export function isSessionActive(state?: GameProcessState): boolean {
  return (
    state === "checking_profile" ||
    state === "checking_auth" ||
    state === "preparing_files" ||
    state === "building_command" ||
    state === "starting" ||
    state === "running" ||
    state === "stopping"
  );
}

/**
 * Корректное добавление строк в кольцевой буфер инстанса без лишних аллокаций.
 */
function appendToLogBuffer(
  existing: GameLogLine[] | undefined,
  incoming: GameLogLine[],
  maxLines: number
): GameLogLine[] {
  if (!existing || existing.length === 0) {
    return incoming.length > maxLines ? incoming.slice(incoming.length - maxLines) : [...incoming];
  }
  const total = existing.length + incoming.length;
  if (total <= maxLines) {
    return existing.concat(incoming);
  }
  const overflow = total - maxLines;
  if (overflow >= existing.length) {
    return incoming.slice(incoming.length - maxLines);
  }
  return existing.slice(overflow).concat(incoming);
}

export function useLauncher() {
  const [sessionsByInstance, setSessionsByInstance] = useState<Record<string, GameSessionInfo>>({});
  const [logsByInstance, setLogsByInstance] = useState<Record<string, GameLogLine[]>>({});
  const [systemMem, setSystemMem] = useState<SystemMemoryInfo>({
    total_ram_mb: 8192,
    available_ram_mb: 4096,
    recommended_max_mb: 4096,
  });

  // Защита жизненного цикла от обновлений после unmount
  const isMountedRef = useRef(true);

  // Буфер входящих логов для исключения блокировки UI-потока (ANR)
  const pendingLogsMapRef = useRef<Map<string, GameLogLine[]>>(new Map());
  const flushTimerRef = useRef<number | null>(null);
  const rafHandleRef = useRef<number | null>(null);

  // Флаг наличия активных фоновых сессий для динамического polling fallback
  const sessionsRef = useRef(sessionsByInstance);
  sessionsRef.current = sessionsByInstance;

  /**
   * Сброс буферизованных логов в состояние React.
   * Выполняется пакетом через requestAnimationFrame для идеальной 60 FPS плавности.
   */
  const scheduleFlushLogs = useCallback(() => {
    if (flushTimerRef.current !== null) return;

    flushTimerRef.current = window.setTimeout(() => {
      flushTimerRef.current = null;

      if (!isMountedRef.current) return;
      if (pendingLogsMapRef.current.size === 0) return;

      if (rafHandleRef.current !== null) {
        cancelAnimationFrame(rafHandleRef.current);
      }

      rafHandleRef.current = requestAnimationFrame(() => {
        rafHandleRef.current = null;
        if (!isMountedRef.current) return;

        const pending = pendingLogsMapRef.current;
        if (pending.size === 0) return;

        // Копируем и сразу очищаем очередь
        const entriesToApply = Array.from(pending.entries());
        pendingLogsMapRef.current = new Map();

        setLogsByInstance((prev) => {
          let hasChanges = false;
          const next = { ...prev };

          for (const [instId, newLines] of entriesToApply) {
            if (!newLines || newLines.length === 0) continue;
            const updated = appendToLogBuffer(next[instId], newLines, MAX_UI_LOG_LINES);
            next[instId] = updated;
            hasChanges = true;
          }

          return hasChanges ? next : prev;
        });
      });
    }, LOG_FLUSH_INTERVAL_MS);
  }, []);

  /**
   * Запрос актуального снапшота сессий с бэкенда.
   */
  const refresh = useCallback(async () => {
    try {
      const snap = await api.getLauncherSnapshot();
      if (!isMountedRef.current) return;

      const map: Record<string, GameSessionInfo> = {};
      for (const s of snap.sessions || []) {
        if (s && s.instance_id) {
          map[s.instance_id] = s;
        }
      }

      setSessionsByInstance((prev) => {
        // Проверяем реальные изменения перед пересозданием объекта состояния
        const prevKeys = Object.keys(prev);
        const nextKeys = Object.keys(map);
        if (prevKeys.length === nextKeys.length) {
          let unchanged = true;
          for (const k of nextKeys) {
            const a = prev[k];
            const b = map[k];
            if (!a || !b || a.id !== b.id || a.state !== b.state || a.pid !== b.pid || a.stage_message !== b.stage_message) {
              unchanged = false;
              break;
            }
          }
          if (unchanged) return prev;
        }
        return map;
      });

      if (snap.system) {
        setSystemMem((prev) => {
          if (
            prev.total_ram_mb === snap.system.total_ram_mb &&
            prev.available_ram_mb === snap.system.available_ram_mb &&
            prev.recommended_max_mb === snap.system.recommended_max_mb
          ) {
            return prev;
          }
          return snap.system;
        });
      }
    } catch {
      // Игнорируем переходные сетевые / IPC сбои на фазе инициализации
    }
  }, []);

  // Первоначальная загрузка состояния
  useEffect(() => {
    isMountedRef.current = true;
    void refresh();

    return () => {
      isMountedRef.current = false;
      if (flushTimerRef.current !== null) {
        clearTimeout(flushTimerRef.current);
        flushTimerRef.current = null;
      }
      if (rafHandleRef.current !== null) {
        cancelAnimationFrame(rafHandleRef.current);
        rafHandleRef.current = null;
      }
      pendingLogsMapRef.current.clear();
    };
  }, [refresh]);

  // Подписка на IPC события игрового движка
  useEffect(() => {
    const unsubscribe = subscribeBackendEvents((ev) => {
      if (!isMountedRef.current) return;

      try {
        if (ev.name === "game.status" || ev.name === "game.exited") {
          if (ev.payload && typeof ev.payload === "object" && "instance_id" in ev.payload) {
            const sess = ev.payload as GameSessionInfo;
            if (!sess.instance_id) return;

            setSessionsByInstance((prev) => {
              const current = prev[sess.instance_id];
              if (
                current &&
                current.id === sess.id &&
                current.state === sess.state &&
                current.pid === sess.pid &&
                current.stage_message === sess.stage_message &&
                current.error === sess.error &&
                current.exit_code === sess.exit_code
              ) {
                return prev;
              }
              return { ...prev, [sess.instance_id]: sess };
            });

            if (ev.name === "game.exited" && sess.state === "error" && sess.exit_reason) {
              pushToast(sess.exit_reason, "err");
            } else if (ev.name === "game.status" && sess.state === "error" && sess.error) {
              pushToast(sess.error, "err");
            }
          }
        } else if (ev.name === "game.log") {
          const batch = ev.payload as { instance_id?: string; lines?: GameLogLine[] } | undefined;
          if (batch?.instance_id && Array.isArray(batch.lines) && batch.lines.length > 0) {
            const instId = batch.instance_id;
            const existing = pendingLogsMapRef.current.get(instId) || [];
            pendingLogsMapRef.current.set(instId, existing.concat(batch.lines));
            scheduleFlushLogs();
          }
        } else if (ev.name === "game.crash_detected") {
          const info = ev.payload as { instance_id?: string } | undefined;
          if (info?.instance_id) {
            pushToast("Обнаружен аварийный отчёт Minecraft — откройте вкладку «Консоль» для диагностики", "err");
          }
        }
      } catch (err) {
        console.error("[useLauncher] Ошибка обработки IPC события:", err);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [scheduleFlushLogs]);

  // Фоновый опрос только если запущен сторонний браузерный режим без Wails IPC
  const hasActiveGame = useMemo(() => {
    return Object.values(sessionsByInstance).some((s) => isSessionActive(s.state));
  }, [sessionsByInstance]);

  useEffect(() => {
    if (isWailsDesktop() || !hasActiveGame) return;
    const tid = window.setInterval(() => {
      void refresh();
    }, 1200);
    return () => window.clearInterval(tid);
  }, [hasActiveGame, refresh]);

  /**
   * Загрузка архивных строк лога инстанса из бэкенда.
   */
  const loadInstanceLogs = useCallback(async (instanceId: string) => {
    if (!instanceId) return;
    try {
      const lines = await api.getInstanceLogs(instanceId, MAX_UI_LOG_LINES);
      if (!isMountedRef.current) return;
      setLogsByInstance((prev) => ({
        ...prev,
        [instanceId]: Array.isArray(lines) ? lines : [],
      }));
    } catch (err) {
      console.warn(`[useLauncher] Не удалось загрузить логи для ${instanceId}:`, err);
    }
  }, []);

  /**
   * Полная очистка текущего буфера отображения логов.
   */
  const clearDisplayLogs = useCallback((instanceId: string) => {
    if (!instanceId) return;
    pendingLogsMapRef.current.delete(instanceId);
    setLogsByInstance((prev) => {
      if (!prev[instanceId] || prev[instanceId].length === 0) return prev;
      return { ...prev, [instanceId]: [] };
    });
  }, []);

  /**
   * Запуск игры с немедленной валидацией и предварительной очисткой экрана консоли.
   */
  const launch = useCallback(async (req: LaunchInstanceInput): Promise<GameSessionInfo> => {
    if (!req.instance_id) {
      throw new Error("Не передан идентификатор игрового профиля для запуска");
    }
    // Очищаем старые логи перед новым стартом
    pendingLogsMapRef.current.delete(req.instance_id);
    setLogsByInstance((prev) => ({ ...prev, [req.instance_id]: [] }));

    const sess = await api.launchInstance(req);
    if (isMountedRef.current) {
      setSessionsByInstance((prev) => ({ ...prev, [req.instance_id]: sess }));
    }
    return sess;
  }, []);

  /**
   * Остановка процесса (плавная или принудительная) с защитным таймаутом.
   */
  const stop = useCallback(async (instanceId: string, force = false) => {
    if (!instanceId) return;
    await api.stopInstance(instanceId, force);
    pushToast(force ? "Принудительная остановка Minecraft..." : "Остановка Minecraft...", "info");
  }, []);

  /**
   * Количество активных игровых процессов (мемоизировано).
   */
  const runningCount = useMemo(() => {
    return Object.values(sessionsByInstance).filter((s) => isSessionActive(s.state)).length;
  }, [sessionsByInstance]);

  return {
    sessionsByInstance,
    logsByInstance,
    systemMem,
    runningCount,
    refresh,
    launch,
    stop,
    loadInstanceLogs,
    clearDisplayLogs,
  };
}
