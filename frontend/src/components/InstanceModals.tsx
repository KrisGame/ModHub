import React, { useEffect, useMemo, useState } from "react";
import { useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type {
  CreateInstanceInput,
  DeleteInstanceInput,
  DuplicateInstanceInput,
  GameInstance,
  InstanceType,
} from "../types";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";
import { INSTANCE_ICON_PRESETS, InstanceIcon } from "./InstanceIcon";

const TYPE_OPTIONS: CustomSelectOption[] = [
  { value: "empty", label: "Пустой профиль (без версии MC)" },
  { value: "vanilla", label: "Vanilla (Обычный Minecraft)" },
  { value: "neoforge", label: "NeoForge" },
  { value: "fabric", label: "Fabric" },
  { value: "forge", label: "Forge" },
  { value: "quilt", label: "Quilt" },
];

interface CreateInstanceModalProps {
  open: boolean;
  mcVersions: string[];
  onClose: () => void;
  onCreate: (req: CreateInstanceInput) => Promise<GameInstance>;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

export const CreateInstanceModal: React.FC<CreateInstanceModalProps> = ({
  open,
  mcVersions,
  onClose,
  onCreate,
  onToast,
}) => {
  const { mounted, shown } = useOverlayTransition(open, 220);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [icon, setIcon] = useState("grass");
  const [instanceType, setInstanceType] = useState<InstanceType>("vanilla");
  const [mcVersion, setMcVersion] = useState("");
  const [loaderVersion, setLoaderVersion] = useState("");
  const [memoryMax, setMemoryMax] = useState(4096);
  const [customDir, setCustomDir] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!open) return;
    setError("");
    setBusy(false);
    setCustomDir("");
    setLoaderVersion("");
    setMemoryMax(4096);
    setIcon("grass");
    setName("");
    setDescription("");
    setMcVersion(mcVersions[0] || "");
    setInstanceType(mcVersions.length > 0 ? "vanilla" : "empty");
  }, [open, mcVersions]);

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

  const mcOptions = useMemo<CustomSelectOption[]>(
    () => [
      { value: "", label: "Не выбрана (пустая заготовка)" },
      ...mcVersions.map((v) => ({ value: v, label: `Minecraft ${v}` })),
    ],
    [mcVersions]
  );

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    const cleanName = name.trim();
    if (!cleanName) {
      setError("Введите название профиля");
      return;
    }
    setBusy(true);
    setError("");
    try {
      await onCreate({
        name: cleanName,
        description: description.trim(),
        icon,
        mc_version: instanceType === "empty" ? "" : mcVersion,
        instance_type: instanceType,
        loader_version: instanceType === "empty" || instanceType === "vanilla" ? "" : loaderVersion.trim(),
        memory_min_mb: Math.min(512, memoryMax),
        memory_max_mb: memoryMax,
        custom_dir: customDir.trim(),
      });
      onClose();
    } catch (err) {
      const msg = String((err as Error).message || err);
      setError(msg);
      onToast(msg, "err");
    } finally {
      setBusy(false);
    }
  };

  if (!mounted) return null;

  return (
    <div
      className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-hidden={!shown}
      onClick={onClose}
    >
      <div className="modal-window modal-md mh-create-instance-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <InstanceIcon icon={icon} name={name || "M"} size={42} />
            <div>
              <h2 className="modal-title">Создать игровой профиль</h2>
              <p className="modal-subtitle">Независимая установка Minecraft с собственной папкой, модами и сохранениями.</p>
            </div>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <form className="modal-body mh-form-stack mh-create-instance-form" onSubmit={handleSubmit}>
          <div className="mh-form-field">
            <label htmlFor="instCreateName">Название профиля *</label>
            <input
              id="instCreateName"
              type="text"
              className="text-input"
              placeholder="Например: RPG 1.21.1 NeoForge"
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoFocus
            />
          </div>

          <div className="mh-form-field">
            <label htmlFor="instCreateDesc">Описание</label>
            <input
              id="instCreateDesc"
              type="text"
              className="text-input"
              placeholder="Краткое описание сборки или назначения профиля..."
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>

          <div className="mh-form-field">
            <label>Иконка профиля</label>
            <div className="mh-icon-picker-row">
              {INSTANCE_ICON_PRESETS.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className={`mh-icon-pick-btn${icon === p.id ? " is-active" : ""}`}
                  title={p.label}
                  onClick={() => setIcon(p.id)}
                >
                  <InstanceIcon icon={p.id} name={name || p.symbol} size={30} />
                </button>
              ))}
            </div>
          </div>

          <div className="mh-form-grid-2">
            <div className="mh-form-field">
              <label>Тип профиля</label>
              <CustomSelect
                value={instanceType}
                options={TYPE_OPTIONS}
                onChange={(v) => setInstanceType(v as InstanceType)}
                ariaLabel="Тип профиля"
              />
            </div>

            <div className="mh-form-field">
              <label>Версия Minecraft</label>
              <CustomSelect
                value={instanceType === "empty" ? "" : mcVersion}
                options={mcOptions}
                onChange={(v) => {
                  setMcVersion(v);
                  if (v && instanceType === "empty") setInstanceType("vanilla");
                }}
                searchable
                searchPlaceholder="Найти версию..."
                mono
                disabled={instanceType === "empty"}
                ariaLabel="Версия Minecraft"
              />
            </div>
          </div>

          {instanceType !== "empty" && instanceType !== "vanilla" && (
            <div className="mh-form-field">
              <label htmlFor="instLoaderVer">Версия загрузчика (опционально)</label>
              <input
                id="instLoaderVer"
                type="text"
                className="text-input mono-num"
                placeholder="Оставьте пустым для автоподбора рекомендуемой версии"
                value={loaderVersion}
                onChange={(e) => setLoaderVersion(e.target.value)}
              />
            </div>
          )}


          <div className="mh-form-field">
            <div className="mh-form-label-row">
              <label htmlFor="instMemRange">Оперативная память (Xmx)</label>
              <span className="mono-num mh-mem-badge">{memoryMax} МБ ({(memoryMax / 1024).toFixed(1)} ГБ)</span>
            </div>
            <div className="range-row">
              <input
                id="instMemRange"
                type="range"
                min={1024}
                max={16384}
                step={512}
                value={memoryMax}
                onChange={(e) => setMemoryMax(Number(e.target.value))}
              />
            </div>
          </div>

          <div className="mh-form-field">
            <label htmlFor="instCustomDir">Своя папка хранения (опционально)</label>
            <div className="input-action-row">
              <input
                id="instCustomDir"
                type="text"
                className="text-input mono-num"
                placeholder="По умолчанию: data/instances/<id>"
                value={customDir}
                onChange={(e) => setCustomDir(e.target.value)}
              />
              {desktopBridge.isDesktop() && (
                <button
                  type="button"
                  className="btn btn-ghost btn-sm"
                  onClick={async () => {
                    try {
                      const chosen = await desktopBridge.browseInstanceFolder();
                      if (chosen) setCustomDir(chosen);
                    } catch (err) {
                      onToast(`Ошибка выбора папки: ${(err as Error).message}`, "err");
                    }
                  }}
                >
                  Обзор…
                </button>
              )}
            </div>
          </div>

          <div className="mh-info-callout">
            {instanceType === "empty" || !mcVersion
              ? "Будет создана пустая заготовка профиля — версию Minecraft и загрузчик можно установить позже."
              : "Сразу после создания профиля автоматически запустятся скачивание клиента Minecraft, ресурсов, библиотек и загрузчика (без Java)."}
          </div>

          {error && <div className="mh-dl-error-note">{error}</div>}

          <div className="mh-modal-actions-end">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose} disabled={busy}>
              Отмена
            </button>
            <button type="submit" className={`btn btn-primary btn-sm${busy ? " is-loading" : ""}`} disabled={busy}>
              {busy ? "Создание…" : "Создать профиль"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

interface DuplicateInstanceModalProps {
  source: GameInstance | null;
  onClose: () => void;
  onDuplicate: (req: DuplicateInstanceInput) => Promise<GameInstance>;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

export const DuplicateInstanceModal: React.FC<DuplicateInstanceModalProps> = ({
  source,
  onClose,
  onDuplicate,
  onToast,
}) => {
  const open = Boolean(source);
  const { mounted, shown } = useOverlayTransition(open, 220);
  const [newName, setNewName] = useState("");
  const [copySettings, setCopySettings] = useState(true);
  const [copyMods, setCopyMods] = useState(true);
  const [copyConfigs, setCopyConfigs] = useState(true);
  const [copyResourcePacks, setCopyResourcePacks] = useState(true);
  const [copyShaderPacks, setCopyShaderPacks] = useState(true);
  const [copySaves, setCopySaves] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!source) return;
    setNewName(`${source.name} (Копия)`);
    setCopySettings(true);
    setCopyMods(true);
    setCopyConfigs(true);
    setCopyResourcePacks(true);
    setCopyShaderPacks(true);
    setCopySaves(false);
    setBusy(false);
  }, [source?.id]);

  if (!mounted || !source) return null;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    try {
      await onDuplicate({
        source_id: source.id,
        new_name: newName.trim() || `${source.name} (Копия)`,
        copy_settings: copySettings,
        copy_mods: copyMods,
        copy_configs: copyConfigs,
        copy_resourcepacks: copyResourcePacks,
        copy_shaderpacks: copyShaderPacks,
        copy_saves: copySaves,
      });
      onClose();
    } catch (err) {
      onToast(`Ошибка дублирования: ${(err as Error).message}`, "err");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-hidden={!shown}
      onClick={onClose}
    >
      <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title">Дублировать профиль</h2>
            <p className="modal-subtitle">Будет создана физически независимая копия профиля «{source.name}» с новым ID.</p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <form className="modal-body mh-form-stack" onSubmit={handleSubmit}>
          <div className="mh-form-field">
            <label htmlFor="dupInstName">Название копии</label>
            <input
              id="dupInstName"
              type="text"
              className="text-input"
              value={newName}
              onChange={(e) => setNewName(e.target.value)}
              autoFocus
            />
          </div>

          <div className="mh-form-field">
            <label>Какие данные скопировать</label>
            <div className="mh-check-grid">
              <label className="chip-toggle">
                <input type="checkbox" checked={copySettings} onChange={(e) => setCopySettings(e.target.checked)} />
                <span>Настройки и options.txt</span>
              </label>
              <label className="chip-toggle">
                <input type="checkbox" checked={copyMods} onChange={(e) => setCopyMods(e.target.checked)} />
                <span>Моды (mods/)</span>
              </label>
              <label className="chip-toggle">
                <input type="checkbox" checked={copyConfigs} onChange={(e) => setCopyConfigs(e.target.checked)} />
                <span>Конфигурации (config/)</span>
              </label>
              <label className="chip-toggle">
                <input type="checkbox" checked={copyResourcePacks} onChange={(e) => setCopyResourcePacks(e.target.checked)} />
                <span>Ресурс-паки</span>
              </label>
              <label className="chip-toggle">
                <input type="checkbox" checked={copyShaderPacks} onChange={(e) => setCopyShaderPacks(e.target.checked)} />
                <span>Шейдеры</span>
              </label>
              <label className="chip-toggle">
                <input type="checkbox" checked={copySaves} onChange={(e) => setCopySaves(e.target.checked)} />
                <span>Сохранения (saves/)</span>
              </label>
            </div>
          </div>

          <div className="mh-modal-actions-end">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose} disabled={busy}>
              Отмена
            </button>
            <button type="submit" className={`btn btn-primary btn-sm${busy ? " is-loading" : ""}`} disabled={busy}>
              {busy ? "Копирование…" : "Создать копию"}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};

interface DeleteInstanceModalProps {
  target: GameInstance | null;
  onClose: () => void;
  onDelete: (req: DeleteInstanceInput) => Promise<void>;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

export const DeleteInstanceModal: React.FC<DeleteInstanceModalProps> = ({
  target,
  onClose,
  onDelete,
  onToast,
}) => {
  const open = Boolean(target);
  const { mounted, shown } = useOverlayTransition(open, 220);
  const [deleteFiles, setDeleteFiles] = useState(false);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    if (!target) return;
    setDeleteFiles(false);
    setBusy(false);
  }, [target?.id]);

  if (!mounted || !target) return null;

  const handleConfirm = async () => {
    setBusy(true);
    try {
      await onDelete({ id: target.id, delete_files: deleteFiles });
      onClose();
    } catch (err) {
      onToast(`Ошибка удаления: ${(err as Error).message}`, "err");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-hidden={!shown}
      onClick={onClose}
    >
      <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title">Удалить профиль «{target.name}»?</h2>
            <p className="modal-subtitle mono-num">{target.root_dir}</p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <div className="modal-body mh-form-stack">
          <label className="chip-toggle chip-block">
            <input
              type="radio"
              name="delMode"
              checked={!deleteFiles}
              onChange={() => setDeleteFiles(false)}
            />
            <span>Удалить только запись профиля из ModHub (файлы и сохранения останутся на диске)</span>
          </label>
          <label className="chip-toggle chip-block">
            <input
              type="radio"
              name="delMode"
              checked={deleteFiles}
              onChange={() => setDeleteFiles(true)}
            />
            <span>Удалить профиль вместе с его папкой, модами и сохранениями</span>
          </label>

          <div className="mh-info-callout">
            Общие библиотеки, кэш загрузок, Java Runtime и остальные профили гарантированно не будут затронуты.
          </div>

          <div className="mh-modal-actions-end">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose} disabled={busy}>
              Отмена
            </button>
            <button
              type="button"
              className={`btn btn-danger-ghost btn-sm${busy ? " is-loading" : ""}`}
              disabled={busy}
              onClick={handleConfirm}
            >
              {busy ? "Удаление…" : deleteFiles ? "Удалить профиль и файлы" : "Удалить запись"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
