import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { useScrollFade } from "../hooks";
import { BanSmIcon, CategoryIcon, CloseIcon, LoaderIcon } from "../icons";
import { subscribePageScroll } from "../smoothScroll";
import type { ContentType, FilterCategory, ProjectCard } from "../types";
import { cachedIconUrl, fallbackIconDataUri, formatLoaderName, REAL_LOADERS } from "../utils";
import { CustomSelect, type CustomSelectOption } from "./CustomSelect";

interface SidebarProps {
  mobileOpen: boolean;
  onCloseMobile: () => void;
  popoverOpen?: boolean;
  onClosePopover?: () => void;
  contentType: ContentType;
  mcVersions: string[];
  version: string;
  onVersionChange: (v: string) => void;
  loadersIn: string[];
  loadersOut: string[];
  onLoaderAction: (id: string, action: "toggle_in" | "toggle_out") => void;
  onClearLoaders: () => void;
  categories: FilterCategory[];
  categoriesIn: string[];
  categoriesOut: string[];
  catMode: "and" | "or";
  onCatModeChange: (m: "and" | "or") => void;
  onCategoryAction: (id: string, action: "toggle_in" | "toggle_out") => void;
  onClearCategories: () => void;
  env: string;
  onEnvChange: (v: string) => void;
  authorFilter: string;
  onAuthorFilterChange: (a: string) => void;
  minDownloads: number;
  onMinDownloadsChange: (n: number) => void;
  updatedWithin: number;
  onUpdatedWithinChange: (d: number) => void;
  hideAbandoned: boolean;
  onHideAbandonedChange: (v: boolean) => void;
  srcModrinth: boolean;
  srcCurseforge: boolean;
  cfConfigured: boolean;
  onToggleModrinth: (v: boolean) => void;
  onToggleCurseforge: (v: boolean) => void;
  dependentsTarget: ProjectCard | null;
  onSelectDependentsTarget: (item: ProjectCard) => void;
  onClearDependentsTarget: () => void;
  onResetAll: () => void;
}

const ENV_OPTS: CustomSelectOption[] = [
  { value: "", label: "Любая" },
  { value: "client_only", label: "Только клиент" },
  { value: "server_only", label: "Только сервер" },
  { value: "both", label: "Клиент и сервер" },
];

const MIN_DL_OPTS: CustomSelectOption[] = [
  { value: "0", label: "Любое" },
  { value: "10000", label: "От 10K" },
  { value: "100000", label: "От 100K" },
  { value: "1000000", label: "От 1M" },
  { value: "10000000", label: "От 10M" },
];

const UPDATED_OPTS: CustomSelectOption[] = [
  { value: "0", label: "Любая" },
  { value: "30", label: "За последний месяц" },
  { value: "90", label: "За 3 месяца" },
  { value: "180", label: "За полгода" },
  { value: "365", label: "За последний год" },
  { value: "730", label: "За 2 года" },
];

export const Sidebar: React.FC<SidebarProps> = ({
  mobileOpen,
  onCloseMobile,
  popoverOpen = false,
  onClosePopover,
  contentType,
  mcVersions,
  version,
  onVersionChange,
  loadersIn,
  loadersOut,
  onLoaderAction,
  onClearLoaders,
  categories,
  categoriesIn,
  categoriesOut,
  catMode,
  onCatModeChange,
  onCategoryAction,
  onClearCategories,
  env,
  onEnvChange,
  authorFilter,
  onAuthorFilterChange,
  minDownloads,
  onMinDownloadsChange,
  updatedWithin,
  onUpdatedWithinChange,
  hideAbandoned,
  onHideAbandonedChange,
  srcModrinth,
  srcCurseforge,
  cfConfigured,
  onToggleModrinth,
  onToggleCurseforge,
  dependentsTarget,
  onSelectDependentsTarget,
  onClearDependentsTarget,
  onResetAll,
}) => {
  const [catsOpen, setCatsOpen] = useState(true);
  const [advOpen, setAdvOpen] = useState(false);
  const [localAuthor, setLocalAuthor] = useState(authorFilter);
  const [depInput, setDepInput] = useState(dependentsTarget?.title || "");
  const [retainedDep, setRetainedDep] = useState<ProjectCard | null>(dependentsTarget);
  const [suggestions, setSuggestions] = useState<ProjectCard[]>([]);
  const [suggestOpen, setSuggestOpen] = useState(false);
  const sidebarRef = useRef<HTMLElement | null>(null);
  const timerRef = useRef<number | null>(null);
  const authorTimerRef = useRef<number | null>(null);
  useScrollFade(sidebarRef, [contentType, catsOpen, advOpen, categories.length, dependentsTarget?.group_id]);

  useEffect(() => {
    setLocalAuthor(authorFilter);
  }, [authorFilter]);

  useEffect(() => {
    setDepInput(dependentsTarget?.title || "");
    if (dependentsTarget) {
      setRetainedDep(dependentsTarget);
      setAdvOpen(true);
    }
  }, [dependentsTarget?.group_id, dependentsTarget?.title]);

  useEffect(() => {
    return () => {
      if (timerRef.current) window.clearTimeout(timerRef.current);
      if (authorTimerRef.current) window.clearTimeout(authorTimerRef.current);
    };
  }, []);

  const versionOptions = useMemo<CustomSelectOption[]>(
    () => [
      { value: "", label: "Любая версия" },
      ...mcVersions.map((v) => ({ value: v, label: v })),
    ],
    [mcVersions]
  );

  const fetchSuggestions = (val: string) => {
    setDepInput(val);
    if (timerRef.current) window.clearTimeout(timerRef.current);
    const q = val.trim();
    if (q.length < 2) {
      setSuggestions([]);
      setSuggestOpen(false);
      return;
    }
    timerRef.current = window.setTimeout(async () => {
      try {
        const srcs: string[] = [];
        if (srcModrinth) srcs.push("modrinth");
        if (srcCurseforge) srcs.push("curseforge");
        const res = await api.search({
          q,
          type: contentType,
          sources: srcs.join(",") || "modrinth",
          sort: "downloads",
          page: 1,
          limit: 6,
          merge: true,
        });
        setSuggestions((res.items || []).slice(0, 6));
        setSuggestOpen((res.items || []).length > 0);
      } catch {
        setSuggestions([]);
      }
    }, 200);
  };

  const loaderItems = useMemo(() => {
    if (contentType === "datapack") {
      return [
        { id: "datapack", label: "Datapack", fullSpan: true },
        ...REAL_LOADERS.map((l) => ({ id: l, label: formatLoaderName(l), fullSpan: false })),
      ];
    }
    return REAL_LOADERS.map((l) => ({ id: l, label: formatLoaderName(l), fullSpan: false }));
  }, [contentType]);

  const activeLoaderCount = loadersIn.length + loadersOut.length;
  const activeCatCount = categoriesIn.length + categoriesOut.length;
  const activeAdvCount =
    (contentType === "mod" && env ? 1 : 0) +
    (updatedWithin > 0 ? 1 : 0) +
    (hideAbandoned ? 1 : 0) +
    (authorFilter.trim() ? 1 : 0) +
    (minDownloads > 0 ? 1 : 0) +
    (dependentsTarget ? 1 : 0);

  const visibleCats = useMemo(
    () =>
      categories.filter((c) => {
        if (!c.platforms?.length) return true;
        return (srcModrinth && c.platforms.includes("modrinth")) || (srcCurseforge && c.platforms.includes("curseforge"));
      }),
    [categories, srcModrinth, srcCurseforge]
  );

  const [popoverPos, setPopoverPos] = useState<{ top: number; left: number; width: number; maxHeight: number }>({
    top: 96,
    left: 224,
    width: 344,
    maxHeight: 580,
  });

  useLayoutEffect(() => {
    if (!popoverOpen) return;
    const btn = document.getElementById("catalogFilterToggleBtn");
    const syncPos = () => {
      const vw = document.documentElement.clientWidth || window.innerWidth;
      const vh = document.documentElement.clientHeight || window.innerHeight;
      const panelW = Math.min(352, Math.max(280, vw - 24));
      const minLeft = vw > 980 ? 214 : 12;
      let top = 90;
      let left = Math.max(minLeft, vw - panelW - 18);
      if (btn) {
        const r = btn.getBoundingClientRect();
        if (r.width > 0 && r.height > 0) {
          top = Math.round(r.bottom + 8);
          left = Math.round(r.right - panelW);
          if (left < minLeft) left = Math.round(r.left);
        }
      }
      left = Math.max(minLeft, Math.min(vw - panelW - 14, left));
      top = Math.max(40, Math.min(vh - 240, top));
      const maxHeight = Math.max(240, vh - top - 16);
      setPopoverPos({ top, left, width: panelW, maxHeight });
    };
    syncPos();
    const raf = requestAnimationFrame(syncPos);
    const unsubScroll = subscribePageScroll(syncPos);
    let ro: ResizeObserver | null = null;
    if (btn && typeof ResizeObserver !== "undefined") {
      ro = new ResizeObserver(syncPos);
      ro.observe(btn);
    }
    window.addEventListener("resize", syncPos, { passive: true });
    return () => {
      cancelAnimationFrame(raf);
      unsubScroll();
      ro?.disconnect();
      window.removeEventListener("resize", syncPos);
    };
  }, [popoverOpen]);

  useEffect(() => {
    if (!popoverOpen || !onClosePopover) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClosePopover();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [popoverOpen, onClosePopover]);

  return (
    <>
      {popoverOpen && (
        <div
          className="filters-popover-backdrop"
          aria-hidden="true"
          onClick={() => onClosePopover?.()}
        />
      )}
      <aside
        className={`filters-sidebar is-popover${popoverOpen ? " is-open" : ""}${mobileOpen ? " open" : ""}`}
        style={
          !mobileOpen
            ? {
                top: `${popoverPos.top}px`,
                left: `${popoverPos.left}px`,
                width: `${popoverPos.width}px`,
                maxHeight: `${popoverPos.maxHeight}px`,
              }
            : undefined
        }
        aria-label="Фильтры"
        aria-hidden={!popoverOpen && !mobileOpen}
      >
        <div className="sidebar-head">
          <span className="sidebar-head-title">Фильтры</span>
          <div className="sidebar-head-actions">
            <button className="btn btn-ghost btn-xs" type="button" title="Сбросить все фильтры" onClick={onResetAll}>
              Сброс
            </button>
            <button
              className="icon-close sidebar-close-btn"
              type="button"
              aria-label="Закрыть фильтры"
              onClick={() => {
                if (mobileOpen) onCloseMobile();
                else onClosePopover?.();
              }}
            >
              <CloseIcon size={15} />
            </button>
          </div>
        </div>

        <div ref={sidebarRef as React.RefObject<HTMLDivElement>} className="sidebar-scroll-body">
          <div className="sidebar-group-heading">Основные</div>

      {/* 1. Minecraft Version */}
      <div className="sidebar-section">
        <div className="filter-group">
          <span className="filter-group-label lbl-ico">
            <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/></svg>
            <span>Версия Minecraft</span>
            <span className="ctx-hint-dot" title="Фильтрует каталог по релизной версии MC и включает проверку совместимости на карточках">?</span>
          </span>
          <CustomSelect
            id="versionSelect"
            value={version}
            options={versionOptions}
            onChange={onVersionChange}
            searchable
            searchPlaceholder="Найти версию (1.21...)"
            mono
            ariaLabel="Версия Minecraft"
          />
        </div>
      </div>

      {/* 2. Loaders / Formats */}
      {(contentType === "mod" || contentType === "datapack") && (
        <div className="sidebar-section">
          <div className="section-head-compact">
            <span className="sidebar-label lbl-ico">
              <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3"/></svg>
              <span>{contentType === "datapack" ? "Форматы / Загрузчики" : "Загрузчик"}</span>
              <span className="ctx-hint-dot" title="Нажмите на название, чтобы включить загрузчик. Кнопка ⊘ исключает его из поиска. Выбор версии и загрузчика помогает проверить совместимость.">?</span>
            </span>
            <div className="section-head-right">
              <span className={`badge-count${activeLoaderCount === 0 ? " is-invisible" : ""}`}>{activeLoaderCount}</span>
              <button
                className={`btn btn-ghost btn-xs${activeLoaderCount === 0 ? " is-invisible" : ""}`}
                type="button"
                onClick={onClearLoaders}
              >
                Сброс
              </button>
            </div>
          </div>
          <div className="filter-chips-grid">
            {loaderItems.map((l) => {
              const isIn = loadersIn.includes(l.id);
              const isOut = !isIn && loadersOut.includes(l.id);
              return (
                <span key={l.id} className={`filter-chip loader-chip-${l.id}${l.fullSpan ? " is-full-span" : ""}${isIn ? " is-included" : ""}${isOut ? " is-excluded" : ""}`}>
                  <button
                    type="button"
                    className="filter-chip-btn"
                    aria-label={`Фильтр загрузчика: ${l.label}`}
                    aria-pressed={isIn}
                    onClick={() => onLoaderAction(l.id, "toggle_in")}
                    onContextMenu={(e) => {
                      e.preventDefault();
                      onLoaderAction(l.id, "toggle_out");
                    }}
                  >
                    <span className="filter-chip-text">
                      <LoaderIcon loader={l.id} size={12.5} />
                      <span>{l.label}</span>
                    </span>
                    <span className="filter-chip-check" aria-hidden="true">✓</span>
                  </button>
                  <button
                    type="button"
                    className="filter-chip-ban"
                    title={`Исключить «${l.label}»`}
                    aria-label={isOut ? `Убрать исключение: ${l.label}` : `Исключить загрузчик: ${l.label}`}
                    aria-pressed={isOut}
                    onClick={() => onLoaderAction(l.id, "toggle_out")}
                  >
                    <BanSmIcon />
                  </button>
                </span>
              );
            })}
          </div>
        </div>
      )}

      {/* 3. Sources (Primary Group — P1.4.4) */}
      <div className="sidebar-section">
        <div className="section-head-compact">
          <span className="sidebar-label lbl-ico">
            <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="9"/><path d="M3.6 9h16.8M3.6 15h16.8"/><path d="M11.5 3a17 17 0 0 0 0 18M12.5 3a17 17 0 0 1 0 18"/></svg>
            <span>Источники</span>
            <span className="ctx-hint-dot" title="Управляйте платформами поиска. Совпадающие проекты объединяются в одну карточку.">?</span>
          </span>
        </div>
        <div className="sidebar-sources-grid">
          <label className={`chip-toggle sidebar-src-chip${srcModrinth ? " is-on" : " is-off"}`} title="Modrinth API v2">
            <input type="checkbox" checked={srcModrinth} onChange={(e) => onToggleModrinth(e.target.checked)} />
            <img className="platform-icon-img platform-icon-mr" src="/assets/modrinth.svg" width={14} height={14} alt="" />
            <span>Modrinth</span>
          </label>
          <label
            className={`chip-toggle sidebar-src-chip${srcCurseforge ? " is-on" : " is-off"}${!cfConfigured ? " is-warn" : ""}`}
            title={cfConfigured ? "CurseForge Core API v1" : "CurseForge — требуется API-ключ в настройках"}
          >
            <input type="checkbox" checked={srcCurseforge} onChange={(e) => onToggleCurseforge(e.target.checked)} />
            <img className="platform-icon-img platform-icon-cf" src="/assets/curseforge.svg" width={14} height={14} alt="" />
            <span>CurseForge</span>
          </label>
        </div>
      </div>

      {/* 4. Categories (inline AND/OR switch integrated directly into header — no separate row) */}
      <div className="sidebar-section sidebar-categories">
        <div className="categories-header">
          <button
            className="btn-link-head categories-toggle-btn"
            type="button"
            aria-expanded={catsOpen}
            onClick={() => setCatsOpen((v) => !v)}
          >
            <span className="categories-title lbl-ico">
              <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/></svg>
              <span>Категории</span>
            </span>
            {activeCatCount > 0 && <span className="badge-count">{activeCatCount}</span>}
            <svg className="chevron-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><polyline points="18 15 12 9 6 15" /></svg>
          </button>

          <div className="categories-inline-controls">
            <div className={`cat-logic-pill${catMode === "or" ? " is-or" : ""}`} role="group" aria-label="Логика фильтрации категорий">
              <span className="cat-logic-thumb" aria-hidden="true" />
              <button
                type="button"
                className={`cat-logic-opt${catMode === "and" ? " is-active" : ""}`}
                aria-pressed={catMode === "and"}
                aria-label="Все выбранные категории (И)"
                title="Все (И) — проект должен соответствовать всем выбранным категориям"
                onClick={() => onCatModeChange("and")}
              >
                И
              </button>
              <button
                type="button"
                className={`cat-logic-opt${catMode === "or" ? " is-active" : ""}`}
                aria-pressed={catMode === "or"}
                aria-label="Любая из выбранных категорий (ИЛИ)"
                title="Любая (ИЛИ) — достаточно совпадения хотя бы с одной выбранной категорией"
                onClick={() => onCatModeChange("or")}
              >
                ИЛИ
              </button>
            </div>
            {activeCatCount > 0 && (
              <button
                className="btn btn-ghost btn-xs cat-reset-inline"
                type="button"
                title="Сбросить выбранные категории"
                onClick={onClearCategories}
              >
                Сброс
              </button>
            )}
          </div>
        </div>

        <div className={`collapsible-body${catsOpen ? " open" : ""}`}>
          <div className="collapsible-inner">
            <div className="filter-chips-list">
              {visibleCats.map((cat) => {
                const isIn = categoriesIn.includes(cat.id);
                const isOut = !isIn && categoriesOut.includes(cat.id);
                return (
                  <span key={cat.id} className={`filter-chip${isIn ? " is-included" : ""}${isOut ? " is-excluded" : ""}`}>
                    <button
                      type="button"
                      className="filter-chip-btn"
                      aria-label={`Фильтр категории: ${cat.label}`}
                      aria-pressed={isIn}
                      onClick={() => onCategoryAction(cat.id, "toggle_in")}
                      onContextMenu={(e) => {
                        e.preventDefault();
                        onCategoryAction(cat.id, "toggle_out");
                      }}
                    >
                      <span className="filter-chip-text">
                        <CategoryIcon id={cat.id} size={13} />
                        <span>{cat.label}</span>
                      </span>
                      <span className="filter-chip-check" aria-hidden="true">✓</span>
                    </button>
                    <button
                      type="button"
                      className="filter-chip-ban"
                      title={`Исключить «${cat.label}»`}
                      aria-label={isOut ? `Убрать исключение: ${cat.label}` : `Исключить категорию: ${cat.label}`}
                      aria-pressed={isOut}
                      onClick={() => onCategoryAction(cat.id, "toggle_out")}
                    >
                      <BanSmIcon />
                    </button>
                  </span>
                );
              })}
            </div>
          </div>
        </div>
      </div>

      {/* 5. Advanced Filters Group (P1.4.4) */}
      <div className="sidebar-section sidebar-section-advanced">
        <button
          type="button"
          className="sidebar-advanced-toggle"
          aria-expanded={advOpen}
          onClick={() => setAdvOpen((v) => !v)}
        >
          <span className="lbl-ico">
            <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/><circle cx="9" cy="6" r="2"/><circle cx="15" cy="12" r="2"/><circle cx="11" cy="18" r="2"/></svg>
            <span>Расширенные</span>
          </span>
          <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
            <span className={`badge-count${activeAdvCount === 0 ? " is-invisible" : ""}`}>{activeAdvCount}</span>
            <svg className="chevron-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round"><polyline points="18 15 12 9 6 15" /></svg>
          </span>
        </button>

        <div className={`collapsible-body${advOpen ? " open" : ""}`}>
          <div className="collapsible-inner">
            <div className="sidebar-advanced-inner">
              {contentType === "mod" && (
                <div className="filter-group">
                  <span className="filter-group-label">Сторона (Клиент / Сервер)</span>
                  <CustomSelect
                    id="envSelect"
                    value={env}
                    options={ENV_OPTS}
                    onChange={onEnvChange}
                    ariaLabel="Сторона"
                  />
                </div>
              )}

              <div className="filter-group">
                <span className="filter-group-label lbl-ico">
                  <span>Аддоны для мода</span>
                  <span className="ctx-hint-dot" title="Показывает проекты, которым нужен выбранный мод (например, дополнения для Create)">?</span>
                </span>
                <div className="deps-picker-wrap">
                  <input
                    type="search"
                    className="text-input deps-picker-input"
                    placeholder="Напр. Create, Fabric API..."
                    value={depInput}
                    onChange={(e) => fetchSuggestions(e.target.value)}
                    onFocus={() => {
                      if (suggestions.length > 0) setSuggestOpen(true);
                    }}
                    onBlur={() => window.setTimeout(() => setSuggestOpen(false), 180)}
                  />
                  <div className={`deps-suggest-list${suggestOpen && suggestions.length > 0 ? " is-open" : ""}`}>
                    {suggestions.map((s) => {
                      const fb = fallbackIconDataUri(s.title);
                      return (
                        <button
                          key={s.group_id}
                          type="button"
                          className="deps-suggest-item"
                          onMouseDown={(e) => {
                            e.preventDefault();
                            onSelectDependentsTarget(s);
                            setSuggestOpen(false);
                          }}
                        >
                          <img
                            className="deps-suggest-icon"
                            src={s.icon_url ? cachedIconUrl(s.icon_url, 64) : fb}
                            alt=""
                            onError={(e) => {
                              e.currentTarget.src = fb;
                            }}
                          />
                          <div className="deps-suggest-meta">
                            <span className="deps-suggest-title">{s.title}</span>
                            <span className="deps-suggest-sub">{(s.authors || []).slice(0, 2).join(", ") || "—"}</span>
                          </div>
                        </button>
                      );
                    })}
                  </div>
                </div>
                <div className={`reveal-slot reveal-slot-tight${dependentsTarget ? " is-shown" : ""}`}>
                  <div className="reveal-inner">
                    {retainedDep && (
                      <div className="deps-active-chip">
                        <img
                          src={retainedDep.icon_url ? cachedIconUrl(retainedDep.icon_url, 48) : fallbackIconDataUri(retainedDep.title)}
                          alt=""
                        />
                        <span>{retainedDep.title}</span>
                        <button
                          type="button"
                          className="icon-close"
                          style={{ width: 20, height: 20 }}
                          aria-label="Сбросить зависимость"
                          onClick={onClearDependentsTarget}
                        >
                          <CloseIcon size={11} />
                        </button>
                      </div>
                    )}
                  </div>
                </div>
              </div>

              <div className="filter-group">
                <span className="filter-group-label">Автор</span>
                <input
                  type="search"
                  className="text-input deps-picker-input"
                  placeholder="Ник автора (напр. jellysquid3)"
                  value={localAuthor}
                  onChange={(e) => {
                    const v = e.target.value;
                    setLocalAuthor(v);
                    if (authorTimerRef.current) window.clearTimeout(authorTimerRef.current);
                    authorTimerRef.current = window.setTimeout(() => {
                      onAuthorFilterChange(v.trim());
                    }, 260);
                  }}
                  onKeyDown={(e) => {
                    if (e.key === "Enter") {
                      if (authorTimerRef.current) window.clearTimeout(authorTimerRef.current);
                      onAuthorFilterChange(localAuthor.trim());
                    }
                  }}
                />
              </div>

              <div className="filter-group">
                <span className="filter-group-label">Минимум загрузок</span>
                <CustomSelect
                  id="minDownloadsSelect"
                  value={String(minDownloads || 0)}
                  options={MIN_DL_OPTS}
                  onChange={(v) => onMinDownloadsChange(Number(v || 0))}
                  ariaLabel="Минимум загрузок"
                />
              </div>

              <div className="filter-group">
                <span className="filter-group-label">Дата обновления</span>
                <CustomSelect
                  id="updatedWithinSelect"
                  value={String(updatedWithin || 0)}
                  options={UPDATED_OPTS}
                  onChange={(v) => onUpdatedWithinChange(Number(v || 0))}
                  ariaLabel="Дата обновления"
                />
              </div>

              <label className="chip-toggle chip-block" title="Скрыть архивные и не обновлявшиеся более 2 лет проекты">
                <input type="checkbox" checked={hideAbandoned} onChange={(e) => onHideAbandonedChange(e.target.checked)} />
                <span>Без заброшенных / архивных</span>
              </label>
            </div>
          </div>
        </div>
      </div>
        </div>
      </aside>
    </>
  );
};
