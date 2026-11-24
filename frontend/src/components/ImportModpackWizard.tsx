import React, { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { useEscapeKey, useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type { ConflictResolution, GameInstance, ImportPreview, ModpackImportProgress, PreviewImportInput, SavedPack } from "../types";
import { formatBytes, formatLoaderName } from "../utils";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";

interface ImportModpackWizardProps {
  open: boolean;
  initialFilePath?: string;
  initialPackId?: string;
  targetInstanceId?: string;
  savedPacks: SavedPack[];
  instances: GameInstance[];
  importProgress: ModpackImportProgress | null;
  onClose: () => void;
  onOpenDownloads: () => void;
  onOpenInstances: () => void;
  onRefreshAll: () => Promise<void>;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

const SOURCE_TABS: Array<{ id: PreviewImportInput["source_type"]; label: string }> = [
  { id: "file", label: "Файл архива" },
  { id: "modrinth", label: "Modrinth" },
  { id: "curseforge", label: "CurseForge" },
  { id: "url", label: "Прямой URL" },
  { id: "pack", label: "Из Packs" },
  { id: "instance", label: "Из профиля" },
];

const CONFLICT_OPTS: CustomSelectOption[] = [
  { value: "keep_existing", label: "Сохранять существующие файлы" },
  { value: "backup_and_overwrite", label: "Резервная копия и замена" },
  { value: "overwrite", label: "Перезаписывать при совпадении" },
];

function formatLabel(fmt: string): string {
  if (fmt === "mrpack") return "Modrinth (.mrpack)";
  if (fmt === "curseforge") return "CurseForge (.zip)";
  return "ModHub (.modhub)";
}

export const ImportModpackWizard: React.FC<ImportModpackWizardProps> = ({
  open, initialFilePath = "", initialPackId = "", targetInstanceId = "", savedPacks, instances, importProgress,
  onClose, onOpenDownloads, onOpenInstances, onRefreshAll, onToast,
}) => {
  const { mounted, shown } = useOverlayTransition(open, 220);
  const [step, setStep] = useState<1 | 2 | 3 | 4 | 5 | 6 | 7>(1);
  useEscapeKey(open && step !== 6, onClose);
  const [sourceType, setSourceType] = useState<PreviewImportInput["source_type"]>("file");
  const [sourceRef, setSourceRef] = useState("");
  const [analyzeError, setAnalyzeError] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [showAllFiles, setShowAllFiles] = useState(false);

  const [profileName, setProfileName] = useState("");
  const [description, setDescription] = useState("");
  const [memoryMax, setMemoryMax] = useState(4096);
  const [selectedOptional, setSelectedOptional] = useState<Record<string, boolean>>({});
  const [skipUnavailable, setSkipUnavailable] = useState(true);
  const [conflictPolicy, setConflictPolicy] = useState<ConflictResolution>("keep_existing");
  const [installMinecraft, setInstallMinecraft] = useState(true);
  const [submitting, setSubmitting] = useState(false);
  const importProgressBaseline = useRef("");

  const runAnalyze = async (st: PreviewImportInput["source_type"], ref: string) => {
    const cleanRef = ref.trim();
    if (!cleanRef) { setAnalyzeError("Укажите файл, ссылку или выберите сборку"); return; }
    setAnalyzeError("");
    setStep(2);
    try {
      const prev = await api.previewModpackImport({ source_type: st, source_ref: cleanRef });
      setPreview(prev);
      setProfileName(prev.name || "Imported Modpack");
      setDescription(prev.description || "");
      setMemoryMax(prev.profile_settings?.memory_max_mb || 4096);
      const optInit: Record<string, boolean> = {};
      for (const f of prev.optional_files || []) optInit[f.rel_path] = false;
      setSelectedOptional(optInit);
      setSkipUnavailable((prev.unavailable_files || []).length > 0);
      setShowAllFiles(false);
      setStep(3);
    } catch (err) {
      setAnalyzeError(String((err as Error).message || err));
      setStep(1);
    }
  };

  useEffect(() => {
    if (!open) return;
    setAnalyzeError("");
    setPreview(null);
    setSubmitting(false);
    setStep(1);
    if (initialFilePath) { setSourceType("file"); setSourceRef(initialFilePath); void runAnalyze("file", initialFilePath); }
    else if (initialPackId) { setSourceType("pack"); setSourceRef(initialPackId); void runAnalyze("pack", initialPackId); }
    else { setStep(1); setSourceType("file"); setSourceRef(""); }
  }, [open, initialFilePath, initialPackId, targetInstanceId]);

  useEffect(() => {
    if (step !== 6 || !importProgress) return;
    const signature = `${importProgress.updated_at}|${importProgress.stage}|${importProgress.instance_id || ""}|${importProgress.message}`;
    if (signature === importProgressBaseline.current) return;
    if (targetInstanceId ? importProgress.instance_id !== targetInstanceId : Boolean(importProgress.instance_id)) return;
    if (importProgress.stage === "completed" || importProgress.stage === "partial" || importProgress.stage === "error") {
      setStep(7);
      void onRefreshAll();
    }
  }, [step, importProgress, onRefreshAll, targetInstanceId]);

  const packOptions = useMemo<CustomSelectOption[]>(() => [
    { value: "", label: "Выберите сборку из Packs..." },
    ...savedPacks.map((p) => ({ value: p.id, label: p.name, sub: `${p.items.length} шт.${p.mc_version ? ` • ${p.mc_version}` : ""}` })),
  ], [savedPacks]);

  const instanceOptions = useMemo<CustomSelectOption[]>(() => [
    { value: "", label: "Выберите существующий профиль..." },
    ...instances.map((i) => ({ value: i.id, label: i.name, sub: i.mc_version ? `MC ${i.mc_version}` : "Пустой" })),
  ], [instances]);
  const targetInstance = instances.find((instance) => instance.id === targetInstanceId) || null;

  const handleStartImport = async () => {
    if (!preview || submitting) return;
    setSubmitting(true);
    importProgressBaseline.current = importProgress
      ? `${importProgress.updated_at}|${importProgress.stage}|${importProgress.instance_id || ""}|${importProgress.message}`
      : "";
    try {
      const optList = Object.entries(selectedOptional).filter(([, checked]) => checked).map(([p]) => p);
      await api.executeModpackImport({
        preview_id: preview.preview_id, profile_name: profileName.trim() || preview.name,
        description: description.trim(), memory_max_mb: memoryMax, selected_optional: optList,
        skip_unavailable: skipUnavailable, conflict_policy: conflictPolicy, install_minecraft: targetInstanceId ? false : installMinecraft,
        target_instance_id: targetInstanceId || undefined,
      });
      setStep(6);
    } catch (err) {
      onToast(`Не удалось запустить импорт: ${(err as Error).message}`, "err");
    } finally {
      setSubmitting(false);
    }
  };

  if (!mounted) return null;

  return (
    <div className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`} role="dialog" aria-modal="true" onClick={step === 6 ? undefined : onClose}>
      <div className="modal-window modal-lg mh-wizard-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title">{targetInstanceId ? "Установка сборки в профиль" : "Мастер импорта сборки"}</h2>
            <p className="modal-subtitle">Этап {step} из 7 • Поддерживаются .mrpack, CurseForge .zip, .modhub, URL, Packs и профили</p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}><CloseIcon size={15} /></button>
        </div>

        <div className="modal-body mh-form-stack">
          {step === 1 && (
            <>
              <div className="mh-inst-tabs" role="tablist" aria-label="Источник сборки">
                {SOURCE_TABS.map((t) => (
                  <button key={t.id} type="button" role="tab" aria-selected={sourceType === t.id} className={`mh-inst-tab${sourceType === t.id ? " is-active" : ""}`} onClick={() => { setSourceType(t.id); setSourceRef(""); setAnalyzeError(""); }}>
                    {t.label}
                  </button>
                ))}
              </div>
              {sourceType === "file" && (
                <div className="mh-form-field">
                  <label>Путь к архиву сборки (.mrpack, .zip, .modhub)</label>
                  <div className="input-action-row">
                    <input type="text" className="text-input mono-num" placeholder="Перетащите архив в окно ModHub или выберите файл..." value={sourceRef} onChange={(e) => setSourceRef(e.target.value)} />
                    {desktopBridge.isDesktop() && (
                      <button type="button" className="btn btn-primary btn-sm" onClick={async () => { const p = await desktopBridge.browseModpackArchive(); if (p) setSourceRef(p); }}>Выбрать файл…</button>
                    )}
                  </div>
                </div>
              )}
              {(sourceType === "modrinth" || sourceType === "curseforge" || sourceType === "url") && (
                <div className="mh-form-field">
                  <label>{sourceType === "modrinth" ? "Slug, ID или ссылка на модпак Modrinth" : sourceType === "curseforge" ? "ID, slug или ссылка на модпак CurseForge" : "Прямая HTTPS-ссылка на архив"}</label>
                  <input type="text" className="text-input mono-num" value={sourceRef} onChange={(e) => setSourceRef(e.target.value)} />
                </div>
              )}
              {sourceType === "pack" && (
                <div className="mh-form-field">
                  <label>Существующая сборка из Packs</label>
                  <CustomSelect value={sourceRef} options={packOptions} onChange={setSourceRef} ariaLabel="Выбор сборки Packs" />
                </div>
              )}
              {sourceType === "instance" && (
                <div className="mh-form-field">
                  <label>Создать профиль на основе существующего</label>
                  <CustomSelect value={sourceRef} options={instanceOptions} onChange={setSourceRef} ariaLabel="Выбор профиля" />
                </div>
              )}
              {analyzeError && <div className="mh-dl-error-note">{analyzeError}</div>}
              <div className="mh-modal-actions-end">
                <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Отмена</button>
                <button type="button" className="btn btn-primary btn-sm" disabled={!sourceRef.trim()} onClick={() => void runAnalyze(sourceType, sourceRef)}>Анализировать сборку →</button>
              </div>
            </>
          )}

          {step === 2 && <div className="empty-state"><span className="spinner" style={{ marginRight: 10, verticalAlign: "middle" }} />Проверка безопасности архива и анализ манифеста…</div>}

          {step === 3 && preview && (
            <>
              <div className="mh-data-dir-box">
                <div><strong>Название:</strong> {preview.name} {preview.version ? `(v${preview.version})` : ""}</div>
                {preview.author && <div><strong>Автор:</strong> {preview.author}</div>}
                {preview.description && <div><strong>Описание:</strong> {preview.description}</div>}
                <div className="mono-num"><strong>Формат:</strong> {formatLabel(preview.format)} • <strong>Minecraft:</strong> {preview.mc_version || "?"} • <strong>Загрузчик:</strong> {formatLoaderName(preview.loader || "vanilla")} {preview.loader_version || ""}</div>
                <div className="mono-num"><strong>Состав:</strong> Модов: {preview.counts.mods} • Ресурспаков: {preview.counts.resourcepacks} • Шейдеров: {preview.counts.shaders} • Датапаков: {preview.counts.datapacks} • Конфигов/прочего: {preview.counts.configs + preview.counts.user_files} • Размер: ~{formatBytes(preview.estimated_bytes)}</div>
              </div>
              {(preview.warnings || []).map((w, i) => <div key={i} className="mh-inst-issue is-warn">{w}</div>)}
              {(preview.unavailable_files || []).length > 0 && (
                <div className="mh-dl-error-note">
                  <strong>Недоступные файлы ({preview.unavailable_files!.length}):</strong>
                  <div style={{ marginTop: 4, maxHeight: 110, overflowY: "auto" }}>
                    {preview.unavailable_files!.map((u) => <div key={u.rel_path} className="mono-num" style={{ fontSize: 11.5 }}>• {u.title || u.filename}: {u.unavailable_reason}</div>)}
                  </div>
                </div>
              )}
              {(preview.conflicts || []).length > 0 && (
                <div className="mh-inst-issue is-warn">
                  <strong>Конфликты и предупреждения ({preview.conflicts!.length}):</strong>
                  {preview.conflicts!.slice(0, 6).map((c, idx) => <div key={idx} className="mono-num" style={{ fontSize: 11.5 }}>• {c.message}</div>)}
                </div>
              )}
              <div>
                <button type="button" className="btn btn-ghost btn-xs" onClick={() => setShowAllFiles((v) => !v)}>
                  {showAllFiles ? "Скрыть полный список" : `Показать все компоненты (${preview.files.length})`}
                </button>
                {showAllFiles && (
                  <div className="mh-loader-ver-list mono-num" style={{ marginTop: 8, maxHeight: 180 }}>
                    {preview.files.map((f) => (
                      <div key={f.rel_path} className="mh-ver-row" style={{ height: "auto", padding: "5px 10px", cursor: "default" }}>
                        <span style={{ flex: 1, fontSize: 12 }}>{f.rel_path}</span>
                        <span className="pill">{f.category}</span>
                        {f.optional && <span className="pill">Опц.</span>}
                        {!f.available && <span className="pill pill-alpha">Недоступен</span>}
                      </div>
                    ))}
                  </div>
                )}
              </div>
              <div className="mh-modal-actions-end" style={{ justifyContent: "space-between" }}>
                <button type="button" className="btn btn-ghost btn-sm" onClick={() => setStep(1)}>← Назад</button>
                <div style={{ display: "inline-flex", gap: 8 }}>
                  <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Отмена</button>
                  <button type="button" className="btn btn-primary btn-sm" onClick={() => setStep(4)}>Настройки импорта →</button>
                </div>
              </div>
            </>
          )}

          {step === 4 && preview && (
            <>
              {targetInstanceId ? (
                <div className="mh-info-callout">
                  Файлы будут установлены в профиль <strong>{targetInstance?.name || ""}</strong>. Настройки профиля и установленная версия Minecraft не изменятся.
                  {targetInstance && preview.mc_version && (targetInstance.mc_version !== preview.mc_version || (preview.loader && targetInstance.instance_type !== preview.loader)) && (
                    <div className="mh-inst-issue is-warn" style={{ marginTop: 8 }}>
                      Сборка рассчитана на Minecraft {preview.mc_version} / {formatLoaderName(preview.loader || "vanilla")}, а профиль настроен на {targetInstance.mc_version || "без версии"} / {formatLoaderName(targetInstance.instance_type)}. Убедитесь, что совместимость подходит.
                    </div>
                  )}
                </div>
              ) : (
                <>
                  <div className="mh-form-grid-2">
                    <div className="mh-form-field"><label>Название нового профиля</label><input type="text" className="text-input" value={profileName} onChange={(e) => setProfileName(e.target.value)} /></div>
                    <div className="mh-form-field"><label>Память Xmx (МБ)</label><input type="number" min={1024} max={32768} step={512} className="text-input mono-num" value={memoryMax} onChange={(e) => setMemoryMax(Number(e.target.value) || 4096)} /></div>
                  </div>
                  <div className="mh-form-field"><label>Описание профиля</label><input type="text" className="text-input" value={description} onChange={(e) => setDescription(e.target.value)} /></div>
                </>
              )}
              <div className="mh-form-field"><label>Разрешение конфликтов</label><CustomSelect value={conflictPolicy} options={CONFLICT_OPTS} onChange={(v) => setConflictPolicy(v as ConflictResolution)} ariaLabel="Конфликты" /></div>
              {!targetInstanceId && <label className="chip-toggle"><input type="checkbox" checked={installMinecraft} onChange={(e) => setInstallMinecraft(e.target.checked)} /><span>Автоматически установить Minecraft {preview.mc_version || ""} и загрузчик {formatLoaderName(preview.loader || "vanilla")}</span></label>}
              {(preview.unavailable_files || []).length > 0 && (
                <label className="chip-toggle"><input type="checkbox" checked={skipUnavailable} onChange={(e) => setSkipUnavailable(e.target.checked)} /><span>Пропустить недоступные файлы ({preview.unavailable_files!.length})</span></label>
              )}
              {(preview.optional_files || []).length > 0 && (
                <div className="mh-form-field">
                  <label>Необязательные файлы ({preview.optional_files!.length})</label>
                  <div className="mh-loader-ver-list" style={{ maxHeight: 140 }}>
                    {preview.optional_files!.map((opt) => (
                      <label key={opt.rel_path} className="chip-toggle" style={{ padding: "4px 8px" }}>
                        <input type="checkbox" checked={Boolean(selectedOptional[opt.rel_path])} onChange={(e) => setSelectedOptional((prev) => ({ ...prev, [opt.rel_path]: e.target.checked }))} />
                        <span className="mono-num" style={{ fontSize: 12 }}>{opt.title || opt.rel_path}</span>
                      </label>
                    ))}
                  </div>
                </div>
              )}
              <div className="mh-modal-actions-end" style={{ justifyContent: "space-between" }}>
                <button type="button" className="btn btn-ghost btn-sm" onClick={() => setStep(3)}>← Предпросмотр</button>
                <button type="button" className="btn btn-primary btn-sm" onClick={() => setStep(5)}>К подтверждению →</button>
              </div>
            </>
          )}

          {step === 5 && preview && (
            <>
              <div className="mh-data-dir-box">
                <div><strong>{targetInstanceId ? "Целевой профиль:" : "Новый изолированный профиль:"}</strong> {targetInstanceId ? targetInstance?.name || "Профиль" : profileName || preview.name}</div>
                <div><strong>Версия и загрузчик:</strong> MC {preview.mc_version || "?"} • {formatLoaderName(preview.loader || "vanilla")} {preview.loader_version || ""}</div>
                <div><strong>Установка MC + загрузчика:</strong> {targetInstanceId ? "Нет — версия профиля не изменяется" : installMinecraft ? "Да" : "Нет"}</div>
                <div><strong>Загрузки:</strong> через централизованный Download Manager с проверкой контрольных сумм.</div>
              </div>
              <div className="mh-modal-actions-end" style={{ justifyContent: "space-between" }}>
                <button type="button" className="btn btn-ghost btn-sm" onClick={() => setStep(4)}>← Назад</button>
                <button type="button" className={`btn btn-primary btn-sm${submitting ? " is-loading" : ""}`} disabled={submitting || ((preview.unavailable_files || []).length > 0 && !skipUnavailable)} onClick={handleStartImport}>
                  {submitting ? "Запуск…" : "Подтвердить и установить"}
                </button>
              </div>
            </>
          )}

          {step === 6 && (
            <div className="mh-form-stack">
              <div className="mh-info-callout"><strong>{importProgress?.message || "Выполняется импорт сборки..."}</strong></div>
              <div className="mh-dl-progress-track">
                <div className={`mh-dl-progress-fill${!importProgress || importProgress.progress < 0 ? " is-indeterminate" : ""}`} style={{ width: !importProgress || importProgress.progress < 0 ? "35%" : `${Math.max(3, Math.min(100, importProgress.progress))}%` }} />
              </div>
              <div className="mh-modal-actions-end"><button type="button" className="btn btn-ghost btn-sm" onClick={() => { onClose(); onOpenDownloads(); }}>Открыть «Загрузки» →</button></div>
            </div>
          )}

          {step === 7 && (
            <div className="mh-form-stack">
              {importProgress?.stage === "error" ? (
                <div className="mh-dl-error-note"><strong>Ошибка импорта:</strong> {importProgress.error || importProgress.message}</div>
              ) : (
                <div className="mh-inst-ok-note"><strong>{importProgress?.message || "Сборка успешно импортирована!"}</strong></div>
              )}
              <div className="mh-modal-actions-end">
                <button type="button" className="btn btn-ghost btn-sm" onClick={onClose}>Закрыть</button>
                <button type="button" className="btn btn-primary btn-sm" onClick={() => { onClose(); onOpenInstances(); }}>Открыть «Профили» →</button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
};
