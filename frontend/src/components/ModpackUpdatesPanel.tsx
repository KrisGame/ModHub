import React, { useEffect, useState } from "react";
import { api } from "../api";
import type { ModpackView, UpdateDiffPreview } from "../types";

interface Props { onToast: (text: string, kind?: "info" | "ok" | "err") => void }

export const ModpackUpdatesPanel: React.FC<Props> = ({ onToast }) => {
  const [packs, setPacks] = useState<ModpackView[]>([]);
  const [preview, setPreview] = useState<UpdateDiffPreview | null>(null);
  const [busy, setBusy] = useState("");
  const [loadError, setLoadError] = useState(false);
  const refresh = async () => {
    try { setPacks(await api.listModpacks()); setLoadError(false); }
    catch { setLoadError(true); }
  };
  useEffect(() => { void refresh(); }, []);
  if (!packs.length && !loadError) return null;

  const inspect = async (pack: ModpackView) => {
    setBusy(pack.id);
    setPreview(null);
    try {
      const diff = await api.previewModpackUpdate(pack.id);
      setPreview(diff);
      if (!diff.has_update) onToast(`Для «${pack.name}» обновлений не найдено`, "info");
    } catch (err) { onToast(`Не удалось проверить «${pack.name}»: ${(err as Error).message}`, "err"); }
    finally { setBusy(""); }
  };
  const unavailable = preview?.unavailable_files?.length ?? 0;
  const apply = async (mode: "keep_existing" | "backup_and_overwrite") => {
    if (!preview || unavailable > 0) return;
    setBusy(preview.modpack_id);
    try {
      await api.applyModpackUpdate({ modpack_id: preview.modpack_id, preview_id: preview.preview_id, skip_unavailable: false, config_conflict_mode: mode });
      onToast("Обновление сборки применено", "ok"); setPreview(null); await refresh();
    } catch (err) { onToast(`Не удалось применить обновление: ${(err as Error).message}`, "err"); }
    finally { setBusy(""); }
  };
  const remove = async (pack: ModpackView) => {
    if (!window.confirm(`Удалить запись об импортированной сборке «${pack.name}»? Файлы профиля не удаляются.`)) return;
    setBusy(pack.id);
    try { await api.deleteModpackRecord(pack.id); onToast("Запись сборки удалена; файлы профиля сохранены", "ok"); await refresh(); }
    catch (err) { onToast(`Не удалось удалить запись: ${(err as Error).message}`, "err"); }
    finally { setBusy(""); }
  };

  return <section className="mh-panel"><div className="mh-panel-head"><h2>Импортированные сборки</h2></div>
    {loadError && <p role="alert">Не удалось загрузить список сборок. <button type="button" className="btn btn-secondary btn-xs" onClick={() => void refresh()}>Повторить</button></p>}
    <div className="mh-form-stack">{packs.map((pack) => <div key={pack.id} style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 10 }}>
      <span>{pack.name} <small>v{pack.version}</small></span>
      <div style={{ display: "flex", gap: 6 }}><button type="button" className="btn btn-secondary btn-xs" disabled={!!busy || !pack.can_check_remote} onClick={() => void inspect(pack)}>{busy === pack.id ? "Проверка…" : "Проверить обновления"}</button><button type="button" className="btn btn-danger-ghost btn-xs" disabled={!!busy} onClick={() => void remove(pack)}>Удалить запись</button></div>
    </div>)}</div>
    {packs.length > 0 && !packs.some((p) => p.can_check_remote) && <p className="help-text">Для этих сборок нет удалённого источника обновлений.</p>}
    {preview?.has_update && <div className="mh-form-stack" style={{ marginTop: 16 }}>
      <h3>Обновление: {preview.current_version} → {preview.new_version}</h3>
      <p className="help-text">Добавлено: {preview.added.length} • Изменено: {preview.updated.length} • Удалено: {preview.removed.length} • Конфликтов конфигурации: {preview.config_conflicts.length}</p>
      {preview.warnings?.map((warning) => <p key={warning} className="help-text">{warning}</p>)}
      {unavailable > 0 && <p role="status">Обновление недоступно: не удалось получить файлы ({unavailable}).</p>}
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
        <button type="button" className="btn btn-primary btn-xs" disabled={!!busy || unavailable > 0} onClick={() => void apply("keep_existing")}>{busy ? "Применение…" : "Обновить, оставить текущие конфиги"}</button>
        {preview.config_conflicts.length > 0 && <button type="button" className="btn btn-secondary btn-xs" disabled={!!busy || unavailable > 0} onClick={() => void apply("backup_and_overwrite")}>Сделать резервную копию и заменить конфиги</button>}
        <button type="button" className="btn btn-ghost btn-xs" disabled={!!busy} onClick={() => setPreview(null)}>Закрыть</button>
      </div>
    </div>}
  </section>;
};
