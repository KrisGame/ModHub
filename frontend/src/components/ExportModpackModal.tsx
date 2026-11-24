import React, { useEffect, useState } from "react";
import { api } from "../api";
import { useEscapeKey, useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import type { ModpackFormat } from "../types";
import { formatBytes } from "../utils";

export interface ExportTarget {
  sourceType: "instance" | "pack";
  sourceId: string;
  defaultName: string;
  defaultDescription?: string;
}

interface ExportModpackModalProps {
  target: ExportTarget | null;
  onClose: () => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

export const ExportModpackModal: React.FC<ExportModpackModalProps> = ({ target, onClose, onToast }) => {
  const { mounted, shown } = useOverlayTransition(Boolean(target), 220);
  useEscapeKey(Boolean(target), onClose);
  const [format, setFormat] = useState<ModpackFormat>("modhub");
  const [name, setName] = useState("");
  const [author, setAuthor] = useState("");
  const [version, setVersion] = useState("1.0.0");
  const [description, setDescription] = useState("");
  const [incMods, setIncMods] = useState(true);
  const [incConfigs, setIncConfigs] = useState(true);
  const [incRP, setIncRP] = useState(true);
  const [incShaders, setIncShaders] = useState(true);
  const [incDP, setIncDP] = useState(true);
  const [incScreenshots, setIncScreenshots] = useState(false);
  const [incSaves, setIncSaves] = useState(false);
  const [incExtra, setIncExtra] = useState(false);
  const [busy, setBusy] = useState(false);
  const [warnings, setWarnings] = useState<string[]>([]);

  useEffect(() => {
    if (!target) return;
    setFormat("modhub"); setName(target.defaultName); setAuthor(""); setVersion("1.0.0"); setDescription(target.defaultDescription || "");
    setIncMods(true); setIncConfigs(true); setIncRP(true); setIncShaders(true); setIncDP(true);
    setIncScreenshots(false); setIncSaves(false); setIncExtra(false); setWarnings([]); setBusy(false);
  }, [target?.sourceType, target?.sourceId]);

  if (!mounted || !target) return null;

  const handleExport = async (e: React.FormEvent) => {
    e.preventDefault();
    setBusy(true);
    setWarnings([]);
    try {
      const res = await api.exportModpack({
        source_type: target.sourceType, source_id: target.sourceId, format, name: name.trim() || target.defaultName,
        author: author.trim(), description: description.trim(), version: version.trim() || "1.0.0",
        include_mods: incMods, include_configs: incConfigs, include_resourcepacks: incRP, include_shaders: incShaders,
        include_datapacks: incDP, include_screenshots: incScreenshots, include_saves: incSaves, include_extra_files: incExtra,
      });
      if (res.cancelled) return;
      if (res.warnings?.length) setWarnings(res.warnings);
      onToast(`Сборка экспортирована (${res.total_files} файлов, ${formatBytes(res.size_bytes)}): ${res.output_path}`, "ok");
      if (!res.warnings?.length) onClose();
    } catch (err) {
      onToast(`Ошибка экспорта: ${(err as Error).message}`, "err");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`} role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div><h2 className="modal-title">Экспорт сборки</h2><p className="modal-subtitle">Выберите формат (.modhub, .mrpack или CurseForge .zip) и состав.</p></div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}><CloseIcon size={15} /></button>
        </div>
        <form className="modal-body mh-form-stack" onSubmit={handleExport}>
          <div className="mh-loader-grid">
            {([
              { id: "modhub", title: "ModHub (.modhub)", desc: "Полные метаданные, хэши, источники и параметры профиля" },
              { id: "mrpack", title: "Modrinth (.mrpack)", desc: "Официальный modrinth.index.json + overrides/" },
              { id: "curseforge", title: "CurseForge (.zip)", desc: "Официальный manifest.json + overrides/" },
            ] as Array<{ id: ModpackFormat; title: string; desc: string }>).map((opt) => (
              <button key={opt.id} type="button" className={`mh-loader-card${format === opt.id ? " is-selected" : ""}`} onClick={() => setFormat(opt.id)}>
                <strong>{opt.title}</strong><span>{opt.desc}</span>
              </button>
            ))}
          </div>
          <div className="mh-form-grid-2">
            <div className="mh-form-field"><label>Название</label><input type="text" className="text-input" value={name} onChange={(e) => setName(e.target.value)} /></div>
            <div className="mh-form-field"><label>Версия сборки</label><input type="text" className="text-input mono-num" value={version} onChange={(e) => setVersion(e.target.value)} /></div>
          </div>
          <div className="mh-form-grid-2">
            <div className="mh-form-field"><label>Автор</label><input type="text" className="text-input" value={author} onChange={(e) => setAuthor(e.target.value)} /></div>
            <div className="mh-form-field"><label>Описание</label><input type="text" className="text-input" value={description} onChange={(e) => setDescription(e.target.value)} /></div>
          </div>
          <div className="mh-form-field">
            <label>Что включить в экспорт</label>
            <div className="mh-check-grid">
              <label className="chip-toggle"><input type="checkbox" checked={incMods} onChange={(e) => setIncMods(e.target.checked)} /><span>Моды (mods/)</span></label>
              <label className="chip-toggle"><input type="checkbox" checked={incConfigs} onChange={(e) => setIncConfigs(e.target.checked)} /><span>Конфигурации (config/)</span></label>
              <label className="chip-toggle"><input type="checkbox" checked={incRP} onChange={(e) => setIncRP(e.target.checked)} /><span>Ресурс-паки</span></label>
              <label className="chip-toggle"><input type="checkbox" checked={incShaders} onChange={(e) => setIncShaders(e.target.checked)} /><span>Шейдеры</span></label>
              <label className="chip-toggle"><input type="checkbox" checked={incDP} onChange={(e) => setIncDP(e.target.checked)} /><span>Датапаки</span></label>
              <label className="chip-toggle"><input type="checkbox" checked={incExtra} onChange={(e) => setIncExtra(e.target.checked)} /><span>Доп. файлы (kubejs и др.)</span></label>
              <label className="chip-toggle"><input type="checkbox" checked={incScreenshots} onChange={(e) => setIncScreenshots(e.target.checked)} /><span>Скриншоты (тяжёлые)</span></label>
              <label className="chip-toggle"><input type="checkbox" checked={incSaves} onChange={(e) => setIncSaves(e.target.checked)} /><span>Миры / saves (тяжёлые)</span></label>
            </div>
          </div>
          <div className="mh-info-callout">Токены авторизации Microsoft, пароли, файлы аккаунтов и секреты автоматически исключаются.</div>
          {warnings.length > 0 && <div className="mh-inst-issue is-warn">{warnings.map((w, i) => <div key={i}>• {w}</div>)}</div>}
          <div className="mh-modal-actions-end">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Закрыть</button>
            <button type="submit" className={`btn btn-primary btn-sm${busy ? " is-loading" : ""}`} disabled={busy}>{busy ? "Экспорт…" : "Экспортировать"}</button>
          </div>
        </form>
      </div>
    </div>
  );
};
