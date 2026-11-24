import { useCallback, useEffect, useState } from "react";
import { api } from "../api";
import { subscribeBackendEvents } from "../services/bridge";
import { pushToast } from "../toastBus";
import type {
  CreateInstanceInput,
  DeleteInstanceInput,
  DuplicateInstanceInput,
  GameInstance,
  InstallProgressInfo,
  InstanceIntegrityReport,
  SharedStoragePaths,
  UpdateInstanceInput,
} from "../types";

function upsertInst(list: GameInstance[], nextItem: GameInstance): GameInstance[] {
  const idx = list.findIndex((x) => x.id === nextItem.id);
  if (idx >= 0) {
    const cp = list.slice();
    cp[idx] = nextItem;
    return cp;
  }
  return [nextItem, ...list];
}

export function useInstances() {
  const [instances, setInstances] = useState<GameInstance[]>([]);
  const [paths, setPaths] = useState<SharedStoragePaths | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeInstallsById, setActiveInstallsById] = useState<Record<string, InstallProgressInfo>>({});

  const refresh = useCallback(async () => {
    try {
      const snap = await api.getInstances();
      setInstances(snap.instances || []);
      if (snap.paths) setPaths(snap.paths);
    } catch {
      // Ignore transient startup errors
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    return subscribeBackendEvents((ev) => {
      if (
        (ev.name === "instance.created" || ev.name === "instance.updated" || ev.name === "instance.status") &&
        ev.payload &&
        typeof ev.payload === "object" &&
        "id" in ev.payload
      ) {
        setInstances((prev) => upsertInst(prev, ev.payload as GameInstance));
      } else if (ev.name === "instance.deleted" && ev.payload && typeof ev.payload === "object") {
        const rec = ev.payload as { id?: string };
        if (rec.id) {
          setInstances((prev) => prev.filter((x) => x.id !== rec.id));
        }
      } else if (ev.name === "install.progress" && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as InstallProgressInfo;
        if (p.instance_id) {
          setActiveInstallsById((prev) => ({ ...prev, [p.instance_id!]: p }));
        }
      } else if (ev.name === "install.completed" && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as InstallProgressInfo;
        if (p.instance_id) {
          setActiveInstallsById((prev) => {
            if (!(p.instance_id! in prev)) return prev;
            const next = { ...prev };
            delete next[p.instance_id!];
            return next;
          });
          void api
            .getInstance(p.instance_id)
            .then((updated) => {
              setInstances((prev) => upsertInst(prev, updated));
            })
            .catch(() => void refresh());
        }
      } else if ((ev.name === "install.error" || ev.name === "install.cancelled") && ev.payload && typeof ev.payload === "object") {
        const p = ev.payload as InstallProgressInfo;
        if (p.instance_id) {
          setActiveInstallsById((prev) => {
            if (!(p.instance_id! in prev)) return prev;
            const next = { ...prev };
            delete next[p.instance_id!];
            return next;
          });
          if (ev.name === "install.error" && p.error) {
            pushToast(`Подготовка профиля: ${p.error}`, "err");
          }
          void api
            .getInstance(p.instance_id)
            .then((updated) => {
              setInstances((prev) => upsertInst(prev, updated));
            })
            .catch(() => void refresh());
        }
      } else if (ev.name === "versions.updated" && ev.payload && typeof ev.payload === "object") {
        const rec = ev.payload as { instance_id?: string };
        if (rec.instance_id) {
          void api
            .getInstance(rec.instance_id)
            .then((updated) => {
              setInstances((prev) => upsertInst(prev, updated));
            })
            .catch(() => void refresh());
        }
      }
    });
  }, [refresh]);

  const create = useCallback(async (req: CreateInstanceInput): Promise<GameInstance> => {
    const created = await api.createInstance(req);
    setInstances((prev) => upsertInst(prev, created));
    if (created.mc_version && created.instance_type !== "empty") {
      setActiveInstallsById((prev) => ({
        ...prev,
        [created.id]: {
          instance_id: created.id,
          mc_version: created.mc_version,
          loader: created.instance_type,
          loader_version: created.loader_version || "",
          stage: "metadata",
          message: "Подготовка загрузки компонентов Minecraft (без Java)...",
          completed_files: 0,
          total_files: 0,
          progress: -1,
          speed_bps: 0,
          updated_at: Math.floor(Date.now() / 1000),
        },
      }));
      pushToast(`Профиль «${created.name}» создан — скачиваются компоненты Minecraft (без Java)`, "ok");
    } else {
      pushToast(`Создан профиль «${created.name}»`, "ok");
    }
    return created;
  }, []);

  const update = useCallback(async (req: UpdateInstanceInput): Promise<GameInstance> => {
    const updated = await api.updateInstance(req);
    setInstances((prev) => upsertInst(prev, updated));
    pushToast(`Профиль «${updated.name}» сохранён`, "ok");
    return updated;
  }, []);

  const duplicate = useCallback(async (req: DuplicateInstanceInput): Promise<GameInstance> => {
    const dup = await api.duplicateInstance(req);
    setInstances((prev) => upsertInst(prev, dup));
    pushToast(`Создана независимая копия «${dup.name}»`, "ok");
    return dup;
  }, []);

  const remove = useCallback(async (req: DeleteInstanceInput): Promise<void> => {
    await api.deleteInstance(req);
    setInstances((prev) => prev.filter((x) => x.id !== req.id));
    pushToast(req.delete_files ? "Профиль и его файлы удалены" : "Запись профиля удалена (файлы сохранены)", "info");
  }, []);

  const verify = useCallback(async (id: string): Promise<InstanceIntegrityReport> => {
    const rep = await api.verifyInstance(id);
    const updated = await api.getInstance(id).catch(() => null);
    if (updated) setInstances((prev) => upsertInst(prev, updated));
    return rep;
  }, []);

  const repair = useCallback(async (id: string): Promise<InstanceIntegrityReport> => {
    const rep = await api.repairInstance(id);
    const updated = await api.getInstance(id).catch(() => null);
    if (updated) setInstances((prev) => upsertInst(prev, updated));
    pushToast("Структура директорий и instance.json проверены и восстановлены", "ok");
    return rep;
  }, []);

  return {
    instances,
    paths,
    loading,
    activeInstallsById,
    refresh,
    create,
    update,
    duplicate,
    remove,
    verify,
    repair,
  };
}
