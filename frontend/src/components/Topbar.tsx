import React, { useEffect, useLayoutEffect, useRef, useState } from "react";
import { CloseIcon } from "../icons";
import { getPageScrollTop, subscribePageScroll } from "../smoothScroll";
import type { ContentType, ThemeMode } from "../types";
import { isProjectUrlString } from "../utils";

interface TopbarProps {
  query: string;
  onQueryCommit: (q: string) => void;
  onUrlPaste: (url: string) => void;
  contentType: ContentType;
  onContentTypeChange: (ct: ContentType) => void;
  theme: ThemeMode;
  onToggleTheme: (e: React.MouseEvent<HTMLButtonElement>) => void;
  cfStatus: "ok" | "warn" | "err";
  installedCount: number;
  onResetAll: () => void;
  onOpenMobileSidebar: () => void;
  filtersOpen: boolean;
  onToggleFilters: () => void;
  activeFiltersCount?: number;
  onScanInstalledFiles: (files: FileList) => void;
  onScanInstalledNative?: () => void;
  onOpenSettings: () => void;
  onOpenAbout: () => void;
}

export const Topbar: React.FC<TopbarProps> = React.memo(({
  query,
  onQueryCommit,
  onUrlPaste,
  contentType,
  onContentTypeChange,
  theme,
  onToggleTheme,
  cfStatus,
  installedCount,
  onResetAll,
  onOpenMobileSidebar,
  filtersOpen,
  onToggleFilters,
  activeFiltersCount = 0,
  onScanInstalledFiles,
  onScanInstalledNative,
  onOpenSettings,
  onOpenAbout,
}) => {
  const navRef = useRef<HTMLElement | null>(null);
  const searchInputRef = useRef<HTMLInputElement | null>(null);
  const searchWrapRef = useRef<HTMLDivElement | null>(null);
  const [helpOpen, setHelpOpen] = useState(false);
  const [ind, setInd] = useState({ left: 0, width: 0, ready: false });
  const [scrolled, setScrolled] = useState(false);
  const [localQuery, setLocalQuery] = useState(query);
  const debounceRef = useRef<number | null>(null);

  useEffect(() => {
    setLocalQuery(query);
  }, [query]);

  useEffect(() => {
    return () => {
      if (debounceRef.current) window.clearTimeout(debounceRef.current);
    };
  }, []);

  useEffect(() => {
    if (!helpOpen) return;
    const onDocDown = (e: MouseEvent) => {
      if (searchWrapRef.current && !searchWrapRef.current.contains(e.target as Node)) {
        setHelpOpen(false);
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setHelpOpen(false);
    };
    document.addEventListener("mousedown", onDocDown);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDocDown);
      window.removeEventListener("keydown", onKey);
    };
  }, [helpOpen]);

  const insertOperator = (snippet: string) => {
    const cur = localQuery.trim();
    const next = cur ? `${cur} ${snippet}` : snippet;
    setLocalQuery(next);
    setHelpOpen(false);
    searchInputRef.current?.focus();
    if (!snippet.endsWith(":")) {
      if (debounceRef.current) window.clearTimeout(debounceRef.current);
      onQueryCommit(next.trim());
    }
  };

  const syncInd = () => {
    const nav = navRef.current;
    if (!nav) return;
    const active = nav.querySelector<HTMLButtonElement>(`[data-type="${contentType}"]`);
    if (!active) return;
    setInd({ left: active.offsetLeft, width: active.offsetWidth, ready: true });
  };

  useLayoutEffect(() => {
    syncInd();
  }, [contentType]);

  useEffect(() => {
    const onScroll = () => setScrolled(getPageScrollTop() > 6);
    onScroll();
    const unsubScroll = subscribePageScroll(onScroll);
    window.addEventListener("resize", syncInd, { passive: true });
    return () => {
      unsubScroll();
      window.removeEventListener("resize", syncInd);
    };
  }, [contentType]);

  return (
    <header className={`topbar${scrolled ? " is-scrolled" : ""}`}>
      <div className="topbar-inner">
        <div className="topbar-left">
          <button
            className="btn btn-ghost btn-sm mobile-filters-btn"
            type="button"
            aria-label="Открыть фильтры"
            onClick={onOpenMobileSidebar}
          >
            Фильтры
          </button>
          <div className="brand" onClick={onResetAll} title="Сбросить фильтры">
            <img src="/assets/favicon.svg" alt="ModHub" className="brand-logo" />
            <div className="brand-text">
              <span className="brand-name">ModHub</span>
              <span className="brand-sub brand-sub-icons" title="Modrinth + CurseForge">
                <img className="platform-icon-img platform-icon-mr" src="/assets/modrinth.svg" width={15} height={15} alt="Modrinth" />
                <span className="brand-plus">+</span>
                <img className="platform-icon-img platform-icon-cf" src="/assets/curseforge.svg" width={16} height={16} alt="CurseForge" />
              </span>
            </div>
          </div>
        </div>

        <nav className="type-tabs" ref={navRef} aria-label="Тип контента">
          <span
            className={`type-tab-indicator${ind.ready ? " ready" : ""}`}
            style={{ width: `${ind.width}px`, transform: `translateX(${ind.left}px)` }}
            aria-hidden="true"
          />
          <button
            type="button"
            className={`type-tab${contentType === "mod" ? " active" : ""}`}
            data-type="mod"
            onClick={() => onContentTypeChange("mod")}
          >
            <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="m3.3 7 8.7 5 8.7-5"/><path d="M12 22V12"/></svg>
            <span>Моды</span>
          </button>
          <button
            type="button"
            className={`type-tab${contentType === "resourcepack" ? " active" : ""}`}
            data-type="resourcepack"
            onClick={() => onContentTypeChange("resourcepack")}
          >
            <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="3" width="18" height="18" rx="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/></svg>
            <span>Ресурспаки</span>
          </button>
          <button
            type="button"
            className={`type-tab${contentType === "shader" ? " active" : ""}`}
            data-type="shader"
            onClick={() => onContentTypeChange("shader")}
          >
            <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M12 3l1.9 5.6L19.5 10.5l-5.6 1.9L12 18l-1.9-5.6-5.6-1.9 5.6-1.9L12 3z"/><path d="M19 3v3M20.5 4.5h-3"/></svg>
            <span>Шейдеры</span>
          </button>
          <button
            type="button"
            className={`type-tab${contentType === "datapack" ? " active" : ""}`}
            data-type="datapack"
            onClick={() => onContentTypeChange("datapack")}
          >
            <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><polygon points="12 2 2 7 12 12 22 7 12 2"/><polyline points="2 12 12 17 22 12"/><polyline points="2 17 12 22 22 17"/></svg>
            <span>Датапаки</span>
          </button>
        </nav>

        {/* Clean Search in Topbar */}
        <div className="topbar-search" ref={searchWrapRef}>
          <div className={`search-input-wrap${helpOpen ? " is-help-open" : ""}`}>
            <svg className="search-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
              <circle cx="11" cy="11" r="7" />
              <path d="M20 20L16.65 16.65" />
            </svg>
            <input
              ref={searchInputRef}
              type="search"
              className={`search-input topbar-search-input topbar-search-clean${localQuery ? " has-value" : ""}`}
              placeholder="Найти мод, автора или вставить ссылку…"
              aria-label="Поиск модов, ресурспаков, шейдеров и датапаков"
              autoComplete="off"
              value={localQuery}
              onChange={(e) => {
                const v = e.target.value;
                if (isProjectUrlString(v)) {
                  if (debounceRef.current) window.clearTimeout(debounceRef.current);
                  setLocalQuery("");
                  onUrlPaste(v.trim());
                  return;
                }
                setLocalQuery(v);
                if (debounceRef.current) window.clearTimeout(debounceRef.current);
                const trimmed = v.trim();
                const hasDanglingOp = /(?:^|\s)-?(?:author|by|dep|depends|mc|v|version|dl|downloads|loader|modloader|source|platform|env|side|cat|category):(?:>=|<=|>|<|\.\.)?$/i.test(trimmed);
                if (hasDanglingOp) return;
                debounceRef.current = window.setTimeout(() => {
                  onQueryCommit(trimmed);
                }, 220);
              }}
              onPaste={(e) => {
                const pasted = e.clipboardData.getData("text");
                if (isProjectUrlString(pasted)) {
                  e.preventDefault();
                  if (debounceRef.current) window.clearTimeout(debounceRef.current);
                  setLocalQuery("");
                  onUrlPaste(pasted.trim());
                }
              }}
              onKeyDown={(e) => {
                if (e.key === "Enter") {
                  if (debounceRef.current) window.clearTimeout(debounceRef.current);
                  if (isProjectUrlString(localQuery)) {
                    setLocalQuery("");
                    onUrlPaste(localQuery.trim());
                  } else {
                    onQueryCommit(localQuery.trim());
                  }
                }
              }}
            />
            <button
              type="button"
              className={`search-clear-btn${localQuery ? " is-visible" : ""}`}
              title="Очистить поиск"
              aria-label="Очистить поиск"
              tabIndex={localQuery ? 0 : -1}
              onClick={() => {
                if (debounceRef.current) window.clearTimeout(debounceRef.current);
                setLocalQuery("");
                onQueryCommit("");
                searchInputRef.current?.focus();
              }}
            >
              <CloseIcon size={12} />
            </button>
            <button
              type="button"
              className={`search-help-btn${helpOpen ? " is-active" : ""}`}
              title="Справка по операторам расширенного поиска"
              aria-label="Справка по расширенному поиску"
              aria-expanded={helpOpen}
              aria-controls="search-help-popover"
              onClick={() => setHelpOpen((v) => !v)}
            >
              ?
            </button>
          </div>

          <div id="search-help-popover" className={`search-help-popover${helpOpen ? " is-open" : ""}`} role="region" aria-label="Расширенные возможности поиска" aria-hidden={!helpOpen}>
            <div className="search-help-head">
              <span>Расширенный поиск</span>
              <span className="search-help-sub">Дополнительно</span>
            </div>
            <p className="search-help-intro">Обычный поиск работает по названию и описанию — просто введите запрос. Команды ниже нужны только для точной настройки выдачи.</p>
            <div className="search-help-grid">
              <button type="button" className="search-op-btn" onClick={() => insertOperator("author:")}>
                <code>author:имя</code>
                <span>Фильтр по автору</span>
              </button>
              <button type="button" className="search-op-btn" onClick={() => insertOperator("dep:")}>
                <code>dep:мод</code>
                <span>Аддоны для мода</span>
              </button>
              <button type="button" className="search-op-btn" onClick={() => insertOperator("mc:1.21.1")}>
                <code>mc:1.21.1</code>
                <span>Версия Minecraft</span>
              </button>
              <button type="button" className="search-op-btn" onClick={() => insertOperator("dl:>100k")}>
                <code>dl:&gt;100k</code>
                <span>По числу загрузок</span>
              </button>
              <button type="button" className="search-op-btn" onClick={() => insertOperator('"exact"')}>
                <code>"фраза"</code>
                <span>Точное совпадение</span>
              </button>
              <button type="button" className="search-op-btn" onClick={() => insertOperator("loader:neoforge")}>
                <code>loader:neoforge</code>
                <span>Фильтр по загрузчику</span>
              </button>
              <button type="button" className="search-op-btn" onClick={() => insertOperator("source:mr")}>
                <code>source:mr / cf</code>
                <span>Только одна площадка</span>
              </button>
              <button type="button" className="search-op-btn" onClick={() => insertOperator("-fabric")}>
                <code>-слово</code>
                <span>Исключить из выдачи</span>
              </button>
            </div>
            <div className="search-help-foot">
              Также можно вставить прямую ссылку на проект Modrinth или CurseForge — карточка откроется сразу.
            </div>
          </div>
        </div>

        {/* CurseForge-style Filters Toggle Button */}
        <div className="topbar-right-group">
          <button
            id="catalogFilterToggleBtn"
            type="button"
            className={`btn-filter-toggle${filtersOpen ? " is-active" : ""}`}
            onClick={onToggleFilters}
            title={filtersOpen ? "Скрыть панель фильтров" : "Открыть панель фильтров"}
            aria-expanded={filtersOpen}
          >
            <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <line x1="4" y1="21" x2="4" y2="14" />
              <line x1="4" y1="10" x2="4" y2="3" />
              <line x1="12" y1="21" x2="12" y2="12" />
              <line x1="12" y1="8" x2="12" y2="3" />
              <line x1="20" y1="21" x2="20" y2="16" />
              <line x1="20" y1="12" x2="20" y2="3" />
              <line x1="1" y1="14" x2="7" y2="14" />
              <line x1="9" y1="8" x2="15" y2="8" />
              <line x1="17" y1="16" x2="23" y2="16" />
            </svg>
            <span>Фильтры</span>
            {activeFiltersCount > 0 && (
              <span className="filter-count-badge mono-num">{activeFiltersCount}</span>
            )}
          </button>

          <div className="topbar-actions">
            <button
              className="btn btn-ghost btn-sm btn-topbar-theme"
              type="button"
              onClick={onToggleTheme}
              title="Переключить светлую/тёмную тему"
              aria-label="Переключить тему"
            >
              <span className="theme-icon" aria-hidden="true">
                <svg className="theme-morph-svg" viewBox="0 0 24 24" fill="none">
                  <defs>
                    <mask id="themeMoonMask">
                      <rect width="24" height="24" fill="#fff" />
                      <circle className="tm-mask-cut" cx="16.5" cy="7.5" r="6.8" fill="#000" />
                    </mask>
                  </defs>
                  <circle className="tm-core" cx="12" cy="12" r="7.4" fill="currentColor" mask="url(#themeMoonMask)" />
                  <g className="tm-rays" stroke="currentColor" strokeWidth="2" strokeLinecap="round">
                    <line x1="12" y1="2" x2="12" y2="4.4" />
                    <line x1="12" y1="19.6" x2="12" y2="22" />
                    <line x1="4.93" y1="4.93" x2="6.63" y2="6.63" />
                    <line x1="17.37" y1="17.37" x2="19.07" y2="19.07" />
                    <line x1="2" y1="12" x2="4.4" y2="12" />
                    <line x1="19.6" y1="12" x2="22" y2="12" />
                    <line x1="4.93" y1="19.07" x2="6.63" y2="17.37" />
                    <line x1="17.37" y1="6.63" x2="19.07" y2="4.93" />
                  </g>
                </svg>
              </span>
              <span>{theme === "light" ? "Светлая" : "Тёмная"}</span>
            </button>

            <button
              className="btn btn-ghost btn-sm btn-topbar-about"
              type="button"
              onClick={onOpenAbout}
              title="О проекте ModHub"
              aria-label="О проекте"
            >
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
            </button>

            <button className="btn btn-ghost btn-sm btn-topbar-api" type="button" onClick={onOpenSettings} title="Настройки">
              <span className={`status-dot ${cfStatus}`} />
              <svg className="ui-ico api-sliders-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><line x1="4" y1="6" x2="20" y2="6"/><line x1="4" y1="12" x2="20" y2="12"/><line x1="4" y1="18" x2="20" y2="18"/><circle className="api-dot api-dot-1" cx="9" cy="6" r="2"/><circle className="api-dot api-dot-2" cx="15" cy="12" r="2"/><circle className="api-dot api-dot-3" cx="11" cy="18" r="2"/></svg>
              <span>Настройки</span>
            </button>
          </div>
        </div>
      </div>
    </header>
  );
});
