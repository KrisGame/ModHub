import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { isWailsDesktop, subscribeBackendEvents } from "../services/bridge";
import { pushToast } from "../toastBus";
import type { ModpackImportProgress, ModpackView } from "../types";

export function useModpacks() {
  const [modpacks, setModpacks] = useState<ModpackView[]>([]);
  const [loading, setLoading] = useState(true);
  const [importProgress, setImportProgress] = useState<ModpackImportProgress | null>(null);

  const refresh = useCallback(async () => {
    try {
      const [list, prog] = await Promise.all([api.listModpacks(), api.getActiveModpackProgress()]);
      setModpacks(list);
      setImportProgress(prog);
    } catch {
      // ignore transient errors
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => { void refresh(); }, [refresh]);

  useEffect(() => {
    return subscribeBackendEvents((ev) => {
      if (ev.name === "modpack.import.progress" && ev.payload && typeof ev.payload === "object") {
        setImportProgress(ev.payload as ModpackImportProgress);
      } else if (ev.name === "modpack.import.completed" && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as ModpackImportProgress;
        setImportProgress(p);
        pushToast(p.message || "Импорт сборки завершён", p.stage === "partial" ? "err" : "ok");
        void refresh();
      } else if (ev.name === "modpack.import.error" && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as ModpackImportProgress;
        setImportProgress(p);
        if (p.error) pushToast(p.error, "err");
        void refresh();
      } else if (ev.name === "modpack.updated" || ev.name === "modpack.deleted") {
        void refresh();
      }
    });
  }, [refresh]);

  const isImporting = Boolean(importProgress && importProgress.stage !== "completed" && importProgress.stage !== "partial" && importProgress.stage !== "error");
  useEffect(() => {
    if (isWailsDesktop() || !isImporting) return;
    const tid = window.setInterval(() => { void refresh(); }, 600);
    return () => window.clearInterval(tid);
  }, [isImporting, refresh]);

  const deleteRecord = useCallback(async (id: string) => {
    await api.deleteModpackRecord(id);
    setModpacks((prev) => prev.filter((m) => m.id !== id));
    pushToast("Запись сборки удалена (игровой профиль сохранён)", "info");
  }, []);

  return { modpacks, loading, importProgress, isImporting, refresh, deleteRecord };
}
