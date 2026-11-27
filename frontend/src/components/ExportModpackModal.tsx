import React, { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { api } from "../api";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type { ExportModpackInput, GameInstance, ModpackFormat } from "../types";
import { formatBytes } from "../utils";

interface ExportModpackModalProps {
  instance: GameInstance;
  onClose: () => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

const FORMATS: Array<{ value: ModpackFormat; label: string; extension: string }> = [
  { value: "modhub", label: "ModHub (.modhub) — сохраняет все выбранные файлы", extension: ".modhub" },
  { value: "mrpack", label: "Modrinth (.mrpack) — моды скачиваются по ссылкам", extension: ".mrpack" },
  { value: "curseforge", label: "CurseForge (.zip) — моды скачиваются по ссылкам", extension: ".zip" },
];

export const ExportModpackModal: React.FC<ExportModpackModalProps> = ({ instance, onClose, onToast }) => {
  const [format, setFormat] = useState<ModpackFormat>("modhub");
  const [author, setAuthor] = useState("");
  const [description, setDescription] = useState(instance.description || "");
  const [version, setVersion] = useState("1.0.0");
  const [includeMods, setIncludeMods] = useState(true);
  const [includeConfigs, setIncludeConfigs] = useState(true);
  const [includeResourcePacks, setIncludeResourcePacks] = useState(true);
  const [includeShaders, setIncludeShaders] = useState(true);
  const [includeDatapacks, setIncludeDatapacks] = useState(true);
  const [includeScreenshots, setIncludeScreenshots] = useState(false);
  const [includeSaves, setIncludeSaves] = useState(false);
  const [includeExtraFiles, setIncludeExtraFiles] = useState(false);
  const [busy, setBusy] = useState(false);
  const dialogRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const previousFocus = document.activeElement;
    dialogRef.current?.focus();
    return () => {
      if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus();
    };
  }, []);

  const exportPack = async () => {
    if (busy) return;
    setBusy(true);
    try {
      const req: ExportModpackInput = {
        source_type: "instance", source_id: instance.id, format,
        name: instance.name, author: author.trim(), description: description.trim(), version: version.trim() || "1.0.0",
        include_mods: includeMods, include_configs: includeConfigs, include_resourcepacks: includeResourcePacks,
        include_shaders: includeShaders, include_datapacks: includeDatapacks,
        include_screenshots: format === "modhub" && includeScreenshots,
        include_saves: format === "modhub" && includeSaves,
        include_extra_files: format === "modhub" && includeExtraFiles,
      };
      const result = await api.exportModpack(req);
      if (result.cancelled) return;
      const warnings = result.warnings?.length ? ` Предупреждения: ${result.warnings.join("; ")}` : "";
      onToast(`Сборка экспортирована: ${result.output_path || "архив готов"} • ${result.total_files} файлов • ${formatBytes(result.size_bytes)}.${warnings}`, "ok");
      onClose();
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      setBusy(false);
    }
  };

  const toggle = (label: string, checked: boolean, onChange: (value: boolean) => void) => (
    <label style={{ display: "flex", alignItems: "center", gap: 8 }}><input type="checkbox" checked={checked} onChange={(e) => onChange(e.target.checked)} />{label}</label>
  );

  return createPortal(
    <div className="modal-backdrop overlay-animated is-open mh-export-modal-backdrop" onClick={() => !busy && onClose()}>
      <div
        ref={dialogRef}
        className="modal-window modal-md mh-export-modal"
        role="dialog"
        aria-modal="true"
        aria-labelledby="export-pack-title"
        tabIndex={-1}
        onClick={(event) => event.stopPropagation()}
        onKeyDown={(event) => {
          if (event.key !== "Escape") return;
          event.stopPropagation();
          if (!busy) onClose();
        }}
      >
        <div className="modal-header">
          <div><h2 id="export-pack-title" className="modal-title">Экспорт сборки</h2><p className="modal-subtitle">Профиль «{instance.name}»</p></div>
          <button type="button" className="icon-close" aria-label="Закрыть" disabled={busy} onClick={onClose}><CloseIcon size={15} /></button>
        </div>
        <div className="modal-body mh-form-stack">
          <div className="mh-form-field"><label htmlFor="export-pack-format">Формат</label><select id="export-pack-format" className="text-input" value={format} onChange={(e) => setFormat(e.target.value as ModpackFormat)}>{FORMATS.map((f) => <option key={f.value} value={f.value}>{f.label}</option>)}</select></div>
          <div className="mh-form-field"><label htmlFor="export-pack-author">Автор (необязательно)</label><input id="export-pack-author" className="text-input" value={author} onChange={(e) => setAuthor(e.target.value)} /></div>
          <div className="mh-form-field"><label htmlFor="export-pack-description">Описание</label><textarea id="export-pack-description" className="text-input" rows={2} value={description} onChange={(e) => setDescription(e.target.value)} /></div>
          <div className="mh-form-field"><label htmlFor="export-pack-version">Версия сборки</label><input id="export-pack-version" className="text-input" value={version} onChange={(e) => setVersion(e.target.value)} /></div>
          <fieldset disabled={busy} className="mh-form-stack" style={{ border: 0, padding: 0, margin: 0 }}><legend>Содержимое</legend>
            {toggle("Моды", includeMods, setIncludeMods)}{toggle("Конфигурации", includeConfigs, setIncludeConfigs)}
            {toggle("Ресурспаки", includeResourcePacks, setIncludeResourcePacks)}{toggle("Шейдеры", includeShaders, setIncludeShaders)}
            {toggle("Datapacks", includeDatapacks, setIncludeDatapacks)}
            {format === "modhub" && <>{toggle("Скриншоты", includeScreenshots, setIncludeScreenshots)}{toggle("Миры (сохранения)", includeSaves, setIncludeSaves)}{toggle("Прочие файлы", includeExtraFiles, setIncludeExtraFiles)}</>}
          </fieldset>
          {format !== "modhub" && <p className="mh-muted-meta">Для Modrinth/CurseForge экспорт включайте только моды и datapacks, которые имеют идентификаторы платформы. Локальные конфиги и файлы попадут в overrides только если они есть в профиле. Пропуски будут указаны в предупреждениях.</p>}
          {!desktopBridge.isDesktop() && <p className="mh-muted-meta">После экспорта архив будет загружен браузером.</p>}
        </div>
        <div className="mh-export-modal-actions">
          <button type="button" className="btn btn-secondary" disabled={busy} onClick={onClose}>Отмена</button>
          <button type="button" className={`btn btn-primary${busy ? " is-loading" : ""}`} disabled={busy} onClick={() => void exportPack()}>{busy ? "Экспорт…" : "Экспортировать"}</button>
        </div>
      </div>
    </div>,
    document.body,
  );
};
