import React, { useState } from "react";
import { api } from "../api";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type { ImportPreview } from "../types";
import { formatBytes } from "../utils";

interface ImportModpackModalProps {
  open: boolean;
  onClose: () => void;
  onSuccess: (profileName: string) => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

export const ImportModpackModal: React.FC<ImportModpackModalProps> = ({
  open,
  onClose,
  onSuccess,
  onToast,
}) => {
  const [archivePath, setArchivePath] = useState("");
  const [preview, setPreview] = useState<ImportPreview | null>(null);
  const [loadingPreview, setLoadingPreview] = useState(false);
  const [customName, setCustomName] = useState("");
  const [importing, setImporting] = useState(false);
  const [error, setError] = useState("");

  if (!open) return null;

  const loadPreview = async (path: string) => {
    if (!path.trim()) return;
    setLoadingPreview(true);
    setError("");
    setPreview(null);
    try {
      const prev = await api.previewModpackImport({
        source_type: "file",
        source_ref: path.trim(),
      });
      setPreview(prev);
      setCustomName(prev.name || "Импортированная сборка");
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      setLoadingPreview(false);
    }
  };

  const handleBrowse = async () => {
    try {
      const chosen = await api.browseModpackArchive();
      if (chosen && chosen.trim()) {
        setArchivePath(chosen);
        await loadPreview(chosen);
      }
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    }
  };

  const handleExecute = async () => {
    if (!preview) return;
    setImporting(true);
    try {
      await api.executeModpackImport({
        preview_id: preview.preview_id,
        profile_name: customName.trim() || preview.name,
        install_minecraft: true,
        skip_unavailable: true,
      });
      onToast(`Сборка «${customName.trim() || preview.name}» успешно создана! Загружаются файлы модов…`, "ok");
      onSuccess(customName.trim() || preview.name);
      onClose();
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      setImporting(false);
    }
  };

  const formatLabel = (fmt: string) => {
    switch (fmt) {
      case "mrpack":
        return "Modrinth Pack (.mrpack)";
      case "curseforge":
        return "CurseForge Pack (.zip)";
      case "modhub":
        return "ModHub Pack (.modhub)";
      default:
        return fmt.toUpperCase();
    }
  };

  return (
    <div className="modal-backdrop overlay-animated is-open" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title">Импорт сторонней сборки</h2>
            <p className="modal-subtitle">
              Поддерживаются официальные сборки <strong>Modrinth (.mrpack)</strong>, <strong>CurseForge (.zip)</strong> и <strong>ModHub (.modhub)</strong>.
            </p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" disabled={importing} onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <div className="modal-body mh-form-stack">
          {/* File Selection */}
          <div className="mh-form-field">
            <label htmlFor="modpackPath">Файл архива сборки</label>
            <div style={{ display: "flex", gap: "8px" }}>
              <input
                id="modpackPath"
                type="text"
                className="text-input mono-num"
                style={{ flex: 1 }}
                placeholder="C:\Users\...\Downloads\Fabulously_Optimized.mrpack"
                value={archivePath}
                onChange={(e) => setArchivePath(e.target.value)}
                onBlur={() => {
                  if (archivePath && (!preview || preview.source_ref !== archivePath)) {
                    void loadPreview(archivePath);
                  }
                }}
              />
              {desktopBridge.isDesktop() && (
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  disabled={loadingPreview || importing}
                  onClick={handleBrowse}
                >
                  📁 Обзор…
                </button>
              )}
            </div>
            <p className="mh-muted-meta" style={{ marginTop: 4, fontSize: "11px" }}>
              Выберите файл с диска или вставьте полный путь к файлу .mrpack или .zip.
            </p>
          </div>

          {loadingPreview && (
            <div className="mh-panel-empty" style={{ padding: "24px", textAlign: "center" }}>
              Чтение манифеста сборки, проверка хэшей и файлов…
            </div>
          )}

          {error && (
            <div className="mh-dl-error-note" role="alert">
              <strong>Ошибка манифеста:</strong> {error}
            </div>
          )}

          {preview && (
            <div style={{ background: "rgba(255,255,255,0.03)", padding: "16px", borderRadius: "8px", border: "1px solid rgba(255,255,255,0.08)", display: "flex", flexDirection: "column", gap: "12px" }}>
              <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", flexWrap: "wrap", gap: "8px" }}>
                <div>
                  <h3 style={{ margin: 0, fontSize: "16px", fontWeight: 700 }}>{preview.name}</h3>
                  <div style={{ fontSize: "12px", color: "var(--text-muted, #94a3b8)", marginTop: "2px" }}>
                    {preview.author ? `Автор: ${preview.author} • ` : ""}Версия: {preview.version || "1.0.0"}
                  </div>
                </div>
                <span
                  style={{
                    background: "rgba(16, 185, 129, 0.15)",
                    color: "#10b981",
                    border: "1px solid rgba(16, 185, 129, 0.3)",
                    borderRadius: "4px",
                    padding: "2px 8px",
                    fontSize: "11px",
                    fontWeight: 600,
                  }}
                >
                  {formatLabel(preview.format)}
                </span>
              </div>

              {preview.description && (
                <p style={{ margin: 0, fontSize: "12px", color: "var(--text-muted, #94a3b8)", lineHeight: 1.4 }}>
                  {preview.description}
                </p>
              )}

              {/* Specs & Counts */}
              <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", gap: "8px" }} className="mono-num">
                <div style={{ background: "rgba(0,0,0,0.2)", padding: "8px 10px", borderRadius: "6px" }}>
                  <div style={{ fontSize: "11px", color: "var(--text-muted, #94a3b8)" }}>Minecraft</div>
                  <div style={{ fontSize: "13px", fontWeight: 600 }}>{preview.mc_version}</div>
                </div>
                <div style={{ background: "rgba(0,0,0,0.2)", padding: "8px 10px", borderRadius: "6px" }}>
                  <div style={{ fontSize: "11px", color: "var(--text-muted, #94a3b8)" }}>Загрузчик</div>
                  <div style={{ fontSize: "13px", fontWeight: 600 }}>{preview.loader} {preview.loader_version ? `(${preview.loader_version})` : ""}</div>
                </div>
                <div style={{ background: "rgba(0,0,0,0.2)", padding: "8px 10px", borderRadius: "6px" }}>
                  <div style={{ fontSize: "11px", color: "var(--text-muted, #94a3b8)" }}>Всего файлов</div>
                  <div style={{ fontSize: "13px", fontWeight: 600 }}>{preview.counts?.total_files || (preview.files || []).length} шт.</div>
                </div>
                <div style={{ background: "rgba(0,0,0,0.2)", padding: "8px 10px", borderRadius: "6px" }}>
                  <div style={{ fontSize: "11px", color: "var(--text-muted, #94a3b8)" }}>Объём файлов</div>
                  <div style={{ fontSize: "13px", fontWeight: 600 }}>{formatBytes(preview.estimated_bytes)}</div>
                </div>
              </div>

              {/* Custom profile name */}
              <div className="mh-form-field" style={{ marginTop: "4px" }}>
                <label htmlFor="customProfileName">Название нового профиля</label>
                <input
                  id="customProfileName"
                  type="text"
                  className="text-input"
                  value={customName}
                  onChange={(e) => setCustomName(e.target.value)}
                  placeholder="Название профиля"
                />
              </div>

              {/* Warnings */}
              {(preview.warnings || []).length > 0 && (
                <div className="mh-info-callout" style={{ fontSize: "12px", margin: 0 }}>
                  <strong>Внимание:</strong>
                  <ul style={{ margin: "4px 0 0 16px", padding: 0 }}>
                    {preview.warnings!.map((w, idx) => (
                      <li key={idx}>{w}</li>
                    ))}
                  </ul>
                </div>
              )}
            </div>
          )}

          {/* Footer actions */}
          <div className="mh-modal-actions-end" style={{ marginTop: "12px" }}>
            <button type="button" className="btn btn-ghost btn-sm" disabled={importing} onClick={onClose}>
              Отмена
            </button>
            <button
              type="button"
              className={`btn btn-primary btn-sm${importing ? " is-loading" : ""}`}
              disabled={!preview || importing}
              onClick={handleExecute}
            >
              {importing ? "Импорт и создание профиля…" : "Создать профиль из сборки"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
