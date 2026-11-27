import React, { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import type { AppSection } from "../components/AppNavRail";
import { CustomSelect, type CustomSelectOption } from "../components/CustomSelect";
import { INSTANCE_ICON_PRESETS, InstanceIcon } from "../components/InstanceIcon";
import { InstanceConsoleTab, processStateMeta } from "../components/InstanceConsoleTab";
import { CreateInstanceModal, DeleteInstanceModal, DuplicateInstanceModal } from "../components/InstanceModals";
import { ImportModpackModal } from "../components/ImportModpackModal";
import { ExportModpackModal } from "../components/ExportModpackModal";
import { JavaSettingsSection } from "../components/JavaSettingsSection";
import { isSessionActive } from "../hooks/useLauncher";
import { desktopBridge, subscribeBackendEvents } from "../services/bridge";
import type { BackendEventMessage } from "../services/bridge";
import type {
  AccountView,
  CreateInstanceInput,
  DeleteInstanceInput,
  DuplicateInstanceInput,
  GameInstance,
  GameLogLine,
  GameSessionInfo,
  InstallProgressInfo,
  InstanceIntegrityReport,
  InstanceJavaMode,
  InstanceStatus,
  InstanceType,
  JavaRuntimeInfo,
  LaunchInstanceInput,
  SharedStoragePaths,
  SystemMemoryInfo,
  UpdateInstanceInput,
  InstalledModRef,
  ModUpdateInfo,
  ResourcePackRef,
  ShaderPackRef,
  WorldRef,
} from "../types";
import { formatBytes, formatDate, formatLoaderName } from "../utils";

type DetailTab = "mods" | "resourcepacks" | "shaders" | "worlds" | "settings" | "console";
type SortBy = "updated" | "name" | "created" | "mc_version";

interface InstancesPageProps {
  instances: GameInstance[];
  paths: SharedStoragePaths | null;
  loading: boolean;
  mcVersions: string[];
  accounts: AccountView[];
  activeAccount: AccountView | null;
  sessionsByInstance: Record<string, GameSessionInfo>;
  logsByInstance: Record<string, GameLogLine[]>;
  systemMem: SystemMemoryInfo;
  onCreate: (req: CreateInstanceInput) => Promise<GameInstance>;
  onUpdate: (req: UpdateInstanceInput) => Promise<GameInstance>;
  onDuplicate: (req: DuplicateInstanceInput) => Promise<GameInstance>;
  onDelete: (req: DeleteInstanceInput) => Promise<void>;
  onRefresh: () => Promise<void>;
  onVerify: (id: string) => Promise<InstanceIntegrityReport>;
  onRepair: (id: string) => Promise<InstanceIntegrityReport>;
  onLaunch: (req: LaunchInstanceInput) => Promise<GameSessionInfo>;
  onStop: (instanceId: string, force: boolean) => Promise<void>;
  onLoadLogs: (instanceId: string) => Promise<void>;
  onClearDisplayLogs: (instanceId: string) => void;
  onNavigate: (section: AppSection) => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
  modInstallTarget?: { id: string; name: string } | null;
  onArmModInstall: (inst: GameInstance) => void;
  onClearModInstall: () => void;
  requestedInstanceSelect?: { id: string; nonce: number } | null;
  activeInstallsById?: Record<string, InstallProgressInfo>;
}

const SORT_OPTS: CustomSelectOption[] = [
  { value: "updated", label: "По изменению" },
  { value: "name", label: "По названию" },
  { value: "created", label: "По дате создания" },
  { value: "mc_version", label: "По версии MC" },
];

const TYPE_OPTS: CustomSelectOption[] = [
  { value: "empty", label: "Пустой профиль" },
  { value: "vanilla", label: "Vanilla" },
  { value: "neoforge", label: "NeoForge" },
  { value: "fabric", label: "Fabric" },
  { value: "forge", label: "Forge" },
  { value: "quilt", label: "Quilt" },
];

const JAVA_MODE_OPTS: CustomSelectOption[] = [
  { value: "auto", label: "Автоопределение по версии MC" },
  { value: "managed", label: "Выбрать из обнаруженных / установленных Java" },
  { value: "custom", label: "Указать путь к java.exe вручную" },
];

function statusBadge(st: InstanceStatus): { label: string; cls: string } {
  switch (st) {
    case "ready":
      return { label: "Готов к запуску", cls: "is-ok" };
    case "needs_setup":
      return { label: "Требует установки", cls: "is-paused" };
    case "corrupted":
      return { label: "Повреждён", cls: "is-err" };
    case "creating":
      return { label: "Создаётся", cls: "is-active" };
    case "deleting":
      return { label: "Удаляется", cls: "is-muted" };
    default:
      return { label: st, cls: "is-muted" };
  }
}

function typeLabel(t: InstanceType): string {
  switch (t) {
    case "vanilla":
      return "Vanilla";
    case "empty":
      return "Пустой";
    default:
      return formatLoaderName(t);
  }
}

export const InstancesPage: React.FC<InstancesPageProps> = ({
  instances,
  paths,
  loading,
  mcVersions,
  accounts,
  activeAccount,
  sessionsByInstance,
  logsByInstance,
  systemMem,
  onCreate,
  onUpdate,
  onDuplicate,
  onDelete,
  onRefresh,
  onVerify,
  onRepair,
  onLaunch,
  onStop,
  onLoadLogs,
  onClearDisplayLogs,
  onNavigate,
  onToast,
  modInstallTarget = null,
  onArmModInstall,
  onClearModInstall,
  requestedInstanceSelect = null,
  activeInstallsById = {},
}) => {
  const [selectedId, setSelectedId] = useState<string>("");
  const [detailTab, setDetailTab] = useState<DetailTab>("mods");
  const [search, setSearch] = useState("");
  const [sortBy, setSortBy] = useState<SortBy>("updated");

  useEffect(() => {
    if (requestedInstanceSelect?.id) {
      setSelectedId(requestedInstanceSelect.id);
      setDetailTab("mods");
    }
  }, [requestedInstanceSelect?.id, requestedInstanceSelect?.nonce]);

  // Защита от утечек при unmount
  const isMountedRef = useRef(true);
  useEffect(() => {
    isMountedRef.current = true;
    return () => {
      isMountedRef.current = false;
    };
  }, []);

  // Синхронизированный ref активного инстанса для отсечения stale-ответов
  const selectedIdRef = useRef(selectedId);
  selectedIdRef.current = selectedId;

  // Монотонный счетчик очереди запросов для предотвращения асинхронных гонок профилей
  const modsQuerySeqRef = useRef(0);
  const rpQuerySeqRef = useRef(0);
  const shadersQuerySeqRef = useRef(0);
  const worldsQuerySeqRef = useRef(0);

  // Mods state
  const [mods, setMods] = useState<InstalledModRef[]>([]);
  const [modsSearch, setModsSearch] = useState("");
  const [modsLoading, setModsLoading] = useState(false);
  const [busyMods, setBusyMods] = useState<Set<string>>(() => new Set());
  const [modUpdates, setModUpdates] = useState<Record<string, ModUpdateInfo>>({});
  const [checkingUpdates, setCheckingUpdates] = useState(false);
  const [busyUpdatingMods, setBusyUpdatingMods] = useState<Set<string>>(() => new Set());
  const [selectedMods, setSelectedMods] = useState<Set<string>>(() => new Set());

  // Resource Packs state
  const [resourcePacks, setResourcePacks] = useState<ResourcePackRef[]>([]);
  const [rpSearch, setRpSearch] = useState("");
  const [rpLoading, setRpLoading] = useState(false);
  const [busyRp, setBusyRp] = useState<Set<string>>(() => new Set());

  // Shader Packs state
  const [shaders, setShaders] = useState<ShaderPackRef[]>([]);
  const [shadersSearch, setShadersSearch] = useState("");
  const [shadersLoading, setShadersLoading] = useState(false);
  const [busyShaders, setBusyShaders] = useState<Set<string>>(() => new Set());

  // Worlds / Saves state
  const [worlds, setWorlds] = useState<WorldRef[]>([]);
  const [worldsSearch, setWorldsSearch] = useState("");
  const [worldsLoading, setWorldsLoading] = useState(false);
  const [exportingWorld, setExportingWorld] = useState<string | null>(null);

  // Modals state
  const [createOpen, setCreateOpen] = useState(false);
  const [importOpen, setImportOpen] = useState(false);
  const [exportTarget, setExportTarget] = useState<GameInstance | null>(null);
  useEffect(() => {
    setExportTarget((target) => target?.id === selectedId ? target : null);
  }, [selectedId]);
  const [dupTarget, setDupTarget] = useState<GameInstance | null>(null);
  const [delTarget, setDelTarget] = useState<GameInstance | null>(null);

  // Detail tab states
  const [verifying, setVerifying] = useState(false);
  const [repairing, setRepairing] = useState(false);
  const [javaList, setJavaList] = useState<JavaRuntimeInfo[]>([]);
  const [scanningJava, setScanningJava] = useState(false);
  const [showJavaRuntimeMgr, setShowJavaRuntimeMgr] = useState(false);

  // Settings editor state
  const [editName, setEditName] = useState("");
  const [editDesc, setEditDesc] = useState("");
  const [editIcon, setEditIcon] = useState("grass");
  const [editMc, setEditMc] = useState("");
  const [editType, setEditType] = useState<InstanceType>("vanilla");
  const [editLoaderVer, setEditLoaderVer] = useState("");
  const [editMemMin, setEditMemMin] = useState(512);
  const [editMemMax, setEditMemMax] = useState(4096);
  const [editJavaMode, setEditJavaMode] = useState<InstanceJavaMode>("auto");
  const [editJavaId, setEditJavaId] = useState("");
  const [editJavaPath, setEditJavaPath] = useState("");
  const [editJvmArgs, setEditJvmArgs] = useState("");
  const [editWinW, setEditWinW] = useState(1280);
  const [editWinH, setEditWinH] = useState(720);
  const [editFullscreen, setEditFullscreen] = useState(false);
  const [editAccountId, setEditAccountId] = useState("");
  const [savingSettings, setSavingSettings] = useState(false);

  const selected = useMemo(
    () => instances.find((i) => i.id === selectedId) || null,
    [instances, selectedId]
  );

  // Полная изоляция и сброс локального состояния при переключении профиля (Anti-Race Condition)
  useEffect(() => {
    setSelectedMods(new Set());
    setModUpdates({});
    setBusyMods(new Set());
    setBusyUpdatingMods(new Set());
    setBusyRp(new Set());
    setBusyShaders(new Set());
    setModsSearch("");
    setRpSearch("");
    setShadersSearch("");
    setWorldsSearch("");
  }, [selectedId]);

  // Защищенная загрузка модов с отсечением устаревших асинхронных ответов
  const refreshMods = async (instanceId: string) => {
    if (!instanceId) return;
    const seq = ++modsQuerySeqRef.current;
    setModsLoading(true);
    try {
      const res = await api.listInstanceMods(instanceId);
      if (!isMountedRef.current || modsQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setMods(Array.isArray(res) ? res : []);
    } catch (err) {
      if (!isMountedRef.current || modsQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setMods([]);
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current && modsQuerySeqRef.current === seq) {
        setModsLoading(false);
      }
    }
  };

  // Защищенная загрузка ресурспаков
  const refreshResourcePacks = async (instanceId: string) => {
    if (!instanceId) return;
    const seq = ++rpQuerySeqRef.current;
    setRpLoading(true);
    try {
      const res = await api.listInstanceResourcePacks(instanceId);
      if (!isMountedRef.current || rpQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setResourcePacks(Array.isArray(res) ? res : []);
    } catch (err) {
      if (!isMountedRef.current || rpQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setResourcePacks([]);
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current && rpQuerySeqRef.current === seq) {
        setRpLoading(false);
      }
    }
  };

  // Защищенная загрузка шейдеров
  const refreshShaders = async (instanceId: string) => {
    if (!instanceId) return;
    const seq = ++shadersQuerySeqRef.current;
    setShadersLoading(true);
    try {
      const res = await api.listInstanceShaderPacks(instanceId);
      if (!isMountedRef.current || shadersQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setShaders(Array.isArray(res) ? res : []);
    } catch (err) {
      if (!isMountedRef.current || shadersQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setShaders([]);
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current && shadersQuerySeqRef.current === seq) {
        setShadersLoading(false);
      }
    }
  };

  // Защищенная загрузка сохранений / миров
  const refreshWorlds = async (instanceId: string) => {
    if (!instanceId) return;
    const seq = ++worldsQuerySeqRef.current;
    setWorldsLoading(true);
    try {
      const res = await api.listInstanceWorlds(instanceId);
      if (!isMountedRef.current || worldsQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setWorlds(Array.isArray(res) ? res : []);
    } catch (err) {
      if (!isMountedRef.current || worldsQuerySeqRef.current !== seq || selectedIdRef.current !== instanceId) {
        return;
      }
      setWorlds([]);
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current && worldsQuerySeqRef.current === seq) {
        setWorldsLoading(false);
      }
    }
  };

  useEffect(() => {
    if (!selected) return;
    const targetId = selected.id;

    if (detailTab === "mods") {
      void refreshMods(targetId);
      return subscribeBackendEvents((ev: BackendEventMessage) => {
        if (!isMountedRef.current || selectedIdRef.current !== targetId) return;
        if (ev.name === "instance.updated" || ev.name === "install.completed") {
          const p = ev.payload as { id?: unknown; instance_id?: unknown } | undefined;
          if (p?.id === targetId || p?.instance_id === targetId) {
            void refreshMods(targetId);
          }
          return;
        }
        if (ev.name !== "download.completed" && ev.name !== "download.error") return;
        const payload = ev.payload as { group_id?: unknown; error?: unknown } | undefined;
        if (
          typeof payload?.group_id !== "string" ||
          (!payload.group_id.startsWith(`modinstall:${targetId}`) &&
            !payload.group_id.startsWith(`modupdate:${targetId}`) &&
            !payload.group_id.startsWith(`modpack:${targetId}`))
        ) {
          return;
        }
        void (async () => {
          await refreshMods(targetId);
          await onRefresh();
          if (isMountedRef.current && selectedIdRef.current === targetId) {
            onToast(
              ev.name === "download.completed" ? "Мод установлен/обновлён в профиле" : String(payload.error || "Ошибка загрузки мода"),
              ev.name === "download.completed" ? "ok" : "err"
            );
          }
        })();
      });
    }

    if (detailTab === "resourcepacks") {
      void refreshResourcePacks(targetId);
      return subscribeBackendEvents((ev: BackendEventMessage) => {
        if (!isMountedRef.current || selectedIdRef.current !== targetId) return;
        if (ev.name === "instance.updated") {
          const p = ev.payload as { id?: unknown } | undefined;
          if (p?.id === targetId) void refreshResourcePacks(targetId);
          return;
        }
        if (ev.name !== "download.completed" && ev.name !== "download.error") return;
        const payload = ev.payload as { group_id?: unknown; error?: unknown } | undefined;
        if (typeof payload?.group_id !== "string" || !payload.group_id.startsWith(`resourcepackinstall:${targetId}`)) return;
        void (async () => {
          await refreshResourcePacks(targetId);
          if (isMountedRef.current && selectedIdRef.current === targetId) {
            onToast(
              ev.name === "download.completed" ? "Ресурспак установлен в профиль" : String(payload.error || "Ошибка загрузки"),
              ev.name === "download.completed" ? "ok" : "err"
            );
          }
        })();
      });
    }

    if (detailTab === "shaders") {
      void refreshShaders(targetId);
      return subscribeBackendEvents((ev: BackendEventMessage) => {
        if (!isMountedRef.current || selectedIdRef.current !== targetId) return;
        if (ev.name === "instance.updated") {
          const p = ev.payload as { id?: unknown } | undefined;
          if (p?.id === targetId) void refreshShaders(targetId);
          return;
        }
        if (ev.name !== "download.completed" && ev.name !== "download.error") return;
        const payload = ev.payload as { group_id?: unknown; error?: unknown } | undefined;
        if (typeof payload?.group_id !== "string" || !payload.group_id.startsWith(`shaderinstall:${targetId}`)) return;
        void (async () => {
          await refreshShaders(targetId);
          if (isMountedRef.current && selectedIdRef.current === targetId) {
            onToast(
              ev.name === "download.completed" ? "Шейдерпак установлен в профиль" : String(payload.error || "Ошибка загрузки"),
              ev.name === "download.completed" ? "ok" : "err"
            );
          }
        })();
      });
    }

    if (detailTab === "worlds") {
      void refreshWorlds(targetId);
    }
  }, [detailTab, selected?.id, onRefresh, onToast]);

  const filteredMods = useMemo(() => {
    const list = Array.isArray(mods) ? mods : [];
    const q = modsSearch.trim().toLocaleLowerCase();
    return q ? list.filter((mod) => (mod?.filename || "").toLocaleLowerCase().includes(q)) : list;
  }, [mods, modsSearch]);

  const availableUpdatesCount = useMemo(() => {
    return Object.values(modUpdates).filter((u) => u.has_update && u.new_download_url).length;
  }, [modUpdates]);

  const handleCheckModUpdates = async () => {
    if (!selected) return;
    const targetId = selected.id;
    setCheckingUpdates(true);
    try {
      const updates = await api.checkInstanceModUpdates(targetId);
      if (!isMountedRef.current || selectedIdRef.current !== targetId) return;

      const map: Record<string, ModUpdateInfo> = {};
      let found = 0;
      for (const u of updates || []) {
        map[u.current_filename] = u;
        if (u.has_update && u.new_download_url) found++;
      }
      setModUpdates(map);
      if (found > 0) {
        onToast(`Найдено обновлений: ${found}`, "ok");
      } else {
        onToast("Все установленные моды актуальны", "info");
      }
    } catch (err) {
      if (isMountedRef.current && selectedIdRef.current === targetId) {
        onToast(err instanceof Error ? err.message : String(err), "err");
      }
    } finally {
      if (isMountedRef.current) {
        setCheckingUpdates(false);
      }
    }
  };

  const handleApplyModUpdate = async (mod: InstalledModRef) => {
    if (!selected) return;
    const targetId = selected.id;
    const info = modUpdates[mod.filename];
    if (!info || !info.new_download_url || !info.new_filename) {
      onToast("Для данного мода нет ссылки на обновление", "err");
      return;
    }
    setBusyUpdatingMods((cur) => new Set(cur).add(mod.filename));
    try {
      await api.applyInstanceModUpdate({
        id: targetId,
        current_filename: mod.filename,
        new_filename: info.new_filename,
        new_url: info.new_download_url,
        new_size: info.new_size,
        new_sha1: info.new_sha1,
      });
      onToast(`Обновление «${info.project_title || info.new_filename}» поставлено в загрузку`, "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setBusyUpdatingMods((cur) => {
          const next = new Set(cur);
          next.delete(mod.filename);
          return next;
        });
      }
    }
  };

  const handleApplyAllModUpdates = async () => {
    if (!selected) return;
    const targetId = selected.id;
    const candidates = Object.values(modUpdates).filter((u) => u.has_update && u.new_download_url && u.new_filename);
    if (candidates.length === 0) return;
    try {
      for (const c of candidates) {
        await api.applyInstanceModUpdate({
          id: targetId,
          current_filename: c.current_filename,
          new_filename: c.new_filename!,
          new_url: c.new_download_url!,
          new_size: c.new_size,
          new_sha1: c.new_sha1,
        });
      }
      onToast(`Поставлено в загрузку обновлений: ${candidates.length}`, "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    }
  };

  const handleSetAllMods = async (enabled: boolean) => {
    if (!selected) return;
    const targetId = selected.id;
    setModsLoading(true);
    try {
      const res = await api.setAllInstanceModsEnabled(targetId, enabled);
      if (isMountedRef.current && selectedIdRef.current === targetId) {
        setMods(Array.isArray(res) ? res : []);
      }
      await onRefresh();
      onToast(enabled ? "Все моды включены" : "Все моды отключены", "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setModsLoading(false);
      }
    }
  };

  const handleDeleteSelectedMods = async () => {
    if (!selected || selectedMods.size === 0) return;
    const targetId = selected.id;
    if (!window.confirm(`Удалить выбранные моды (${selectedMods.size} шт.)?`)) return;
    setModsLoading(true);
    try {
      const res = await api.deleteMultipleInstanceMods(targetId, Array.from(selectedMods));
      if (isMountedRef.current && selectedIdRef.current === targetId) {
        setMods(Array.isArray(res) ? res : []);
        setSelectedMods(new Set());
      }
      await onRefresh();
      onToast("Выбранные моды удалены", "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setModsLoading(false);
      }
    }
  };

  const toggleModSelected = (filename: string) => {
    setSelectedMods((cur) => {
      const next = new Set(cur);
      if (next.has(filename)) {
        next.delete(filename);
      } else {
        next.add(filename);
      }
      return next;
    });
  };

  const toggleSelectAllMods = () => {
    if (selectedMods.size === filteredMods.length && filteredMods.length > 0) {
      setSelectedMods(new Set());
    } else {
      setSelectedMods(new Set(filteredMods.map((m) => m.filename)));
    }
  };

  const runModAction = async (filename: string, action: () => Promise<InstalledModRef[]>, success: string) => {
    if (!selected) return;
    const targetId = selected.id;
    setBusyMods((cur) => new Set(cur).add(filename));
    try {
      const res = await action();
      if (isMountedRef.current && selectedIdRef.current === targetId) {
        setMods(Array.isArray(res) ? res : []);
      }
      await onRefresh();
      onToast(success, "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setBusyMods((cur) => {
          const next = new Set(cur);
          next.delete(filename);
          return next;
        });
      }
    }
  };

  const filteredResourcePacks = useMemo(() => {
    const list = Array.isArray(resourcePacks) ? resourcePacks : [];
    const q = rpSearch.trim().toLocaleLowerCase();
    return q ? list.filter((p) => (p.name || p.filename).toLocaleLowerCase().includes(q)) : list;
  }, [resourcePacks, rpSearch]);

  const runRpAction = async (filename: string, action: () => Promise<ResourcePackRef[]>, success: string) => {
    if (!selected) return;
    const targetId = selected.id;
    setBusyRp((cur) => new Set(cur).add(filename));
    try {
      const res = await action();
      if (isMountedRef.current && selectedIdRef.current === targetId) {
        setResourcePacks(Array.isArray(res) ? res : []);
      }
      onToast(success, "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setBusyRp((cur) => {
          const next = new Set(cur);
          next.delete(filename);
          return next;
        });
      }
    }
  };

  const filteredShaders = useMemo(() => {
    const list = Array.isArray(shaders) ? shaders : [];
    const q = shadersSearch.trim().toLocaleLowerCase();
    return q ? list.filter((s) => (s.name || s.filename).toLocaleLowerCase().includes(q)) : list;
  }, [shaders, shadersSearch]);

  const runShaderAction = async (filename: string, action: () => Promise<ShaderPackRef[]>, success: string) => {
    if (!selected) return;
    const targetId = selected.id;
    setBusyShaders((cur) => new Set(cur).add(filename));
    try {
      const res = await action();
      if (isMountedRef.current && selectedIdRef.current === targetId) {
        setShaders(Array.isArray(res) ? res : []);
      }
      onToast(success, "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setBusyShaders((cur) => {
          const next = new Set(cur);
          next.delete(filename);
          return next;
        });
      }
    }
  };

  const filteredWorlds = useMemo(() => {
    const list = Array.isArray(worlds) ? worlds : [];
    const q = worldsSearch.trim().toLocaleLowerCase();
    return q ? list.filter((w) => (w.name || w.folder).toLocaleLowerCase().includes(q)) : list;
  }, [worlds, worldsSearch]);

  const handleExportWorld = async (folder: string) => {
    if (!selected) return;
    setExportingWorld(folder);
    try {
      const res = await api.exportInstanceWorld(selected.id, folder);
      onToast(`Бэкап мира сохранён: «${res.filename}» (${formatBytes(res.size)})`, "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setExportingWorld(null);
      }
    }
  };

  // Очистка выделения при удалении профиля
  useEffect(() => {
    if (selectedId && !selected) {
      setSelectedId("");
    }
  }, [selectedId, selected]);

  // Синхронизация формы настроек при выборе профиля
  useEffect(() => {
    if (!selected) return;
    setEditName(selected.name);
    setEditDesc(selected.description || "");
    setEditIcon(selected.icon || "grass");
    setEditMc(selected.mc_version || "");
    setEditType(selected.instance_type || "empty");
    setEditLoaderVer(selected.loader_version || "");
    setEditMemMin(selected.memory_min_mb || 512);
    setEditMemMax(selected.memory_max_mb || 4096);
    setEditJavaMode(selected.java_mode || "auto");
    setEditJavaId(selected.java_runtime_id || "");
    setEditJavaPath(selected.java_path || "");
    setEditJvmArgs(selected.launch_settings?.jvm_args || "");
    setEditWinW(selected.launch_settings?.window_width || 1280);
    setEditWinH(selected.launch_settings?.window_height || 720);
    setEditFullscreen(Boolean(selected.launch_settings?.fullscreen));
    setEditAccountId(selected.launch_settings?.account_id || "");
  }, [selected?.id, selected?.updated_at]);

  // Загрузка доступных Java Runtimes + автообновление при установке/удалении Java
  useEffect(() => {
    if (!selected || detailTab !== "settings") return;
    api
      .getJavaRuntimes(false)
      .then((snap) => {
        if (isMountedRef.current) {
          setJavaList(snap.runtimes || []);
        }
      })
      .catch(() => {});
    return subscribeBackendEvents((ev: BackendEventMessage) => {
      if (!isMountedRef.current) return;
      if (ev.name === "java.updated" && ev.payload && typeof ev.payload === "object") {
        const snap = ev.payload as { runtimes?: JavaRuntimeInfo[] };
        if (Array.isArray(snap.runtimes)) {
          setJavaList(snap.runtimes);
        }
      }
    });
  }, [selected?.id, detailTab]);

  const filteredInstances = useMemo(() => {
    const q = search.trim().toLowerCase();
    const list = instances.filter((it) => {
      if (!q) return true;
      const hay = `${it.name} ${it.description} ${it.mc_version} ${it.instance_type}`.toLowerCase();
      return hay.includes(q);
    });
    return [...list].sort((a, b) => {
      switch (sortBy) {
        case "name":
          return a.name.localeCompare(b.name, "ru");
        case "created":
          return (b.created_at || 0) - (a.created_at || 0);
        case "mc_version":
          return (b.mc_version || "").localeCompare(a.mc_version || "");
        default:
          return (b.updated_at || 0) - (a.updated_at || 0);
      }
    });
  }, [instances, search, sortBy]);

  const mcOptions = useMemo<CustomSelectOption[]>(
    () => [
      { value: "", label: "Не выбрана" },
      ...mcVersions.map((v) => ({ value: v, label: `Minecraft ${v}` })),
    ],
    [mcVersions]
  );

  const javaOptions = useMemo<CustomSelectOption[]>(
    () => [
      { value: "", label: "Выберите установленную Java..." },
      ...javaList.map((j) => ({
        value: j.id,
        label: `${j.name} (${j.version || `Java ${j.major_version}`})`,
        sub: j.arch,
      })),
    ],
    [javaList]
  );

  const accountOptions = useMemo<CustomSelectOption[]>(
    () => [
      {
        value: "",
        label: activeAccount
          ? `Активный аккаунт (${activeAccount.minecraft_username || activeAccount.xbox_gamertag || "Игрок"}${activeAccount.account_type === "offline" ? " • Офлайн" : ""})`
          : "Активный аккаунт (не выбран)",
      },
      ...accounts.map((a) => ({
        value: a.id,
        label: a.minecraft_username || a.xbox_gamertag || a.id,
        sub: a.account_type === "offline" ? "Офлайн-режим" : a.license_status === "owned" ? "Лицензия ✓" : "Нет лицензии",
      })),
    ],
    [accounts, activeAccount]
  );

  const resolveAccountLabel = (inst: GameInstance): string => {
    const prefId = inst.launch_settings?.account_id || "";
    const acc = prefId ? accounts.find((a) => a.id === prefId) : activeAccount;
    if (!acc) return "Аккаунт не выбран";
    return acc.minecraft_username || acc.xbox_gamertag || "Microsoft";
  };

  const handleLaunchProfile = async (inst: GameInstance, openConsole = true) => {
    try {
      await onLaunch({
        instance_id: inst.id,
        account_id: inst.launch_settings?.account_id || "",
      });
      if (openConsole && isMountedRef.current) {
        setSelectedId(inst.id);
        setDetailTab("console");
      }
    } catch (err) {
      onToast(`Не удалось запустить «${inst.name}»: ${(err as Error).message}`, "err");
    }
  };

  const handleOpenDir = async (id: string, subdir = "") => {
    try {
      await desktopBridge.openInstanceDir(id, subdir);
    } catch (err) {
      onToast(String((err as Error).message || err), "err");
    }
  };

  const handleVerify = async (id: string) => {
    setVerifying(true);
    try {
      const rep = await onVerify(id);
      onToast(`Проверка и восстановление завершены: ${statusBadge(rep.status).label}${rep.issues.length ? ` (нерешённых проблем: ${rep.issues.length})` : ""}`, rep.status === "ready" ? "ok" : "err");
    } catch (err) {
      onToast(`Ошибка проверки: ${(err as Error).message}`, "err");
    } finally {
      if (isMountedRef.current) {
        setVerifying(false);
      }
    }
  };

  const handleRepair = async (id: string) => {
    setRepairing(true);
    try {
      await onRepair(id);
    } catch (err) {
      onToast(`Ошибка восстановления: ${(err as Error).message}`, "err");
    } finally {
      if (isMountedRef.current) {
        setRepairing(false);
      }
    }
  };

  const handleSaveSettings = async (e?: React.SyntheticEvent) => {
    e?.preventDefault();
    if (!selected) return;
    setSavingSettings(true);
    try {
      await onUpdate({
        id: selected.id,
        name: editName.trim(),
        description: editDesc.trim(),
        icon: editIcon,
        mc_version: editType === "empty" ? "" : editMc,
        instance_type: editType,
        loader_version: editType === "empty" || editType === "vanilla" ? "" : editLoaderVer.trim(),
        memory_min_mb: editMemMin,
        memory_max_mb: editMemMax,
        java_mode: editJavaMode,
        java_runtime_id: editJavaId,
        java_path: editJavaPath.trim(),
        launch_settings: {
          jvm_args: editJvmArgs.trim(),
          window_width: editWinW,
          window_height: editWinH,
          fullscreen: editFullscreen,
          active_version_id: selected.launch_settings?.active_version_id || "",
          account_id: editAccountId,
        },
      });
      onToast("Настройки профиля сохранены", "ok");
    } catch (err) {
      onToast(`Ошибка сохранения: ${(err as Error).message}`, "err");
    } finally {
      if (isMountedRef.current) {
        setSavingSettings(false);
      }
    }
  };

  const handleScanJava = async () => {
    setScanningJava(true);
    try {
      const snap = await api.scanJavaRuntimes();
      if (isMountedRef.current) {
        setJavaList(snap.runtimes || []);
      }
      onToast(`Сканирование завершено: обнаружено версий Java: ${(snap.runtimes || []).length}`, "ok");
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      if (isMountedRef.current) {
        setScanningJava(false);
      }
    }
  };

  const recommendedJavaVer = useMemo(() => {
    const mc = selected?.mc_version || "";
    const parts = mc.split(".").map(Number);
    const minor = parts[1] || 0;
    const patch = parts[2] || 0;
    if (minor >= 21 || (minor === 20 && patch >= 5)) return "Java 21";
    if (minor >= 18) return "Java 17";
    if (minor === 17) return "Java 16";
    if (minor > 0 && minor < 17) return "Java 8 (1.8)";
    return "Java 17 / 21";
  }, [selected?.mc_version]);

  // ========================================================
  // DETAIL VIEW FOR A SINGLE INSTANCE
  // ========================================================
  if (selected) {
    const sb = statusBadge(selected.status);
    const curSess = sessionsByInstance[selected.id] || null;
    const isRunning = isSessionActive(curSess?.state);

    return (
      <div className="mh-page mh-page-enter">
        <div className="mh-inst-detail-top">
          <button
            type="button"
            className="btn btn-ghost btn-xs"
            onClick={() => {
              setSelectedId("");
              setDetailTab("mods");
            }}
          >
            ← Все профили
          </button>
          <div className="mh-inst-detail-actions">
            {!isRunning ? (
              <button type="button" className="btn btn-primary btn-xs" onClick={() => void handleLaunchProfile(selected, true)}>
                ▶ Играть
              </button>
            ) : (
              <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => void onStop(selected.id, false)}>
                ■ Остановить
              </button>
            )}
            {selected.status === "corrupted" && (
              <button
                type="button"
                className={`btn btn-primary btn-xs${repairing ? " is-loading" : ""}`}
                disabled={repairing}
                onClick={() => handleRepair(selected.id)}
              >
                {repairing ? "Восстановление…" : "Восстановить"}
              </button>
            )}
            <button type="button" className="btn btn-secondary btn-xs" onClick={() => setExportTarget(selected)}>
              Экспорт сборки
            </button>
            <button
              type="button"
              className={`btn btn-ghost btn-xs${verifying ? " is-loading" : ""}`}
              disabled={verifying}
              onClick={() => handleVerify(selected.id)}
            >
              {verifying ? "Проверка…" : "Проверить"}
            </button>
            {desktopBridge.isDesktop() && (
              <button type="button" className="btn btn-ghost btn-xs" onClick={() => handleOpenDir(selected.id, "minecraft")}>
                Открыть папку
              </button>
            )}
            <button type="button" className="btn btn-ghost btn-xs" onClick={() => setDupTarget(selected)}>
              Дублировать
            </button>
            <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => setDelTarget(selected)}>
              Удалить
            </button>
          </div>
        </div>

        <header className="mh-page-hero mh-inst-hero">
          <div className="mh-inst-hero-left">
            <InstanceIcon icon={selected.icon} name={selected.name} size={62} />
            <div className="mh-inst-hero-copy">
              <div className="mh-inst-hero-title-row">
                <h1 className="mh-page-title">{selected.name}</h1>
                <span className={`mh-dl-status-pill ${sb.cls}`}>{sb.label}</span>
              </div>
              {selected.description && <p className="mh-page-subtitle">{selected.description}</p>}
              <div className="mh-inst-hero-pills mono-num">
                <span className="pill">{selected.mc_version ? `MC ${selected.mc_version}` : "Без версии MC"}</span>
                <span className="pill">
                  {typeLabel(selected.instance_type)}
                  {selected.loader_version ? ` ${selected.loader_version}` : ""}
                </span>
                <span className="pill">{selected.memory_max_mb} МБ ОЗУ</span>
                <span className="pill">Модов: {selected.mods_count}</span>
              </div>
            </div>
          </div>
        </header>

        {activeInstallsById[selected.id] && (() => {
          const prog = activeInstallsById[selected.id];
          return (
            <section className="mh-panel mh-install-live-panel" role="status">
              <div className="mh-panel-head">
                <div>
                  <h2>Загрузка компонентов игры ({prog.loader === "vanilla" ? "Vanilla" : formatLoaderName(prog.loader)})</h2>
                  <p className="mh-muted-meta" style={{ margin: "2px 0 0" }}>
                    Minecraft {prog.mc_version}{prog.loader_version ? ` • ${prog.loader_version}` : ""}
                  </p>
                </div>
                <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => void api.cancelProfileInstall(selected.id)}>
                  Отменить
                </button>
              </div>
              <div className="mh-dl-meta mono-num">
                <span>{prog.message}</span>
                {prog.total_files > 0 && <span>• Файлов: {prog.completed_files} / {prog.total_files}</span>}
                {prog.progress >= 0 && <span>• {Math.round(prog.progress)}%</span>}
              </div>
              <div className="mh-dl-progress-track">
                <div
                  className={`mh-dl-progress-fill${prog.progress < 0 ? " is-indeterminate" : ""}`}
                  style={{ width: prog.progress < 0 ? "35%" : `${Math.max(2, Math.min(100, prog.progress))}%` }}
                />
              </div>
            </section>
          );
        })()}

        <nav className="mh-inst-tabs" role="tablist" aria-label="Разделы профиля">
          {(
            [
              { id: "mods", label: `Моды (${selected.mods_count})` },
              { id: "resourcepacks", label: `Ресурспаки (${resourcePacks.length})` },
              { id: "shaders", label: `Шейдеры (${shaders.length})` },
              { id: "worlds", label: `Миры (${worlds.length})` },
              { id: "settings", label: "Настройки запуска" },
              { id: "console", label: isRunning ? "Консоль ●" : "Консоль" },
            ] as Array<{ id: DetailTab; label: string }>
          ).map((t) => (
            <button
              key={t.id}
              type="button"
              role="tab"
              aria-selected={detailTab === t.id}
              className={`mh-inst-tab${detailTab === t.id ? " is-active" : ""}`}
              onClick={() => setDetailTab(t.id)}
            >
              {t.label}
            </button>
          ))}
        </nav>

        {detailTab === "console" && (
          <InstanceConsoleTab
            instance={selected}
            session={curSess}
            logs={logsByInstance[selected.id] || []}
            onLoadLogs={onLoadLogs}
            onClearDisplay={onClearDisplayLogs}
            onLaunch={() => void handleLaunchProfile(selected, false)}
            onStop={(force) => void onStop(selected.id, force)}
            onOpenMods={() => setDetailTab("mods")}
            onOpenSettings={() => setDetailTab("settings")}
            onToast={onToast}
          />
        )}

        {detailTab === "mods" && (
          <section className="mh-panel">
            <div className="mh-panel-head">
              <div>
                <h2>Моды профиля ({(mods || []).length})</h2>
                {availableUpdatesCount > 0 && (
                  <span style={{ fontSize: "12px", color: "#10b981", fontWeight: 600 }}>
                    Доступно обновлений: {availableUpdatesCount}
                  </span>
                )}
              </div>
              <div className="mh-placeholder-actions" style={{ flexWrap: "wrap", gap: "6px" }}>
                <button
                  type="button"
                  className="btn btn-ghost btn-xs"
                  disabled={modsLoading || checkingUpdates}
                  onClick={handleCheckModUpdates}
                >
                  {checkingUpdates ? "Поиск обновлений…" : "Проверить обновления"}
                </button>
                {availableUpdatesCount > 0 && (
                  <button
                    type="button"
                    className="btn btn-primary btn-xs"
                    disabled={selected.in_use}
                    onClick={handleApplyAllModUpdates}
                  >
                    Обновить все ({availableUpdatesCount})
                  </button>
                )}
                {(mods || []).length > 0 && (
                  <>
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs"
                      disabled={modsLoading || selected.in_use}
                      onClick={() => void handleSetAllMods(true)}
                    >
                      Включить все
                    </button>
                    <button
                      type="button"
                      className="btn btn-ghost btn-xs"
                      disabled={modsLoading || selected.in_use}
                      onClick={() => void handleSetAllMods(false)}
                    >
                      Отключить все
                    </button>
                  </>
                )}
                {selectedMods.size > 0 && (
                  <button
                    type="button"
                    className="btn btn-danger btn-xs"
                    disabled={modsLoading || selected.in_use}
                    onClick={handleDeleteSelectedMods}
                  >
                    Удалить выбранные ({selectedMods.size})
                  </button>
                )}
                <button type="button" className="btn btn-ghost btn-xs" disabled={modsLoading} onClick={() => void refreshMods(selected.id)}>
                  Обновить список
                </button>
                <button type="button" className="btn btn-primary btn-xs" onClick={() => { onArmModInstall(selected); onNavigate("catalog"); }}>
                  Установить из каталога
                </button>
              </div>
            </div>

            {modInstallTarget?.id === selected.id && (
              <div className="mh-info-callout">
                Установка в профиль «{modInstallTarget.name}» — выберите мод в каталоге
                <button type="button" className="btn btn-ghost btn-xs" onClick={onClearModInstall}>Отменить</button>
              </div>
            )}

            {(mods || []).length > 0 && (
              <div style={{ display: "flex", gap: "8px", alignItems: "center", marginBottom: "8px" }}>
                <label style={{ display: "flex", alignItems: "center", gap: "6px", fontSize: "12px", cursor: "pointer", userSelect: "none" }}>
                  <input
                    type="checkbox"
                    checked={filteredMods.length > 0 && selectedMods.size === filteredMods.length}
                    onChange={toggleSelectAllMods}
                  />
                  <span>Выбрать все</span>
                </label>
                {(mods || []).length > 5 && (
                  <input
                    className="text-input"
                    type="search"
                    style={{ flex: 1, margin: 0 }}
                    value={modsSearch}
                    onChange={(e) => setModsSearch(e.target.value)}
                    placeholder="Поиск по имени файла…"
                    aria-label="Поиск модов"
                  />
                )}
              </div>
            )}

            {(mods || []).length > 0 ? (
              <div className="mh-dl-list">
                {filteredMods.map((mod) => {
                  const upd = modUpdates[mod.filename];
                  const hasUpd = upd?.has_update && upd.new_download_url;
                  const isUpdating = busyUpdatingMods.has(mod.filename);
                  return (
                    <div key={mod.filename} className="mh-saved-row" style={{ alignItems: "center" }}>
                      <div style={{ display: "flex", alignItems: "center", marginRight: 8 }}>
                        <input
                          type="checkbox"
                          checked={selectedMods.has(mod.filename)}
                          onChange={() => toggleModSelected(mod.filename)}
                          aria-label={`Выбрать ${mod.filename}`}
                        />
                      </div>
                      <div className="mh-saved-info">
                        <div style={{ display: "flex", alignItems: "center", gap: "8px", flexWrap: "wrap" }}>
                          <span className="mh-saved-name mono-num">{mod.filename.replace(/\.jar(?:\.disabled)?$/i, "")}</span>
                          {hasUpd && (
                            <span
                              style={{
                                background: "rgba(16, 185, 129, 0.15)",
                                color: "#10b981",
                                border: "1px solid rgba(16, 185, 129, 0.3)",
                                borderRadius: "4px",
                                padding: "1px 6px",
                                fontSize: "11px",
                                fontWeight: 600,
                              }}
                            >
                              Доступно: {upd.new_version_number || "новое"}
                            </span>
                          )}
                        </div>
                        <span className="mh-saved-meta mono-num">
                          {formatBytes(mod.size)} • {mod.enabled ? "Активен" : "Отключён"} • <span title={mod.filename}>{mod.filename}</span>
                        </span>
                      </div>
                      <div className="mh-placeholder-actions">
                        {hasUpd && (
                          <button
                            type="button"
                            className="btn btn-primary btn-xs"
                            disabled={isUpdating || selected.in_use}
                            onClick={() => void handleApplyModUpdate(mod)}
                          >
                            {isUpdating ? "Обновление…" : "Обновить"}
                          </button>
                        )}
                        <button
                          type="button"
                          className="btn btn-ghost btn-xs"
                          disabled={busyMods.has(mod.filename) || selected.in_use}
                          onClick={() => void runModAction(mod.filename, () => api.setInstanceModEnabled(selected.id, mod.filename, !mod.enabled), mod.enabled ? "Мод отключён" : "Мод включён")}
                        >
                          {busyMods.has(mod.filename) ? "Сохранение…" : mod.enabled ? "Отключить" : "Включить"}
                        </button>
                        <button
                          type="button"
                          className="btn btn-ghost btn-xs"
                          disabled={busyMods.has(mod.filename) || selected.in_use}
                          onClick={() => {
                            if (!window.confirm(`Удалить мод «${mod.filename}» из профиля?`)) return;
                            void runModAction(mod.filename, () => api.deleteInstanceMod(selected.id, mod.filename), "Мод удалён");
                          }}
                        >
                          Удалить
                        </button>
                      </div>
                    </div>
                  );
                })}
              </div>
            ) : (
              <div className="mh-panel-empty">
                В папке <code>minecraft/mods/</code> пока нет файлов модов.
              </div>
            )}
          </section>
        )}

        {detailTab === "resourcepacks" && (
          <section className="mh-panel">
            <div className="mh-panel-head">
              <h2>Ресурспаки профиля ({(resourcePacks || []).length})</h2>
              <div className="mh-placeholder-actions">
                <button type="button" className="btn btn-ghost btn-xs" disabled={rpLoading} onClick={() => void refreshResourcePacks(selected.id)}>Обновить</button>
                <button type="button" className="btn btn-primary btn-xs" onClick={() => { onArmModInstall(selected); onNavigate("catalog"); }}>Установить из каталога</button>
                {desktopBridge.isDesktop() && <button type="button" className="btn btn-ghost btn-xs" onClick={() => handleOpenDir(selected.id, "resourcepacks")}>Открыть папку resourcepacks</button>}
              </div>
            </div>

            {modInstallTarget?.id === selected.id && (
              <div className="mh-info-callout">
                Установка в профиль «{modInstallTarget.name}» — выберите ресурспак в каталоге
                <button type="button" className="btn btn-ghost btn-xs" onClick={onClearModInstall}>Отменить</button>
              </div>
            )}

            {(resourcePacks || []).length > 5 && (
              <input className="text-input" type="search" value={rpSearch} onChange={(e) => setRpSearch(e.target.value)} placeholder="Поиск ресурспаков…" aria-label="Поиск ресурспаков" />
            )}

            {(resourcePacks || []).length > 0 ? (
              <div className="mh-dl-list">
                {filteredResourcePacks.map((rp) => (
                  <div key={rp.filename} className="mh-saved-row" style={{ alignItems: "center" }}>
                    <div style={{ marginRight: 12, display: "flex", alignItems: "center" }}>
                      {rp.icon_data_url ? (
                        <img src={rp.icon_data_url} alt="" style={{ width: 44, height: 44, borderRadius: 6, objectFit: "cover", imageRendering: "pixelated" }} />
                      ) : (
                        <div style={{ width: 44, height: 44, borderRadius: 6, background: "rgba(255,255,255,0.06)", display: "flex", alignItems: "center", justifyContent: "center", fontSize: 20 }}>
                          🎨
                        </div>
                      )}
                    </div>
                    <div className="mh-saved-info" style={{ flex: 1, minWidth: 0 }}>
                      <span className="mh-saved-name" style={{ fontSize: "14px", fontWeight: 600 }}>{rp.name}</span>
                      <span className="mh-saved-meta mono-num">
                        {formatBytes(rp.size)} • {rp.enabled ? "Активен" : "Отключён"} {rp.pack_format > 0 ? `• Format ${rp.pack_format}` : ""} • <span title={rp.filename}>{rp.filename}</span>
                      </span>
                      {rp.description && (
                        <div className="mh-muted-meta" style={{ fontSize: "12px", marginTop: 2, whiteSpace: "nowrap", overflow: "hidden", textOverflow: "ellipsis" }}>
                          {rp.description}
                        </div>
                      )}
                    </div>
                    <div className="mh-placeholder-actions">
                      <button type="button" className="btn btn-ghost btn-xs" disabled={busyRp.has(rp.filename) || selected.in_use} onClick={() => void runRpAction(rp.filename, () => api.setInstanceResourcePackEnabled(selected.id, rp.filename, !rp.enabled), rp.enabled ? "Ресурспак отключён" : "Ресурспак включён")}>
                        {busyRp.has(rp.filename) ? "Сохранение…" : rp.enabled ? "Отключить" : "Включить"}
                      </button>
                      <button type="button" className="btn btn-ghost btn-xs" disabled={busyRp.has(rp.filename) || selected.in_use} onClick={() => {
                        if (!window.confirm(`Удалить ресурспак «${rp.name}» из профиля?`)) return;
                        void runRpAction(rp.filename, () => api.deleteInstanceResourcePack(selected.id, rp.filename), "Ресурспак удалён");
                      }}>Удалить</button>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="mh-panel-empty">
                В папке <code>minecraft/resourcepacks/</code> пока нет файлов ресурспаков.
                {desktopBridge.isDesktop() && (
                  <div className="mh-placeholder-actions">
                    <button type="button" className="btn btn-ghost btn-sm" onClick={() => handleOpenDir(selected.id, "resourcepacks")}>Открыть папку resourcepacks</button>
                  </div>
                )}
              </div>
            )}
          </section>
        )}

        {detailTab === "shaders" && (
          <section className="mh-panel">
            <div className="mh-panel-head">
              <h2>Шейдерпаки профиля ({(shaders || []).length})</h2>
              <div className="mh-placeholder-actions">
                <button type="button" className="btn btn-ghost btn-xs" disabled={shadersLoading} onClick={() => void refreshShaders(selected.id)}>Обновить</button>
                <button type="button" className="btn btn-primary btn-xs" onClick={() => { onArmModInstall(selected); onNavigate("catalog"); }}>Установить из каталога</button>
                {desktopBridge.isDesktop() && <button type="button" className="btn btn-ghost btn-xs" onClick={() => handleOpenDir(selected.id, "shaderpacks")}>Открыть папку shaderpacks</button>}
              </div>
            </div>

            {modInstallTarget?.id === selected.id && (
              <div className="mh-info-callout">
                Установка в профиль «{modInstallTarget.name}» — выберите шейдер в каталоге
                <button type="button" className="btn btn-ghost btn-xs" onClick={onClearModInstall}>Отменить</button>
              </div>
            )}

            {(shaders || []).length > 5 && (
              <input className="text-input" type="search" value={shadersSearch} onChange={(e) => setShadersSearch(e.target.value)} placeholder="Поиск шейдеров…" aria-label="Поиск шейдеров" />
            )}

            {(shaders || []).length > 0 ? (
              <div className="mh-dl-list">
                {filteredShaders.map((sh) => (
                  <div key={sh.filename} className="mh-saved-row" style={{ alignItems: "center" }}>
                    <div style={{ marginRight: 12, display: "flex", alignItems: "center" }}>
                      <div style={{ width: 44, height: 44, borderRadius: 6, background: "rgba(255,255,255,0.06)", display: "flex", alignItems: "center", justifyContent: "center", fontSize: 20 }}>
                        ✨
                      </div>
                    </div>
                    <div className="mh-saved-info" style={{ flex: 1, minWidth: 0 }}>
                      <span className="mh-saved-name" style={{ fontSize: "14px", fontWeight: 600 }}>{sh.name}</span>
                      <span className="mh-saved-meta mono-num">
                        {formatBytes(sh.size)} • {sh.enabled ? "Активен" : "Отключён"} • <span title={sh.filename}>{sh.filename}</span>
                      </span>
                    </div>
                    <div className="mh-placeholder-actions">
                      <button type="button" className="btn btn-ghost btn-xs" disabled={busyShaders.has(sh.filename) || selected.in_use} onClick={() => void runShaderAction(sh.filename, () => api.setInstanceShaderPackEnabled(selected.id, sh.filename, !sh.enabled), sh.enabled ? "Шейдер отключён" : "Шейдер включён")}>
                        {busyShaders.has(sh.filename) ? "Сохранение…" : sh.enabled ? "Отключить" : "Включить"}
                      </button>
                      <button type="button" className="btn btn-ghost btn-xs" disabled={busyShaders.has(sh.filename) || selected.in_use} onClick={() => {
                        if (!window.confirm(`Удалить шейдерпак «${sh.name}» из профиля?`)) return;
                        void runShaderAction(sh.filename, () => api.deleteInstanceShaderPack(selected.id, sh.filename), "Шейдер удалён");
                      }}>Удалить</button>
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="mh-panel-empty">
                В папке <code>minecraft/shaderpacks/</code> пока нет файлов шейдерпаков.
                {desktopBridge.isDesktop() && (
                  <div className="mh-placeholder-actions">
                    <button type="button" className="btn btn-ghost btn-sm" onClick={() => handleOpenDir(selected.id, "shaderpacks")}>Открыть папку shaderpacks</button>
                  </div>
                )}
              </div>
            )}
          </section>
        )}

        {detailTab === "worlds" && (
          <section className="mh-panel">
            <div className="mh-panel-head">
              <h2>Миры и сохранения профиля ({(worlds || []).length})</h2>
              <div className="mh-placeholder-actions">
                <button type="button" className="btn btn-ghost btn-xs" disabled={worldsLoading} onClick={() => void refreshWorlds(selected.id)}>Обновить</button>
                {desktopBridge.isDesktop() && <button type="button" className="btn btn-ghost btn-xs" onClick={() => handleOpenDir(selected.id, "saves")}>Открыть папку saves</button>}
              </div>
            </div>

            {(worlds || []).length > 5 && (
              <input className="text-input" type="search" value={worldsSearch} onChange={(e) => setWorldsSearch(e.target.value)} placeholder="Поиск миров…" aria-label="Поиск миров" />
            )}

            {(worlds || []).length > 0 ? (
              <div className="mh-dl-list">
                {filteredWorlds.map((w) => (
                  <div key={w.folder} className="mh-saved-row" style={{ alignItems: "center" }}>
                    <div style={{ marginRight: 12, display: "flex", alignItems: "center" }}>
                      {w.icon_data_url ? (
                        <img src={w.icon_data_url} alt="" style={{ width: 48, height: 48, borderRadius: 6, objectFit: "cover", imageRendering: "pixelated" }} />
                      ) : (
                        <div style={{ width: 48, height: 48, borderRadius: 6, background: "rgba(255,255,255,0.06)", display: "flex", alignItems: "center", justifyContent: "center", fontSize: 22 }}>
                          🌍
                        </div>
                      )}
                    </div>
                    <div className="mh-saved-info" style={{ flex: 1, minWidth: 0 }}>
                      <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
                        <span className="mh-saved-name" style={{ fontSize: "14px", fontWeight: 600 }}>{w.name}</span>
                        <span className={`pill ${w.hardcore ? "pill-danger" : w.game_type === "creative" ? "pill-info" : w.game_type === "adventure" ? "pill-warn" : w.game_type === "spectator" ? "pill-muted" : "pill-installed"}`}>
                          {w.hardcore ? "Хардкор" : w.game_type === "creative" ? "Творчество" : w.game_type === "adventure" ? "Приключение" : w.game_type === "spectator" ? "Наблюдатель" : "Выживание"}
                        </span>
                      </div>
                      <span className="mh-saved-meta mono-num">
                        Папка: {w.folder} • {formatBytes(w.size)} {w.version ? `• MC ${w.version}` : ""} {w.last_played ? `• Играли: ${formatDate(new Date(w.last_played * 1000).toISOString())}` : ""}
                      </span>
                    </div>
                    <div className="mh-placeholder-actions">
                      <button
                        type="button"
                        className={`btn btn-primary btn-xs${exportingWorld === w.folder ? " is-loading" : ""}`}
                        disabled={exportingWorld === w.folder || selected.in_use}
                        onClick={() => void handleExportWorld(w.folder)}
                      >
                        {exportingWorld === w.folder ? "Архивация…" : "Экспорт в ZIP (бэкап)"}
                      </button>
                      {desktopBridge.isDesktop() && (
                        <button type="button" className="btn btn-ghost btn-xs" onClick={() => handleOpenDir(selected.id, "saves")}>
                          Папка saves
                        </button>
                      )}
                    </div>
                  </div>
                ))}
              </div>
            ) : (
              <div className="mh-panel-empty">
                В папке <code>minecraft/saves/</code> пока нет сохранённых миров.
                <p className="mh-muted-meta" style={{ marginTop: 4 }}>Запустите игру и создайте одиночный мир, либо скопируйте существующий мир в папку saves.</p>
                <div className="mh-placeholder-actions" style={{ marginTop: 8 }}>
                  {desktopBridge.isDesktop() && <button type="button" className="btn btn-ghost btn-sm" onClick={() => handleOpenDir(selected.id, "saves")}>Открыть папку saves</button>}
                </div>
              </div>
            )}
          </section>
        )}

        {detailTab === "settings" && (
          <div className="mh-panel mh-form-stack">
            <div className="mh-panel-head">
              <h2>Настройки запуска</h2>
              <button
                type="button"
                className={`btn btn-primary btn-sm${savingSettings ? " is-loading" : ""}`}
                disabled={savingSettings}
                onClick={() => void handleSaveSettings()}
              >
                {savingSettings ? "Сохранение…" : "Сохранить изменения"}
              </button>
            </div>

            <div className="mh-form-grid-2">
              <div className="mh-form-field">
                <label htmlFor="editInstName">Название</label>
                <input id="editInstName" type="text" className="text-input" value={editName} onChange={(e) => setEditName(e.target.value)} />
              </div>
              <div className="mh-form-field">
                <label htmlFor="editInstDesc">Описание</label>
                <input id="editInstDesc" type="text" className="text-input" value={editDesc} onChange={(e) => setEditDesc(e.target.value)} />
              </div>
            </div>

            <div className="mh-form-field">
              <label>Иконка</label>
              <div className="mh-icon-picker-row">
                {INSTANCE_ICON_PRESETS.map((p) => (
                  <button
                    key={p.id}
                    type="button"
                    className={`mh-icon-pick-btn${editIcon === p.id ? " is-active" : ""}`}
                    title={p.label}
                    onClick={() => setEditIcon(p.id)}
                  >
                    <InstanceIcon icon={p.id} name={editName || p.symbol} size={30} />
                  </button>
                ))}
              </div>
            </div>

            <div className="mh-form-grid-2">
              <div className="mh-form-field">
                <label>Тип профиля</label>
                <CustomSelect value={editType} options={TYPE_OPTS} onChange={(v) => setEditType(v as InstanceType)} ariaLabel="Тип профиля" />
              </div>
              <div className="mh-form-field">
                <label>Версия Minecraft</label>
                <CustomSelect
                  value={editType === "empty" ? "" : editMc}
                  options={mcOptions}
                  onChange={setEditMc}
                  searchable
                  mono
                  disabled={editType === "empty"}
                  ariaLabel="Версия Minecraft"
                />
              </div>
            </div>

            {editType !== "empty" && editType !== "vanilla" && (
              <div className="mh-form-field">
                <label htmlFor="editLoaderVer">Версия загрузчика</label>
                <input id="editLoaderVer" type="text" className="text-input mono-num" value={editLoaderVer} onChange={(e) => setEditLoaderVer(e.target.value)} />
              </div>
            )}

            <div className="mh-form-field">
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 6 }}>
                <label htmlFor="editMemRange" style={{ marginBottom: 0 }}>
                  Выделение оперативной памяти (Xmx)
                </label>
                <span className="mono-num mh-mem-badge" style={{ fontWeight: 600 }}>
                  {editMemMax} МБ ({(editMemMax / 1024).toFixed(1)} ГБ)
                </span>
              </div>
              <input
                id="editMemRange"
                type="range"
                min={1024}
                max={Math.max(4096, systemMem.total_ram_mb - 512)}
                step={512}
                value={editMemMax}
                onChange={(e) => setEditMemMax(Number(e.target.value))}
                style={{ width: "100%", accentColor: "var(--accent-color, #10b981)", cursor: "pointer" }}
              />
              <div style={{ display: "flex", gap: 6, marginTop: 8, flexWrap: "wrap", alignItems: "center" }}>
                <span className="mh-muted-meta" style={{ fontSize: "0.8rem", marginRight: 4 }}>Пресеты:</span>
                {[2048, 4096, 6144, 8192].filter((mb) => mb <= Math.max(4096, systemMem.total_ram_mb - 512)).map((mb) => (
                  <button
                    key={mb}
                    type="button"
                    className={`btn btn-xs ${editMemMax === mb ? "btn-primary" : "btn-ghost"}`}
                    onClick={() => setEditMemMax(mb)}
                  >
                    {mb / 1024} ГБ
                  </button>
                ))}
                {systemMem.recommended_max_mb > 0 && systemMem.recommended_max_mb <= systemMem.total_ram_mb - 512 && (
                  <button
                    type="button"
                    className={`btn btn-xs ${editMemMax === systemMem.recommended_max_mb ? "btn-primary" : "btn-ghost"}`}
                    onClick={() => setEditMemMax(systemMem.recommended_max_mb)}
                    title={`Рекомендуемый максимум для вашей системы (${systemMem.total_ram_mb} МБ RAM)`}
                  >
                    Рек. ({Math.round(systemMem.recommended_max_mb / 1024)} ГБ)
                  </button>
                )}
              </div>
              <p className="mh-muted-meta" style={{ marginTop: 6, fontSize: "0.78rem" }}>
                ОЗУ ПК: {systemMem.total_ram_mb} МБ • Мин. память (Xms): {editMemMin} МБ.
              </p>
            </div>

            <div className="mh-form-field">
              <label>Аккаунт для запуска профиля</label>
              <CustomSelect value={editAccountId} options={accountOptions} onChange={setEditAccountId} ariaLabel="Выбор аккаунта" />
            </div>

            <div className="mh-form-field">
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 6, gap: 8, flexWrap: "wrap" }}>
                <label style={{ marginBottom: 0 }}>Настройка Java</label>
                <div style={{ display: "inline-flex", gap: 6, flexWrap: "wrap" }}>
                  <button
                    type="button"
                    className="btn btn-ghost btn-xs"
                    disabled={scanningJava}
                    onClick={handleScanJava}
                    title="Найти установленные версии Java на ПК"
                  >
                    {scanningJava ? "Сканирование…" : "🔍 Найти Java"}
                  </button>
                  <button
                    type="button"
                    className={`btn btn-xs ${showJavaRuntimeMgr ? "btn-primary" : "btn-ghost"}`}
                    onClick={() => setShowJavaRuntimeMgr((v) => !v)}
                  >
                    {showJavaRuntimeMgr ? "Скрыть Java Runtime" : "Управление Java Runtime"}
                  </button>
                </div>
              </div>
              <CustomSelect value={editJavaMode} options={JAVA_MODE_OPTS} onChange={(v) => setEditJavaMode(v as InstanceJavaMode)} ariaLabel="Режим выбора Java" />
              {selected.mc_version && (
                <span className="mh-muted-meta" style={{ fontSize: "12px", marginTop: "4px", display: "block" }}>
                  Для Minecraft {selected.mc_version} рекомендуется: <strong style={{ color: "var(--accent-color, #10b981)" }}>{recommendedJavaVer}</strong>
                </span>
              )}
              {showJavaRuntimeMgr && (
                <div className="mh-inst-java-mgr-wrap">
                  <JavaSettingsSection onOpenDownloads={() => onNavigate("downloads")} onToast={onToast} />
                </div>
              )}
            </div>

            {editJavaMode === "managed" && (
              <div className="mh-form-field">
                <label>Выберите версию Java</label>
                <CustomSelect value={editJavaId} options={javaOptions} onChange={setEditJavaId} ariaLabel="Выбор Java" />
              </div>
            )}

            {editJavaMode === "custom" && (
              <div className="mh-form-field">
                <label htmlFor="editJavaPath">Путь к java.exe / javaw.exe</label>
                <input id="editJavaPath" type="text" className="text-input mono-num" placeholder="C:\Program Files\Java\...\bin\java.exe" value={editJavaPath} onChange={(e) => setEditJavaPath(e.target.value)} />
              </div>
            )}

            <div className="mh-form-grid-2">
              <div className="mh-form-field">
                <label htmlFor="editWinW">Ширина окна</label>
                <input id="editWinW" type="number" min={640} max={7680} className="text-input mono-num" value={editWinW} onChange={(e) => setEditWinW(Number(e.target.value) || 1280)} />
              </div>
              <div className="mh-form-field">
                <label htmlFor="editWinH">Высота окна</label>
                <input id="editWinH" type="number" min={480} max={4320} className="text-input mono-num" value={editWinH} onChange={(e) => setEditWinH(Number(e.target.value) || 720)} />
              </div>
            </div>
            <div style={{ display: "flex", gap: "6px", marginTop: "-6px", marginBottom: "12px", flexWrap: "wrap", alignItems: "center" }}>
              <span className="mh-muted-meta" style={{ fontSize: "0.8rem", marginRight: 4 }}>Разрешение:</span>
              {[
                { label: "720p (HD)", w: 1280, h: 720 },
                { label: "900p", w: 1600, h: 900 },
                { label: "1080p (FHD)", w: 1920, h: 1080 },
                { label: "2K (QHD)", w: 2560, h: 1440 },
              ].map((res) => (
                <button
                  key={res.label}
                  type="button"
                  className={`btn btn-xs ${editWinW === res.w && editWinH === res.h ? "btn-primary" : "btn-ghost"}`}
                  onClick={() => {
                    setEditWinW(res.w);
                    setEditWinH(res.h);
                  }}
                >
                  {res.label}
                </button>
              ))}
            </div>

            <div className="mh-form-field">
              <label htmlFor="editJvmArgs">Дополнительные аргументы JVM</label>
              <input id="editJvmArgs" type="text" className="text-input mono-num" placeholder="-XX:+UseG1GC" value={editJvmArgs} onChange={(e) => setEditJvmArgs(e.target.value)} />
              <div style={{ display: "flex", gap: "6px", marginTop: "6px", flexWrap: "wrap", alignItems: "center" }}>
                <span className="mh-muted-meta" style={{ fontSize: "0.8rem", marginRight: 4 }}>Пресеты GC:</span>
                <button
                  type="button"
                  className={`btn btn-xs ${editJvmArgs.includes("UseG1GC") ? "btn-primary" : "btn-ghost"}`}
                  title="Aikar's Flags (G1GC) — оптимизация сборщика мусора для уменьшения просадок FPS"
                  onClick={() => setEditJvmArgs("-XX:+UseG1GC -XX:+ParallelRefProcEnabled -XX:MaxGCPauseMillis=200 -XX:+UnlockExperimentalVMOptions -XX:+DisableExplicitGC -XX:+AlwaysPreTouch -XX:G1NewSizePercent=30 -XX:G1MaxNewSizePercent=40 -XX:G1ReservePercent=20 -XX:G1HeapWastePercent=5 -XX:G1MixedGCCountTarget=4 -XX:InitiatingHeapOccupancyPercent=15 -XX:G1MixedGCLiveThresholdPercent=90 -XX:G1RSetUpdatingPauseTimePercent=5 -XX:SurvivorRatio=32")}
                >
                  Aikar's Flags (G1GC)
                </button>
                <button
                  type="button"
                  className={`btn btn-xs ${editJvmArgs.includes("UseShenandoahGC") ? "btn-primary" : "btn-ghost"}`}
                  title="Shenandoah GC — ультранизкие задержки пауз сборщика (для Java 17+)"
                  onClick={() => setEditJvmArgs("-XX:+UseShenandoahGC -XX:+UnlockExperimentalVMOptions -XX:ShenandoahGCHeuristics=adaptive -XX:+AlwaysPreTouch")}
                >
                  Shenandoah GC (Java 17+)
                </button>
                <button
                  type="button"
                  className={`btn btn-xs ${editJvmArgs.includes("UseZGC") ? "btn-primary" : "btn-ghost"}`}
                  title="ZGC — сборщик с микропаузами для систем с 8+ ГБ памяти (для Java 17+)"
                  onClick={() => setEditJvmArgs("-XX:+UseZGC -XX:+UnlockExperimentalVMOptions -XX:+AlwaysPreTouch")}
                >
                  ZGC (Java 17+)
                </button>
                {editJvmArgs && (
                  <button
                    type="button"
                    className="btn btn-ghost btn-xs"
                    onClick={() => setEditJvmArgs("")}
                  >
                    Сбросить
                  </button>
                )}
              </div>
            </div>

            <label className="chip-toggle">
              <input type="checkbox" checked={editFullscreen} onChange={(e) => setEditFullscreen(e.target.checked)} />
              <span>Запускать в полноэкранном режиме</span>
            </label>

          </div>
        )}

        <DuplicateInstanceModal
          source={dupTarget}
          onClose={() => setDupTarget(null)}
          onDuplicate={onDuplicate}
          onToast={onToast}
        />
        <DeleteInstanceModal
          target={delTarget}
          onClose={() => setDelTarget(null)}
          onDelete={onDelete}
          onToast={onToast}
        />
        {exportTarget?.id === selected.id && (
          <ExportModpackModal instance={exportTarget} onClose={() => setExportTarget(null)} onToast={onToast} />
        )}
      </div>
    );
  }

  // ========================================================
  // PROFILE LIST VIEW
  // ========================================================
  return (
    <div className="mh-page mh-page-enter">
      <header className="mh-page-head-row">
        <div>
          <h1 className="mh-page-title">Игровые профили</h1>
          <p className="mh-page-subtitle">
            Независимые установки Minecraft с изолированными модами, конфигами и сохранениями, но общими библиотеками и Java Runtime.
          </p>
        </div>
        <div style={{ display: "flex", gap: "8px", alignItems: "center" }}>
          <button type="button" className="btn btn-secondary btn-sm" onClick={() => setImportOpen(true)}>
            📦 Импорт сборки (.mrpack / .zip)
          </button>
          <button type="button" className="btn btn-primary btn-sm" onClick={() => setCreateOpen(true)}>
            + Создать профиль
          </button>
        </div>
      </header>

      {instances.length > 0 && (
        <div className="mh-inst-toolbar">
          <input
            type="search"
            className="text-input mh-inst-search"
            placeholder="Поиск по названию, версии MC, загрузчику..."
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <CustomSelect
            value={sortBy}
            options={SORT_OPTS}
            onChange={(v) => setSortBy(v as SortBy)}
            className="cs-auto"
            ariaLabel="Сортировка профилей"
          />
        </div>
      )}

      {loading && instances.length === 0 ? (
        <div className="empty-state">
          <span className="spinner" style={{ marginRight: 8, verticalAlign: "middle" }} />
          Загрузка профилей…
        </div>
      ) : instances.length === 0 ? (
        <section className="mh-panel mh-placeholder-card">
          <h2>У вас пока нет игровых профилей</h2>
          <p className="mh-placeholder-text">
            Создайте первый независимый профиль Minecraft: каждый профиль получает собственную папку <code>minecraft/</code> для модов, конфигураций, ресурс-паков, шейдеров и сохранений.
          </p>
          {paths && (
            <p className="mh-placeholder-text mono-num" style={{ fontSize: 12 }}>
              Хранилище профилей: {paths.instances_dir}
            </p>
          )}
          <div className="mh-placeholder-actions">
            <button type="button" className="btn btn-primary btn-sm" onClick={() => setCreateOpen(true)}>
              + Создать первый профиль
            </button>
            <button type="button" className="btn btn-secondary btn-sm" onClick={() => setImportOpen(true)}>
              📦 Импортировать готовую сборку (.mrpack, .zip)
            </button>
          </div>
        </section>
      ) : filteredInstances.length === 0 ? (
        <div className="empty-state">
          <h3>Профили не найдены</h3>
          <p>Попробуйте изменить поисковый запрос.</p>
        </div>
      ) : (
        <div className="mh-inst-grid">
          {filteredInstances.map((inst) => {
            const sb = statusBadge(inst.status);
            const sess = sessionsByInstance[inst.id] || null;
            const instRunning = isSessionActive(sess?.state);
            const pMeta = processStateMeta(sess?.state, sess?.game_loaded);
            return (
              <article
                key={inst.id}
                className="mh-inst-card"
                onClick={() => {
                  setSelectedId(inst.id);
                  setDetailTab("mods");
                }}
              >
                <div className="mh-inst-card-head">
                  <InstanceIcon icon={inst.icon} name={inst.name} size={46} />
                  <div className="mh-inst-card-title-wrap">
                    <h3 className="mh-inst-card-title" title={inst.name}>{inst.name}</h3>
                    <div className="mh-inst-card-sub mono-num">
                      <span>{inst.mc_version ? `MC ${inst.mc_version}` : "Без версии"}</span>
                      <span>• {typeLabel(inst.instance_type)}</span>
                    </div>
                  </div>
                  <span className={`mh-dl-status-pill ${activeInstallsById[inst.id] ? "is-active" : sb.cls}`}>
                    {activeInstallsById[inst.id]
                      ? activeInstallsById[inst.id].progress >= 0
                        ? `Загрузка ${Math.round(activeInstallsById[inst.id].progress)}%`
                        : "Скачивание…"
                      : sb.label}
                  </span>
                </div>

                {inst.description && <p className="mh-inst-card-desc">{inst.description}</p>}

                <div className="mh-inst-card-meta mono-num">
                  <span>Модов: {inst.mods_count}</span>
                  <span>• {inst.memory_max_mb} МБ</span>
                  <span>• {resolveAccountLabel(inst)}</span>
                  {sess && <span className={`mh-dl-status-pill ${pMeta.cls}`}>{pMeta.label}</span>}
                </div>

                <div className="mh-inst-card-foot" onClick={(e) => e.stopPropagation()}>
                  {!instRunning ? (
                    <button
                      type="button"
                      className="btn btn-primary btn-xs"
                      onClick={() => void handleLaunchProfile(inst, true)}
                    >
                      ▶ Играть
                    </button>
                  ) : (
                    <button
                      type="button"
                      className="btn btn-danger-ghost btn-xs"
                      onClick={() => void onStop(inst.id, false)}
                    >
                      ■ Стоп
                    </button>
                  )}
                  <button
                    type="button"
                    className="btn btn-ghost btn-xs"
                    onClick={() => {
                      setSelectedId(inst.id);
                      setDetailTab("mods");
                    }}
                  >
                    Открыть
                  </button>
                  {desktopBridge.isDesktop() && (
                    <button type="button" className="btn btn-ghost btn-xs" onClick={() => handleOpenDir(inst.id, "minecraft")}>
                      Папка
                    </button>
                  )}
                  <button type="button" className="btn btn-ghost btn-xs" onClick={() => setDupTarget(inst)}>
                    Копия
                  </button>
                  <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => setDelTarget(inst)}>
                    Удалить
                  </button>
                </div>
              </article>
            );
          })}
        </div>
      )}

      <CreateInstanceModal
        open={createOpen}
        mcVersions={mcVersions}
        onClose={() => setCreateOpen(false)}
        onCreate={async (req) => {
          const created = await onCreate(req);
          setSelectedId(created.id);
          setDetailTab("mods");
          return created;
        }}
        onToast={onToast}
      />

      <DuplicateInstanceModal
        source={dupTarget}
        onClose={() => setDupTarget(null)}
        onDuplicate={onDuplicate}
        onToast={onToast}
      />

      <DeleteInstanceModal
        target={delTarget}
        onClose={() => setDelTarget(null)}
        onDelete={onDelete}
        onToast={onToast}
      />

      <ImportModpackModal
        open={importOpen}
        onClose={() => setImportOpen(false)}
        onSuccess={async () => {
          await onRefresh();
        }}
        onToast={onToast}
      />
    </div>
  );
};
