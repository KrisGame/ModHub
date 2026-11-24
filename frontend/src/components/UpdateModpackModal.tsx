import React, { useEffect, useState } from "react";
import { api } from "../api";
import { useEscapeKey, useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type { ConflictResolution, ModpackView, UpdateDiffPreview } from "../types";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";

interface UpdateModpackModalProps {
  modpack: ModpackView | null;
  onClose: () => void;
  onUpdated: () => Promise<void>;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

const CONF_OPTS: CustomSelectOption[] = [
  { value: "keep_existing", label: "Сохранить пользовательские изменения конфигов" },
  { value: "backup_and_overwrite", label: "Создать .bak копию и обновить конфиги" },
  { value: "overwrite", label: "Перезаписать изменённые конфиги версией из сборки" },
];

export const UpdateModpackModal: React.FC<UpdateModpackModalProps> = ({ modpack, onClose, onUpdated, onToast }) => {
  const { mounted, shown } = useOverlayTransition(Boolean(modpack), 220);
  useEscapeKey(Boolean(modpack), onClose);
  const [loading, setLoading] = useState(false);
  const [applying, setApplying] = useState(false);
  const [diff, setDiff] = useState<UpdateDiffPreview | null>(null);
  const [errMsg, setErrMsg] = useState("");
  const [manualArchive, setManualArchive] = useState("");
  const [conflictMode, setConflictMode] = useState<ConflictResolution>("keep_existing");

  const runCheck = async (mp: ModpackView, archivePath = "") => {
    setLoading(true); setErrMsg(""); setDiff(null);
    try { setDiff(await api.previewModpackUpdate(mp.id, archivePath)); }
    catch (err) { setErrMsg(String((err as Error).message || err)); }
    finally { setLoading(false); }
  };

  useEffect(() => {
    if (!modpack) return;
    setManualArchive(""); setConflictMode("keep_existing");
    void runCheck(modpack, "");
  }, [modpack?.id]);

  if (!mounted || !modpack) return null;

  const handleApply = async () => {
    if (!diff || applying) return;
    setApplying(true);
    try {
      await api.applyModpackUpdate({ modpack_id: modpack.id, preview_id: diff.preview_id, config_conflict_mode: conflictMode, skip_unavailable: true });
      onToast(`Сборка «${modpack.name}» обновлена до версии ${diff.new_version}`, "ok");
      await onUpdated();
      onClose();
    } catch (err) {
      onToast(`Ошибка обновления: ${(err as Error).message}`, "err");
    } finally {
      setApplying(false);
    }
  };

  return (
    <div className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`} role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal-window modal-lg mh-wizard-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div><h2 className="modal-title">Обновление сборки «{modpack.name}»</h2><p className="modal-subtitle mono-num">Текущая версия: {modpack.version || "1.0.0"} • Источник: {modpack.source_type}</p></div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}><CloseIcon size={15} /></button>
        </div>
        <div className="modal-body mh-form-stack">
          <div className="mh-form-field">
            <label>Обновить из нового локального архива (.mrpack / .zip / .modhub)</label>
            <div className="input-action-row">
              <input type="text" className="text-input mono-num" placeholder="Выберите новую версию архива..." value={manualArchive} onChange={(e) => setManualArchive(e.target.value)} />
              {desktopBridge.isDesktop() && <button type="button" className="btn btn-ghost btn-sm" onClick={async () => { const p = await desktopBridge.browseModpackArchive(); if (p) { setManualArchive(p); void runCheck(modpack, p); } }}>Обзор…</button>}
              <button type="button" className="btn btn-ghost btn-sm" disabled={!manualArchive.trim() || loading} onClick={() => void runCheck(modpack, manualArchive.trim())}>Сравнить</button>
            </div>
          </div>
          {loading && <div className="mh-panel-empty">Сравнение манифестов и пользовательских изменений…</div>}
          {errMsg && <div className="mh-dl-error-note">{errMsg}</div>}
          {diff && !diff.source_supported && <div className="mh-inst-issue is-warn">{diff.source_note}</div>}
          {diff && diff.source_supported && (
            <>
              <div className="mh-data-dir-box mono-num">
                <div><strong>Версия:</strong> {diff.current_version} → {diff.new_version}</div>
                <div><strong>Изменения:</strong> +{diff.added.length} • ↻ {diff.updated.length} • − {diff.removed.length} • ⚠ {diff.config_conflicts.length} • ✓ {diff.preserved_user.length} пользовательских</div>
              </div>
              {!diff.has_update ? (
                <div className="mh-inst-ok-note">Установленная сборка уже актуальна.</div>
              ) : (
                <>
                  {diff.config_conflicts.length > 0 && (
                    <div className="mh-form-field"><label>Конфликты пользовательских конфигураций</label><CustomSelect value={conflictMode} options={CONF_OPTS} onChange={(v) => setConflictMode(v as ConflictResolution)} ariaLabel="Конфликты конфигов" /></div>
                  )}
                  <div className="mh-loader-ver-list mono-num" style={{ maxHeight: 200 }}>
                    {diff.added.map((e) => <div key={`a-${e.rel_path}`} className="mh-ver-row" style={{ height: "auto", padding: "4px 10px", cursor: "default" }}><span className="pill pill-installed">+ Добавлен</span><span style={{ fontSize: 12 }}>{e.rel_path}</span></div>)}
                    {diff.updated.map((e) => <div key={`u-${e.rel_path}`} className="mh-ver-row" style={{ height: "auto", padding: "4px 10px", cursor: "default" }}><span className="pill">↻ Обновлён</span><span style={{ fontSize: 12 }}>{e.rel_path}</span></div>)}
                    {diff.removed.map((e) => <div key={`r-${e.rel_path}`} className="mh-ver-row" style={{ height: "auto", padding: "4px 10px", cursor: "default" }}><span className="pill pill-alpha">− Удалён</span><span style={{ fontSize: 12 }}>{e.rel_path}</span></div>)}
                    {diff.config_conflicts.map((e) => <div key={`c-${e.rel_path}`} className="mh-ver-row" style={{ height: "auto", padding: "4px 10px", cursor: "default" }}><span className="pill">⚠ Конфликт</span><span style={{ fontSize: 12 }}>{e.rel_path}</span></div>)}
                    {diff.preserved_user.map((e) => <div key={`p-${e.rel_path}`} className="mh-ver-row" style={{ height: "auto", padding: "4px 10px", cursor: "default" }}><span className="pill">✓ Пользовательский</span><span style={{ fontSize: 12 }}>{e.rel_path}</span></div>)}
                  </div>
                </>
              )}
            </>
          )}
          <div className="mh-modal-actions-end">
            <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Закрыть</button>
            {diff && diff.source_supported && diff.has_update && (
              <button type="button" className={`btn btn-primary btn-sm${applying ? " is-loading" : ""}`} disabled={applying} onClick={handleApply}>{applying ? "Применение…" : "Применить обновление"}</button>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};
