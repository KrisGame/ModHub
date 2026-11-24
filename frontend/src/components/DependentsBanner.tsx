import React, { useEffect, useState } from "react";
import type { ContentType, ProjectCard } from "../types";
import { cachedIconUrl, fallbackIconDataUri, formatNumber } from "../utils";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";

interface DependentsBannerProps {
  target: ProjectCard | null;
  contentType: ContentType;
  countsByType: Record<string, number>;
  relation: string;
  projectFilter: string;
  loadedCount: number;
  totalCf: number;
  cfScanned?: number;
  sourceMode: string;
  cfUrl: string;
  isLoading: boolean;
  onContentTypeChange: (ct: ContentType) => void;
  onRelationChange: (rel: string) => void;
  onProjectFilterChange: (pf: string) => void;
  onOpenBase: () => void;
  onExit: () => void;
}

const REL_OPTS: CustomSelectOption[] = [
  { value: "required", label: "Обязательная зависимость" },
  { value: "optional", label: "Опциональная зависимость" },
  { value: "all", label: "Все типы связей" },
  { value: "embedded", label: "Встроенная библиотека" },
  { value: "tool", label: "Инструмент" },
  { value: "include", label: "Включено" },
  { value: "incompatible", label: "Несовместимо" },
];

const CT_PILLS: Array<{ id: ContentType; label: string }> = [
  { id: "mod", label: "Моды" },
  { id: "resourcepack", label: "Ресурспаки" },
  { id: "datapack", label: "Датапаки" },
  { id: "shader", label: "Шейдеры" },
];

export const DependentsBanner: React.FC<DependentsBannerProps> = ({
  target,
  contentType,
  countsByType,
  relation,
  projectFilter: _projectFilter,
  loadedCount,
  totalCf,
  cfScanned = 0,
  sourceMode,
  cfUrl,
  isLoading,
  onContentTypeChange,
  onRelationChange,
  onProjectFilterChange: _onProjectFilterChange,
  onOpenBase,
  onExit,
}) => {
  const [retained, setRetained] = useState<ProjectCard | null>(target);
  useEffect(() => {
    if (target) setRetained(target);
  }, [target]);

  const disp = target || retained;
  const activeTypeLabel = CT_PILLS.find((c) => c.id === contentType)?.label || "Моды";
  const subText = isLoading
    ? "Проверка подтверждённых связей…"
    : totalCf > 0 && sourceMode === "cf_web"
      ? `${activeTypeLabel}: ${loadedCount} • На CurseForge всего связей: ~${formatNumber(totalCf)} • работают фильтры слева`
      : sourceMode === "cf_core_api"
        ? `${activeTypeLabel}: ${loadedCount}${cfScanned > 0 ? ` (проверено: ${formatNumber(cfScanned)} через Core API)` : " • Core API"} • работают фильтры слева`
        : `${activeTypeLabel}: ${loadedCount} • работают фильтры слева`;

  return (
    <div className={`reveal-slot${target ? " is-shown" : ""}`}>
      <div className="reveal-inner">
        {disp && (
          <div className="deps-mode-banner">
            <div className="deps-mode-banner-top">
              <div className="deps-mode-banner-left">
                <img
                  width={42}
                  height={42}
                  className="deps-mode-banner-icon"
                  src={disp.icon_url ? cachedIconUrl(disp.icon_url, 96) : fallbackIconDataUri(disp.title)}
                  alt=""
                  onError={(e) => {
                    e.currentTarget.src = fallbackIconDataUri(disp.title);
                  }}
                />
                <div className="deps-mode-banner-text">
                  <span className="deps-mode-banner-title">Связанные проекты и аддоны для: {disp.title}</span>
                  <span className="deps-mode-banner-sub">{subText}</span>
                </div>
              </div>
              <div className="deps-mode-banner-actions">
                {cfUrl && (
                  <a className="btn btn-ghost btn-sm" href={cfUrl} target="_blank" rel="noopener noreferrer">
                    <img className="platform-icon-img platform-icon-cf" src="/assets/curseforge.svg" width={13} height={13} alt="CurseForge" />
                    <span>На CurseForge ↗</span>
                  </a>
                )}
                {disp.sources?.length > 0 && (
                  <button type="button" className="btn btn-ghost btn-sm" onClick={onOpenBase}>
                    Страница проекта
                  </button>
                )}
                <button type="button" className="btn btn-ghost btn-sm" onClick={onExit}>
                  Выйти &times;
                </button>
              </div>
            </div>
            <div className="deps-mode-banner-controls">
              <div className="deps-type-tabs" role="tablist" aria-label="Тип связанного контента">
                {CT_PILLS.map((ct) => {
                  const cnt = countsByType[ct.id] ?? 0;
                  const active = contentType === ct.id;
                  return (
                    <button
                      key={ct.id}
                      type="button"
                      role="tab"
                      aria-selected={active}
                      className={`deps-type-pill${active ? " is-active" : ""}`}
                      onClick={() => onContentTypeChange(ct.id)}
                    >
                      <span>{ct.label}</span>
                      <span className="deps-type-count mono-num">{cnt}</span>
                    </button>
                  );
                })}
              </div>
              <CustomSelect
                value={relation}
                options={REL_OPTS}
                onChange={onRelationChange}
                className="cs-auto"
                title="Тип зависимости"
              />
            </div>
          </div>
        )}
      </div>
    </div>
  );
};
