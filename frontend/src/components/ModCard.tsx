import React, { useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { CategoryIcon, EnvBadgeIcon, EnvBadgePill, LoaderIcon } from "../icons";
import type { FilterCategory, InstalledModInfo, ProjectCard, SortMode, ViewMode } from "../types";
import {
  cachedIconUrl,
  evaluatePackCompatibility,
  fallbackIconDataUri,
  filterRealLoaders,
  formatDate,
  formatLoaderName,
  formatMcVersionRange,
  formatNumber,
  getEnvKind,
  highlightSearchTermsHtml,
  isImageUrlLoaded,
  markImageUrlLoaded,
  mapCategoryTags,
  stripHtmlTags,
} from "../utils";

interface ModCardProps {
  item: ProjectCard;
  idx: number;
  query: string;
  sortMode: SortMode;
  viewMode: ViewMode;
  showLoadersCol: boolean;
  activeLoadersIn: string[];
  targetMcVersion: string;
  targetLoader: string;
  availableCategories: FilterCategory[];
  isQuickBusy: boolean;
  isCtxActive?: boolean;
  installedInfo?: InstalledModInfo;
  isProfileInstallMode?: boolean;
  onOpen: (item: ProjectCard) => void;
  onPrimaryAction: (item: ProjectCard, originEl?: HTMLElement | null) => void;
  onAuthorClick: (author: string) => void;
  onCategoryClick: (catId: string) => void;
  onContextMenu: (e: React.MouseEvent, item: ProjectCard) => void;
}

export const ModCard: React.FC<ModCardProps> = React.memo(({
  item,
  idx,
  query,
  sortMode,
  viewMode,
  showLoadersCol,
  activeLoadersIn,
  targetMcVersion,
  targetLoader,
  availableCategories,
  isQuickBusy,
  isCtxActive = false,
  installedInfo,
  isProfileInstallMode = false,
  onOpen,
  onPrimaryAction,
  onAuthorClick,
  onCategoryClick,
  onContextMenu,
}) => {
  const cardRef = useRef<HTMLElement | null>(null);
  const ct = item.content_type || "mod";
  const supportsLoaders = ct === "mod" || ct === "datapack";

  const inSet = useMemo(() => (activeLoadersIn.length > 0 ? new Set(activeLoadersIn) : null), [activeLoadersIn]);

  const orderedLoaders = useMemo(() => {
    const rawLoaders = supportsLoaders ? filterRealLoaders(item.loaders || [], ct) : [];
    if (!inSet || rawLoaders.length <= 1) return rawLoaders;
    return [
      ...rawLoaders.filter((l) => Boolean(inSet && inSet.has(l))),
      ...rawLoaders.filter((l) => !Boolean(inSet && inSet.has(l))),
    ];
  }, [supportsLoaders, item.loaders, ct, inSet]);

  const { allCats, shownCats, hiddenCats } = useMemo(() => {
    const mapped = mapCategoryTags(item.categories || [], availableCategories, 0);
    return { allCats: mapped, shownCats: mapped.slice(0, 2), hiddenCats: mapped.slice(2) };
  }, [item.categories, availableCategories]);

  const fallback = fallbackIconDataUri(item.title);
  const rawIcon = item.icon_url || "";
  const primaryIcon = rawIcon ? cachedIconUrl(rawIcon, 144) : fallback;
  const [imgSrc, setImgSrc] = useState(primaryIcon);
  const [imgReady, setImgReady] = useState(() => isImageUrlLoaded(primaryIcon));

  useEffect(() => {
    setImgSrc(primaryIcon);
    setImgReady(isImageUrlLoaded(primaryIcon));
  }, [primaryIcon]);

  const handleImgReady = (src: string) => {
    markImageUrlLoaded(src);
    setImgReady(true);
  };

  const handleImgRef = (el: HTMLImageElement | null) => {
    if (el && el.complete && el.naturalWidth > 0 && !imgReady) {
      markImageUrlLoaded(imgSrc);
      setImgReady(true);
    }
  };

  const handleImgError = () => {
    if (rawIcon && imgSrc !== rawIcon && imgSrc !== fallback) {
      setImgSrc(rawIcon);
      setImgReady(isImageUrlLoaded(rawIcon));
    } else if (imgSrc !== fallback) {
      setImgSrc(fallback);
      setImgReady(true);
    } else {
      setImgReady(true);
    }
  };

  const envKind = ct === "mod" ? getEnvKind(item) : "";
  const mcRange = useMemo(() => formatMcVersionRange(item.game_versions), [item.game_versions]);
  const compat = useMemo(
    () => evaluatePackCompatibility(item, targetMcVersion, supportsLoaders ? targetLoader : ""),
    [item, targetMcVersion, supportsLoaders, targetLoader]
  );
  const trimmedQuery = query.trim();
  const hasQuery = trimmedQuery.length > 0;
  const cleanSummary = useMemo(() => stripHtmlTags(item.summary) || "Без описания", [item.summary]);

  const { displayIso, isFresh, isStale, dateTitle } = useMemo(() => {
    const iso = sortMode === "newest" && item.date_created ? item.date_created : item.date_modified;
    const ms = iso ? Date.parse(iso) : NaN;
    const age = Number.isNaN(ms) ? NaN : (Date.now() - ms) / 86_400_000;
    const fresh = !Number.isNaN(age) && age <= 7;
    const stale = sortMode !== "newest" && !Number.isNaN(age) && age >= 365;
    const title =
      sortMode === "newest" && item.date_created
        ? `Создан: ${formatDate(item.date_created)} • Обновлён: ${formatDate(item.date_modified)}`
        : fresh
          ? "Обновлено за последние 7 дней"
          : stale
            ? "Не обновлялось более года"
            : undefined;
    return { displayIso: iso, isFresh: fresh, isStale: stale, dateTitle: title };
  }, [sortMode, item.date_created, item.date_modified]);

  const dep = item.dependent_info;
  const depRelCls = dep
    ? dep.relation === "required"
      ? "pill-dep-req"
      : dep.relation === "incompatible"
        ? "pill-dep-incompat"
        : "pill-dep-rel"
    : "";

  const prefetchTimerRef = useRef<number | null>(null);

  useEffect(() => {
    return () => {
      if (prefetchTimerRef.current) window.clearTimeout(prefetchTimerRef.current);
    };
  }, []);

  const handleMouseEnter = () => {
    if (ct === "modpack") return;
    if (prefetchTimerRef.current) window.clearTimeout(prefetchTimerRef.current);
    prefetchTimerRef.current = window.setTimeout(() => {
      api.prefetchModDetails(item);
    }, 120);
  };

  const handleMouseMove = (e: React.MouseEvent<HTMLElement>) => {
    const el = cardRef.current;
    if (!el) return;
    const rect = el.getBoundingClientRect();
    el.style.setProperty("--mx", `${(e.clientX - rect.left).toFixed(0)}px`);
    el.style.setProperty("--my", `${(e.clientY - rect.top).toFixed(0)}px`);
  };

  const handleMouseLeave = () => {
    if (prefetchTimerRef.current) {
      window.clearTimeout(prefetchTimerRef.current);
      prefetchTimerRef.current = null;
    }
  };

  const handleCardOpen = () => {
    onOpen(item);
  };


  const isInstalled = Boolean(installedInfo && !installedInfo.has_update);

  const actionBtn =
    ct !== "modpack" ? (
      <div className="card-quick-actions">
        {isQuickBusy ? (
          <button type="button" className="btn btn-ghost btn-xs btn-quick-add is-loading" disabled aria-busy="true">
            <span className="spinner spinner-xs" aria-hidden="true" />
            <span>Установка…</span>
          </button>
        ) : isInstalled && isProfileInstallMode ? (
          <button
            type="button"
            className="btn btn-xs btn-ghost btn-quick-add is-installed"
            disabled
            aria-disabled="true"
            title={`Уже установлен в выбранный профиль (${installedInfo?.filename || installedInfo?.installed_version_number || "локально"})`}
            onClick={(e) => e.stopPropagation()}
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <span>В профиле</span>
          </button>
        ) : isInstalled ? (
          <button
            type="button"
            className="btn btn-xs btn-ghost btn-quick-add is-installed"
            title={`Найден локально (${installedInfo?.installed_version_number || "установлен"}) — нажмите, чтобы установить в нужный профиль`}
            onClick={(e) => {
              e.stopPropagation();
              onPrimaryAction(item, e.currentTarget);
            }}
          >
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <polyline points="20 6 9 17 4 12" />
            </svg>
            <span>В профиле</span>
          </button>
        ) : (
          <button
            type="button"
            className="btn btn-xs btn-primary btn-quick-add btn-quick-dl"
            title="Установить в профиль Minecraft"
            onClick={(e) => {
              e.stopPropagation();
              onPrimaryAction(item, e.currentTarget);
            }}
          >
            <svg className="dl-arrow-ico" width="12.5" height="12.5" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.3" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
              <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
              <g className="dl-arrow-head">
                <polyline points="7 10 12 15 17 10" />
                <line x1="12" y1="15" x2="12" y2="3" />
              </g>
            </svg>
            <span>{installedInfo?.has_update ? "Обновить" : "В профиль"}</span>
          </button>
        )}
      </div>
    ) : null;

  const hasBothPlatforms = (item.platforms || []).includes("modrinth") && (item.platforms || []).includes("curseforge");

  const renderPlatformChips = (icoSize: number) =>
    hasBothPlatforms ? (
      <span className="plat-dual-capsule" title="Доступно на Modrinth и CurseForge">
        <img className="platform-icon-img platform-icon-mr" src="/assets/modrinth.svg" width={icoSize} height={icoSize} alt="Modrinth" />
        <span className="plat-dual-sep" aria-hidden="true" />
        <img className="platform-icon-img platform-icon-cf" src="/assets/curseforge.svg" width={icoSize} height={icoSize} alt="CurseForge" />
      </span>
    ) : (
      (item.platforms || []).map((p) => (
        <span key={p} className="plat-ico-chip" title={p === "modrinth" ? "Modrinth" : "CurseForge"}>
          <img
            className={`platform-icon-img ${p === "modrinth" ? "platform-icon-mr" : "platform-icon-cf"}`}
            src={`/assets/${p}.svg`}
            width={icoSize}
            height={icoSize}
            alt={p}
          />
        </span>
      ))
    );

  // Status is communicated directly via the card action button ("В профиле" / "Обновить")
  const cornerStatusBadge = null;

  // TABULAR + MODRINTH LIST VIEW (full-height icon, icon+name pills for loaders & env, adaptive 3/4 columns)
  if (viewMode === "list") {
    return (
      <article
        ref={cardRef}
        className={`mod-card tbl-list-row${showLoadersCol ? "" : " no-loaders-col"}${isCtxActive ? " is-ctx-active" : ""}`}
        onClick={handleCardOpen}
        onContextMenu={(e) => onContextMenu(e, item)}
        onMouseEnter={handleMouseEnter}
        onMouseMove={handleMouseMove}
        onMouseLeave={handleMouseLeave}
      >
        <span className="card-top-sheen" aria-hidden="true" />
        <div className="tbl-col-main">
          <div className={`card-icon-wrap is-list img-skel-host${imgReady ? " is-img-ready" : ""}`}>
            <span className="img-skel-shimmer" aria-hidden="true" />
            <img
              width={72}
              height={72}
              className={`card-icon tbl-row-icon icon-smooth-img${imgReady ? " is-loaded" : ""}`}
              src={imgSrc}
              alt=""
              loading={idx < 10 ? "eager" : "lazy"}
              decoding="async"
              onLoad={() => handleImgReady(imgSrc)}
              ref={handleImgRef}
              onError={handleImgError}
            />
            {cornerStatusBadge}
          </div>
          <div className="tbl-col-text">
            <div className="tbl-title-line">
              {hasQuery ? (
                <button
                  type="button"
                  className="card-title tbl-row-title mh-project-title-button"
                  onClick={(event) => { event.stopPropagation(); handleCardOpen(); }}
                  title={item.title}
                  dangerouslySetInnerHTML={{ __html: highlightSearchTermsHtml(item.title, trimmedQuery) }}
                />
              ) : (
                <button type="button" className="card-title tbl-row-title mh-project-title-button" title={item.title} onClick={(event) => { event.stopPropagation(); handleCardOpen(); }}>
                  {item.title}
                </button>
              )}
              <span className="tbl-row-author">
                от{" "}
                {(item.authors || []).slice(0, 2).map((a, i) => (
                  <React.Fragment key={a}>
                    {i > 0 && ", "}
                    <button
                      type="button"
                      className="author-link"
                      onClick={(e) => {
                        e.stopPropagation();
                        onAuthorClick(a);
                      }}
                    >
                      {a}
                    </button>
                  </React.Fragment>
                ))}
                {(!item.authors || item.authors.length === 0) && "—"}
              </span>
              {renderPlatformChips(12)}
              {!showLoadersCol && (compat.state !== "none" ? (
                <span className={`compat-badge is-${compat.state}`} title={compat.title}>
                  <span className="compat-dot" />
                  <span>{compat.label}</span>
                </span>
              ) : (
                mcRange && (
                  <span className="mc-ver-pill mono-num" title={`Поддерживаемые версии MC: ${mcRange.full}`}>
                    MC {mcRange.label}
                  </span>
                )
              ))}
              {dep && <span className={`pill ${depRelCls}`}>{dep.label}</span>}
              {ct === "modpack" && <span className="pill pill-cat">Модпак</span>}
            </div>

            {hasQuery ? (
              <p
                className="tbl-summary-text"
                dangerouslySetInnerHTML={{
                  __html: highlightSearchTermsHtml(cleanSummary, trimmedQuery),
                }}
              />
            ) : (
              <p className="tbl-summary-text">{cleanSummary}</p>
            )}

            <div className="tbl-cats-row">
              {allCats.slice(0, 3).map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className="pill pill-cat js-cat-tag"
                  onClick={(e) => {
                    e.stopPropagation();
                    onCategoryClick(c.id);
                  }}
                >
                  <CategoryIcon id={c.id} size={11.5} />
                  <span>{c.label}</span>
                </button>
              ))}
              {allCats.length > 3 && (
                <span className="pill pill-cat-more mono-num" title={allCats.slice(3).map((x) => x.label).join(", ")}>
                  +{allCats.length - 3}
                </span>
              )}
            </div>
          </div>
        </div>

        {showLoadersCol && (
          <div className="tbl-col-loaders">
            {orderedLoaders.map((l) => (
              <span
                key={l}
                className={`tbl-chip-tag tbl-chip-loader loader-${l}${Boolean(inSet && inSet.has(l)) ? " is-active-loader" : ""}`}
                title={formatLoaderName(l)}
              >
                <LoaderIcon loader={l} size={12} />
                <span>{formatLoaderName(l)}</span>
              </span>
            ))}
            {ct === "datapack" && orderedLoaders.length === 0 && (
              <span className="tbl-chip-tag tbl-chip-loader loader-datapack">
                <LoaderIcon loader="datapack" size={12} />
                <span>Datapack</span>
              </span>
            )}
            {envKind && <EnvBadgePill env={envKind} />}
            {compat.state !== "none" ? (
              <span className={`compat-badge is-${compat.state}`} title={compat.title}>
                <span className="compat-dot" />
                <span>{compat.label}</span>
              </span>
            ) : (
              mcRange && (
                <span className="mc-ver-pill mono-num" title={`Поддерживаемые версии MC: ${mcRange.full}`}>
                  MC {mcRange.label}
                </span>
              )
            )}
          </div>
        )}

        <div className="tbl-col-stats mono-num">
          <span className="card-stat-item">
            <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            <span>{formatNumber(item.downloads)}</span>
          </span>
          <span className={`card-stat-item card-footer-date${isStale ? " is-stale" : ""}`} title={dateTitle}>
            {isFresh ? (
              <span className="fresh-dot" aria-hidden="true" />
            ) : (
              <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
            )}
            <span>{formatDate(displayIso)}</span>
          </span>
        </div>

        <div className="tbl-col-action">{actionBtn}</div>
      </article>
    );
  }

  // GRID CARD VIEW
  return (
    <article
      ref={cardRef}
      className={`mod-card${isCtxActive ? " is-ctx-active" : ""}`}
      onClick={handleCardOpen}
      onContextMenu={(e) => onContextMenu(e, item)}
      onMouseEnter={handleMouseEnter}
      onMouseMove={handleMouseMove}
      onMouseLeave={handleMouseLeave}
    >
      <span className="card-top-sheen" aria-hidden="true" />
      <div className="card-head">
        <div className={`card-icon-wrap img-skel-host${imgReady ? " is-img-ready" : ""}`}>
          <span className="img-skel-shimmer" aria-hidden="true" />
          <img
            width={60}
            height={60}
            className={`card-icon icon-smooth-img${imgReady ? " is-loaded" : ""}`}
            src={imgSrc}
            alt=""
            loading={idx < 8 ? "eager" : "lazy"}
            decoding="async"
            onLoad={() => handleImgReady(imgSrc)}
            ref={handleImgRef}
            onError={handleImgError}
          />
          {cornerStatusBadge}
        </div>
        <div className="card-title-wrap">
          {hasQuery ? (
            <button
              type="button"
              className="card-title mh-project-title-button"
              onClick={(event) => { event.stopPropagation(); handleCardOpen(); }}
              title={item.title}
              dangerouslySetInnerHTML={{ __html: highlightSearchTermsHtml(item.title, trimmedQuery) }}
            />
          ) : (
            <button type="button" className="card-title mh-project-title-button" title={item.title} onClick={(event) => { event.stopPropagation(); handleCardOpen(); }}>
              {item.title}
            </button>
          )}
          <div className="card-author">
            от{" "}
            {(item.authors || []).slice(0, 2).map((a, i) => (
              <React.Fragment key={a}>
                {i > 0 && ", "}
                <button
                  type="button"
                  className="author-link"
                  onClick={(e) => {
                    e.stopPropagation();
                    onAuthorClick(a);
                  }}
                >
                  {a}
                </button>
              </React.Fragment>
            ))}
            {(!item.authors || item.authors.length === 0) && "—"}
          </div>

          <div className="card-meta-icons-row">
            {renderPlatformChips(12.5)}

            {supportsLoaders && orderedLoaders.length > 0 && (
              <>
                <span className="meta-ico-sep" aria-hidden="true" />
                {orderedLoaders.map((l) => (
                  <span
                    key={l}
                    className={`meta-ico-badge${Boolean(inSet && inSet.has(l)) ? " is-active-loader" : ""}`}
                    title={formatLoaderName(l)}
                  >
                    <LoaderIcon loader={l} size={12} />
                  </span>
                ))}
              </>
            )}

            {envKind && <EnvBadgeIcon env={envKind} />}

            {compat.state !== "none" ? (
              <span className={`compat-badge is-${compat.state}`} title={compat.title}>
                <span className="compat-dot" />
                <span>{compat.label}</span>
              </span>
            ) : (
              mcRange && (
                <span className="mc-ver-pill mono-num" title={`Поддерживаемые версии MC: ${mcRange.full}`}>
                  MC {mcRange.label}
                </span>
              )
            )}

            {dep && (
              <span className={`pill ${depRelCls}`} title={`Зависимость: ${dep.label}`}>
                {dep.label}
              </span>
            )}
            {ct === "modpack" && <span className="pill pill-cat">Модпак</span>}
          </div>
        </div>
      </div>

      {hasQuery ? (
        <p
          className="card-summary"
          dangerouslySetInnerHTML={{
            __html: highlightSearchTermsHtml(cleanSummary, trimmedQuery),
          }}
        />
      ) : (
        <p className="card-summary">{cleanSummary}</p>
      )}

      <div className="card-cats">
        {shownCats.map((c) => (
          <button
            key={c.id}
            type="button"
            className="pill pill-cat js-cat-tag"
            title={`Фильтровать по категории: ${c.label}`}
            onClick={(e) => {
              e.stopPropagation();
              onCategoryClick(c.id);
            }}
          >
            <CategoryIcon id={c.id} size={11.5} />
            <span>{c.label}</span>
          </button>
        ))}
        {hiddenCats.length > 0 && (
          <span className="pill pill-cat-more mono-num" title={hiddenCats.map((x) => x.label).join(", ")}>
            +{hiddenCats.length}
          </span>
        )}
      </div>

      <div className="card-footer">
        <div className="card-footer-meta">
          <span className="card-stat-item">
            <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
            <span>{formatNumber(item.downloads)}</span>
          </span>
          <span className={`card-stat-item card-footer-date${isStale ? " is-stale" : ""}`} title={dateTitle}>
            {isFresh ? (
              <span className="fresh-dot" aria-hidden="true" />
            ) : (
              <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round"><rect x="3" y="4" width="18" height="18" rx="2"/><line x1="16" y1="2" x2="16" y2="6"/><line x1="8" y1="2" x2="8" y2="6"/><line x1="3" y1="10" x2="21" y2="10"/></svg>
            )}
            <span>{formatDate(displayIso)}</span>
          </span>
        </div>
        {actionBtn}
      </div>
    </article>
  );
});
