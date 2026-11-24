import React, { useEffect, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { api } from "../api";
import { useEscapeKey, useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import type {
  GameInstance,
  InstallProfileVersionInput,
  JavaRecommendationResponse,
  LoaderVersionsResult,
  MCVersionManifestResult,
  ModloaderKind,
} from "../types";
import { formatDate, formatLoaderName } from "../utils";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";

interface InstallVersionWizardProps {
  open: boolean;
  instance: GameInstance;
  initialMCVersion?: string;
  initialLoader?: ModloaderKind;
  onClose: () => void;
  onStartInstall: (req: InstallProfileVersionInput) => Promise<void>;
  onOpenJavaSettings: () => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

const LOADER_CARDS: Array<{ id: ModloaderKind; title: string; desc: string }> = [
  { id: "vanilla", title: "Vanilla", desc: "Чистый официальный Minecraft без загрузчика модов" },
  { id: "fabric", title: "Fabric", desc: "Лёгкий и быстрый загрузчик (Fabric Meta API)" },
  { id: "neoforge", title: "NeoForge", desc: "Современный загрузчик для Minecraft 1.20.1 / 1.20.4 / 1.21+" },
  { id: "forge", title: "Forge", desc: "Классический загрузчик с широкой библиотекой модов" },
  { id: "quilt", title: "Quilt", desc: "Независимый загрузчик с собственной экосистемой (Quilt Meta API)" },
];

const SORT_VER_OPTS: CustomSelectOption[] = [
  { value: "date_desc", label: "Сначала новые (по дате)" },
  { value: "version_desc", label: "По номеру версии (убыв.)" },
  { value: "date_asc", label: "Сначала старые" },
];

function parseVerNum(id: string): [number, number, number] {
  const parts = id.split(".");
  return [parseInt(parts[0] || "0", 10) || 0, parseInt(parts[1] || "0", 10) || 0, parseInt(parts[2] || "0", 10) || 0];
}

export const InstallVersionWizard: React.FC<InstallVersionWizardProps> = ({
  open,
  instance,
  initialMCVersion = "",
  initialLoader = "vanilla",
  onClose,
  onStartInstall,
  onOpenJavaSettings,
  onToast,
}) => {
  const { mounted, shown } = useOverlayTransition(open, 220);
  useEscapeKey(open, onClose);
  const [step, setStep] = useState<1 | 2 | 3>(1);

  const [manifest, setManifest] = useState<MCVersionManifestResult | null>(null);
  const [manifestLoading, setManifestLoading] = useState(false);
  const [manifestError, setManifestError] = useState("");
  const [search, setSearch] = useState("");
  const [showReleases, setShowReleases] = useState(true);
  const [showSnapshots, setShowSnapshots] = useState(false);
  const [showOld, setShowOld] = useState(false);
  const [sortMode, setSortMode] = useState("date_desc");
  const [selectedMC, setSelectedMC] = useState("");

  const [selectedLoader, setSelectedLoader] = useState<ModloaderKind>("vanilla");
  const [loaderRes, setLoaderRes] = useState<LoaderVersionsResult | null>(null);
  const [loaderLoading, setLoaderLoading] = useState(false);
  const [loaderError, setLoaderError] = useState("");
  const [selectedLoaderVer, setSelectedLoaderVer] = useState("");

  const [javaRec, setJavaRec] = useState<JavaRecommendationResponse | null>(null);
  const [createBackup, setCreateBackup] = useState(true);
  const [submitting, setSubmitting] = useState(false);

  const listParentRef = useRef<HTMLDivElement | null>(null);

  const loadManifest = async (forceRefresh: boolean) => {
    setManifestLoading(true);
    setManifestError("");
    try {
      const res = await api.getMinecraftManifest(forceRefresh);
      setManifest(res);
      if (!selectedMC) {
        setSelectedMC(initialMCVersion || instance.mc_version || res.latest_release || res.versions[0]?.id || "");
      }
    } catch (err) {
      setManifestError(String((err as Error).message || err));
    } finally {
      setManifestLoading(false);
    }
  };

  useEffect(() => {
    if (!open) return;
    setStep(1);
    setSearch("");
    setShowReleases(true);
    setShowSnapshots(false);
    setShowOld(false);
    setSortMode("date_desc");
    setSelectedMC(initialMCVersion || instance.mc_version || "");
    const initLd: ModloaderKind =
      initialLoader ||
      (instance.instance_type && instance.instance_type !== "empty" ? (instance.instance_type as ModloaderKind) : "vanilla");
    setSelectedLoader(initLd);
    setSelectedLoaderVer(instance.loader_version || "");
    setCreateBackup(instance.mods_count > 0 || Boolean(instance.mc_version));
    setSubmitting(false);
    void loadManifest(false);
  }, [open, instance.id]);

  useEffect(() => {
    if (!open || step !== 2 || !selectedMC) return;
    if (selectedLoader === "vanilla") {
      setLoaderRes({ loader: "vanilla", mc_version: selectedMC, compatible: true, versions: [] });
      setSelectedLoaderVer("");
      setLoaderError("");
      setLoaderLoading(false);
      return;
    }
    let alive = true;
    setLoaderLoading(true);
    setLoaderError("");
    setLoaderRes(null);
    api
      .getModloaderVersions(selectedLoader, selectedMC)
      .then((res) => {
        if (!alive) return;
        setLoaderRes(res);
        const vers = Array.isArray(res?.versions) ? res.versions : [];
        if (res?.compatible && vers.length > 0) {
          const rec = vers.find((v) => v.recommended) || vers[0];
          setSelectedLoaderVer((prev) => (vers.some((v) => v.version === prev) ? prev : rec.version));
        } else {
          setSelectedLoaderVer("");
        }
      })
      .catch((err) => {
        if (alive) setLoaderError(String((err as Error).message || err));
      })
      .finally(() => {
        if (alive) setLoaderLoading(false);
      });
    return () => {
      alive = false;
    };
  }, [open, step, selectedMC, selectedLoader]);

  useEffect(() => {
    if (!open || step !== 3 || !selectedMC) return;
    api.recommendJavaForMC(selectedMC, selectedLoader).then(setJavaRec).catch(() => setJavaRec(null));
  }, [open, step, selectedMC, selectedLoader]);

  const filteredVersions = useMemo(() => {
    const all = manifest?.versions || [];
    const q = search.trim().toLowerCase();
    const list = all.filter((v) => {
      if (q && !v.id.toLowerCase().includes(q) && !v.type.toLowerCase().includes(q)) return false;
      if (v.type === "release") return showReleases;
      if (v.type === "snapshot") return showSnapshots;
      return showOld;
    });
    return [...list].sort((a, b) => {
      if (sortMode === "date_asc") return (a.releaseTime || "").localeCompare(b.releaseTime || "");
      if (sortMode === "version_desc") {
        const ta = parseVerNum(a.id);
        const tb = parseVerNum(b.id);
        if (ta[0] !== tb[0]) return tb[0] - ta[0];
        if (ta[1] !== tb[1]) return tb[1] - ta[1];
        if (ta[2] !== tb[2]) return tb[2] - ta[2];
        return (b.releaseTime || "").localeCompare(a.releaseTime || "");
      }
      return (b.releaseTime || "").localeCompare(a.releaseTime || "");
    });
  }, [manifest, search, showReleases, showSnapshots, showOld, sortMode]);

  const rowVirtualizer = useVirtualizer({
    count: filteredVersions.length,
    getScrollElement: () => listParentRef.current,
    estimateSize: () => 44,
    overscan: 8,
  });

  const canNextFromStep1 = Boolean(selectedMC);
  const canNextFromStep2 = selectedLoader === "vanilla" || Boolean(!loaderLoading && loaderRes?.compatible && selectedLoaderVer);

  const handleConfirm = async () => {
    if (!selectedMC || submitting) return;
    setSubmitting(true);
    try {
      await onStartInstall({
        instance_id: instance.id,
        mc_version: selectedMC,
        loader: selectedLoader,
        loader_version: selectedLoader === "vanilla" ? "" : selectedLoaderVer,
        create_backup: createBackup,
      });
      onClose();
    } catch (err) {
      onToast(`Не удалось запустить установку: ${(err as Error).message}`, "err");
    } finally {
      setSubmitting(false);
    }
  };

  if (!mounted) return null;

  return (
    <div className={`modal-backdrop overlay-animated${shown ? " is-open" : ""}`} role="dialog" aria-modal="true" aria-hidden={!shown} onClick={onClose}>
      <div className="modal-window modal-lg mh-wizard-modal" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title">Установка Minecraft и загрузчика</h2>
            <p className="modal-subtitle">Профиль «{instance.name}» • Шаг {step} из 3</p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <div className="mh-wizard-steps" role="tablist" aria-label="Шаги установки версии">
          <button type="button" role="tab" aria-selected={step === 1} className={`mh-wiz-step${step === 1 ? " is-active" : step > 1 ? " is-done" : ""}`} onClick={() => setStep(1)}>1. Версия Minecraft</button>
          <button type="button" role="tab" aria-selected={step === 2} className={`mh-wiz-step${step === 2 ? " is-active" : step > 2 ? " is-done" : ""}`} disabled={!canNextFromStep1} onClick={() => canNextFromStep1 && setStep(2)}>2. Загрузчик модов</button>
          <button type="button" role="tab" aria-selected={step === 3} className={`mh-wiz-step${step === 3 ? " is-active" : ""}`} disabled={!canNextFromStep1 || !canNextFromStep2} onClick={() => canNextFromStep1 && canNextFromStep2 && setStep(3)}>3. Подтверждение</button>
        </div>

        <div className="modal-body mh-form-stack">
          {step === 1 && (
            <>
              <div className="mh-wiz-toolbar">
                <input type="search" className="text-input mh-inst-search" placeholder="Поиск версии (1.21.1, 1.20.1, 24w...)" value={search} onChange={(e) => setSearch(e.target.value)} autoFocus />
                <CustomSelect value={sortMode} options={SORT_VER_OPTS} onChange={setSortMode} className="cs-auto" ariaLabel="Сортировка версий" />
                <button type="button" className={`btn btn-ghost btn-sm${manifestLoading ? " is-loading" : ""}`} disabled={manifestLoading} onClick={() => void loadManifest(true)}>Обновить</button>
              </div>
              <div className="mh-wiz-filter-row">
                <label className="chip-toggle"><input type="checkbox" checked={showReleases} onChange={(e) => setShowReleases(e.target.checked)} /><span>Release</span></label>
                <label className="chip-toggle"><input type="checkbox" checked={showSnapshots} onChange={(e) => setShowSnapshots(e.target.checked)} /><span>Snapshot</span></label>
                <label className="chip-toggle"><input type="checkbox" checked={showOld} onChange={(e) => setShowOld(e.target.checked)} /><span>Старые (Beta/Alpha)</span></label>
                {manifest?.latest_release && (
                  <button type="button" className="btn btn-ghost btn-xs" onClick={() => setSelectedMC(manifest.latest_release)}>Последний релиз: {manifest.latest_release}</button>
                )}
              </div>
              {manifest?.stale && manifest.warning && <div className="mh-inst-issue is-warn" role="status">{manifest.warning}</div>}
              {manifestError && <div className="mh-dl-error-note">{manifestError}</div>}
              {manifestLoading && !manifest ? (
                <div className="mh-panel-empty">Загрузка официального манифеста Mojang…</div>
              ) : (
                <div ref={listParentRef} className="mh-ver-virtual-list" role="listbox" aria-label="Версии Minecraft">
                  <div style={{ height: rowVirtualizer.getTotalSize(), position: "relative", width: "100%" }}>
                    {rowVirtualizer.getVirtualItems().map((vRow) => {
                      const v = filteredVersions[vRow.index];
                      if (!v) return null;
                      const isSel = selectedMC === v.id;
                      return (
                        <button key={v.id} type="button" role="option" aria-selected={isSel} className={`mh-ver-row${isSel ? " is-selected" : ""}`} style={{ position: "absolute", top: 0, left: 0, width: "100%", height: vRow.size, transform: `translateY(${vRow.start}px)` }} onClick={() => setSelectedMC(v.id)}>
                          <span className="mh-ver-row-id mono-num">{v.id}</span>
                          <span className="pill">{v.type}</span>
                          {v.id === manifest?.latest_release && <span className="pill pill-installed">Последний релиз</span>}
                          {v.installed && <span className="pill">Кэширована</span>}
                          <span className="mh-ver-row-date mono-num">{v.releaseTime ? formatDate(v.releaseTime) : ""}</span>
                        </button>
                      );
                    })}
                  </div>
                </div>
              )}
            </>
          )}

          {step === 2 && (
            <>
              <div className="mh-info-callout">Выбрана версия: <strong className="mono-num">Minecraft {selectedMC}</strong>. Выберите тип установки и совместимую версию загрузчика.</div>
              <div className="mh-loader-grid">
                {LOADER_CARDS.map((card) => (
                  <button key={card.id} type="button" className={`mh-loader-card${selectedLoader === card.id ? " is-selected" : ""}`} onClick={() => setSelectedLoader(card.id)}>
                    <strong>{card.title}</strong>
                    <span>{card.desc}</span>
                  </button>
                ))}
              </div>
              {selectedLoader !== "vanilla" && (
                <div className="mh-form-field">
                  <label>Совместимая версия {formatLoaderName(selectedLoader)} для MC {selectedMC}</label>
                  {loaderLoading ? (
                    <div className="mh-panel-empty">Загрузка официальных версий {formatLoaderName(selectedLoader)}…</div>
                  ) : loaderError ? (
                    <div className="mh-dl-error-note">{loaderError}</div>
                  ) : !loaderRes?.compatible || !Array.isArray(loaderRes?.versions) || loaderRes.versions.length === 0 ? (
                    <div className="mh-inst-issue is-warn">{loaderRes?.message || `${formatLoaderName(selectedLoader)} не поддерживает Minecraft ${selectedMC}.`}</div>
                  ) : (
                    <div className="mh-loader-ver-list" role="listbox">
                      {(loaderRes.versions || []).slice(0, 60).map((lv) => (
                        <button key={lv.full_version || lv.version} type="button" className={`mh-ver-row${selectedLoaderVer === lv.version ? " is-selected" : ""}`} onClick={() => setSelectedLoaderVer(lv.version)}>
                          <span className="mh-ver-row-id mono-num">{lv.version}</span>
                          {lv.recommended && <span className="pill pill-installed">Рекомендуемая</span>}
                          {lv.latest && !lv.recommended && <span className="pill">Последняя</span>}
                          {!lv.stable && <span className="pill">Beta</span>}
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </>
          )}

          {step === 3 && (
            <>
              <div className="mh-data-dir-box">
                <div><strong>Профиль:</strong> {instance.name}</div>
                <div><strong>Версия Minecraft:</strong> <span className="mono-num">{selectedMC}</span></div>
                <div><strong>Загрузчик:</strong> {selectedLoader === "vanilla" ? "Vanilla (без загрузчика)" : `${formatLoaderName(selectedLoader)} ${selectedLoaderVer}`}</div>
                <div><strong>Java Runtime:</strong> {javaRec?.recommended ? <span>{javaRec.recommended.name} ({javaRec.recommended.version}) — совместима ✓</span> : <span style={{ color: "var(--status-warn)" }}>Требуется Java {javaRec?.requirement.recommended_major || 21} (не обнаружена)</span>}</div>
              </div>
              {!javaRec?.recommended && (
                <div className="mh-inst-issue is-warn">
                  <div><strong>Внимание:</strong> совместимая Java {javaRec?.requirement.recommended_major || 21} не найдена.{selectedLoader === "forge" || selectedLoader === "neoforge" ? " Для выполнения процессоров Forge/NeoForge требуется установленная Java!" : " Установите Java в «Настройки → Java Runtime»."}</div>
                  <button type="button" className="btn btn-ghost btn-xs" style={{ marginTop: 6 }} onClick={() => { onClose(); onOpenJavaSettings(); }}>Открыть настройки Java →</button>
                </div>
              )}
              <div className="mh-info-callout">Загрузка выполнится через централизованный <strong>Download Manager</strong> с проверкой SHA-1. Существующие моды, конфиги, сохранения, ресурс-паки и шейдеры не будут удалены.</div>
              <label className="chip-toggle"><input type="checkbox" checked={createBackup} onChange={(e) => setCreateBackup(e.target.checked)} /><span>Создать резервную копию метаданных и настроек профиля (instance.json, options.txt) перед установкой</span></label>
            </>
          )}

          <div className="mh-modal-actions-end" style={{ justifyContent: "space-between" }}>
            <div>{step > 1 && <button type="button" className="btn btn-ghost btn-sm" onClick={() => setStep((s) => (s === 3 ? 2 : 1))}>← Назад</button>}</div>
            <div style={{ display: "inline-flex", gap: 8 }}>
              <button type="button" className="btn btn-ghost btn-sm" onClick={onClose} disabled={submitting}>Отмена</button>
              {step === 1 && <button type="button" className="btn btn-primary btn-sm" disabled={!canNextFromStep1} onClick={() => setStep(2)}>Далее →</button>}
              {step === 2 && <button type="button" className="btn btn-primary btn-sm" disabled={!canNextFromStep2} onClick={() => setStep(3)}>Далее →</button>}
              {step === 3 && <button type="button" className={`btn btn-primary btn-sm${submitting ? " is-loading" : ""}`} disabled={submitting} onClick={handleConfirm}>{submitting ? "Запуск установки…" : "Установить"}</button>}
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};
