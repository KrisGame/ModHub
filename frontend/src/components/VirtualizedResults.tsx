import React, { useLayoutEffect, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import type { FilterCategory, InstalledModInfo, ProjectCard, SortMode, ViewMode } from "../types";
import { ModCard } from "./ModCard";
import { getPageScroller } from "../smoothScroll";

interface VirtualizedResultsProps {
  items: ProjectCard[];
  query: string;
  sortMode: SortMode;
  viewMode: ViewMode;
  showLoadersCol: boolean;
  activeLoadersIn: string[];
  targetMcVersion: string;
  targetLoader: string;
  availableCategories: FilterCategory[];
  quickBusySet: Set<string>;
  installedMap: Record<string, InstalledModInfo>;
  isProfileInstallMode?: boolean;
  resolveInstalledInfo?: (item: ProjectCard) => InstalledModInfo | undefined;
  isRefreshing: boolean;
  ctxActiveGroupId?: string;
  onOpen: (item: ProjectCard) => void;
  onPrimaryAction: (item: ProjectCard, originEl?: HTMLElement | null) => void;
  onAuthorClick: (author: string) => void;
  onCategoryClick: (catId: string) => void;
  onContextMenu: (e: React.MouseEvent, item: ProjectCard) => void;
}

const GRID_ROW_SIZE = 216; // 204px fixed card height + 12px fixed vertical gap
const LIST_ROW_SIZE = 124; // 114px fixed list row height + 10px fixed vertical gap

function defaultGetCardInstalled(
  item: ProjectCard,
  installedMap: Record<string, InstalledModInfo>
): InstalledModInfo | undefined {
  if (installedMap[item.group_id]) {
    return installedMap[item.group_id];
  }
  if (!item.sources) return undefined;
  for (const s of item.sources) {
    if (installedMap[s.id]) {
      return installedMap[s.id];
    }
    if (s.source_key && installedMap[s.source_key]) {
      return installedMap[s.source_key];
    }
  }
  return undefined;
}

export const VirtualizedResults: React.FC<VirtualizedResultsProps> = React.memo(({
  items,
  query,
  sortMode,
  viewMode,
  showLoadersCol,
  activeLoadersIn,
  targetMcVersion,
  targetLoader,
  availableCategories,
  quickBusySet,
  installedMap,
  isProfileInstallMode = false,
  resolveInstalledInfo,
  isRefreshing,
  ctxActiveGroupId = "",
  onOpen,
  onPrimaryAction,
  onAuthorClick,
  onCategoryClick,
  onContextMenu,
}) => {
  const containerRef = useRef<HTMLElement | null>(null);
  const [cols, setCols] = useState(4);
  const [scrollMargin, setScrollMargin] = useState(0);

  useLayoutEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const syncMetrics = () => {
      const scroller = getPageScroller();
      const rect = el.getBoundingClientRect();
      const top = scroller
        ? Math.max(0, Math.round(rect.top - scroller.getBoundingClientRect().top + scroller.scrollTop))
        : Math.max(0, Math.round(rect.top + window.scrollY));
      setScrollMargin((prev) => (Math.abs(prev - top) > 1 ? top : prev));

      if (viewMode === "list") {
        setCols(1);
      } else {
        const w = el.clientWidth;
        const nextCols = w >= 880 ? 4 : w >= 620 ? 3 : w >= 420 ? 2 : 1;
        setCols((prev) => (prev === nextCols ? prev : nextCols));
      }
    };

    syncMetrics();
    const rafSync = requestAnimationFrame(syncMetrics);

    const ro = new ResizeObserver(syncMetrics);
    ro.observe(el);
    if (el.parentElement) ro.observe(el.parentElement);
    window.addEventListener("resize", syncMetrics, { passive: true });
    return () => {
      cancelAnimationFrame(rafSync);
      ro.disconnect();
      window.removeEventListener("resize", syncMetrics);
    };
  }, [viewMode]);

  const effectiveCols = viewMode === "list" ? 1 : cols;
  const rowCount = Math.ceil(items.length / effectiveCols);
  const rowHeight = viewMode === "list" ? LIST_ROW_SIZE : GRID_ROW_SIZE;

  const virtualizer = useVirtualizer({
    count: rowCount,
    getScrollElement: () => getPageScroller() || document.documentElement,
    estimateSize: () => rowHeight,
    overscan: 6,
    scrollMargin,
  });

  useLayoutEffect(() => {
    virtualizer.measure();
  }, [viewMode, effectiveCols, rowHeight, virtualizer]);

  const virtualRows = virtualizer.getVirtualItems();
  const totalHeight = virtualizer.getTotalSize();

  return (
    <section
      ref={containerRef}
      className={`results-grid is-virtualized${viewMode === "list" ? " view-list" : ""}${isRefreshing ? " is-refreshing" : ""}${ctxActiveGroupId ? " has-ctx-focus" : ""}`}
      style={{ height: `${totalHeight}px` }}
    >
      {virtualRows.map((vRow) => {
        const startIdx = vRow.index * effectiveCols;
        const rowItems = items.slice(startIdx, startIdx + effectiveCols);
        const yOffset = vRow.start - virtualizer.options.scrollMargin;

        if (viewMode === "list") {
          const item = rowItems[0];
          if (!item) return null;
          const instInfo = resolveInstalledInfo ? resolveInstalledInfo(item) : defaultGetCardInstalled(item, installedMap);
          return (
            <div
              key={`list-${item.group_id}`}
              data-index={vRow.index}
              className="virtual-row virtual-list-row"
              style={{
                height: `${LIST_ROW_SIZE}px`,
                transform: `translate3d(0, ${yOffset}px, 0)`,
              }}
            >
              <ModCard
                item={item}
                idx={startIdx}
                query={query}
                sortMode={sortMode}
                viewMode="list"
                showLoadersCol={showLoadersCol}
                activeLoadersIn={activeLoadersIn}
                targetMcVersion={targetMcVersion}
                targetLoader={targetLoader}
                availableCategories={availableCategories}
                isQuickBusy={quickBusySet.has(item.group_id)}
                isCtxActive={ctxActiveGroupId === item.group_id}
                installedInfo={instInfo}
                isProfileInstallMode={isProfileInstallMode}
                onOpen={onOpen}
                onPrimaryAction={onPrimaryAction}
                onAuthorClick={onAuthorClick}
                onCategoryClick={onCategoryClick}
                onContextMenu={onContextMenu}
              />
            </div>
          );
        }

        return (
          <div
            key={`grid-row-${vRow.index}`}
            data-index={vRow.index}
            className="virtual-row virtual-grid-row"
            style={{
              height: `${GRID_ROW_SIZE}px`,
              transform: `translate3d(0, ${yOffset}px, 0)`,
              gridTemplateColumns: `repeat(${effectiveCols}, minmax(0, 1fr))`,
            }}
          >
            {rowItems.map((item, cIdx) => {
              const instInfo = resolveInstalledInfo ? resolveInstalledInfo(item) : defaultGetCardInstalled(item, installedMap);
              return (
                <ModCard
                  key={item.group_id}
                  item={item}
                  idx={startIdx + cIdx}
                  query={query}
                  sortMode={sortMode}
                  viewMode="grid"
                  showLoadersCol={showLoadersCol}
                  activeLoadersIn={activeLoadersIn}
                  targetMcVersion={targetMcVersion}
                  targetLoader={targetLoader}
                  availableCategories={availableCategories}
                  isQuickBusy={quickBusySet.has(item.group_id)}
                  isCtxActive={ctxActiveGroupId === item.group_id}
                  installedInfo={instInfo}
                  isProfileInstallMode={isProfileInstallMode}
                  onOpen={onOpen}
                  onPrimaryAction={onPrimaryAction}
                  onAuthorClick={onAuthorClick}
                  onCategoryClick={onCategoryClick}
                  onContextMenu={onContextMenu}
                />
              );
            })}
          </div>
        );
      })}
    </section>
  );
});
