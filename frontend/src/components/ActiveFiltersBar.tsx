import React, { useEffect, useRef, useState } from "react";
import { CloseIcon } from "../icons";
import type { FilterCategory } from "../types";
import { formatLoaderName, formatNumber } from "../utils";

interface ActiveFiltersBarProps {
  query: string;
  version: string;
  loadersIn: string[];
  loadersOut: string[];
  categoriesIn: string[];
  categoriesOut: string[];
  availableCategories: FilterCategory[];
  env: string;
  authorFilter: string;
  minDownloads: number;
  updatedWithin: number;
  hideAbandoned: boolean;
  onClearQuery: () => void;
  onClearVersion: () => void;
  onRemoveLoaderIn: (l: string) => void;
  onRemoveLoaderOut: (l: string) => void;
  onRemoveCatIn: (c: string) => void;
  onRemoveCatOut: (c: string) => void;
  onClearEnv: () => void;
  onClearAuthor: () => void;
  onClearMinDownloads: () => void;
  onClearUpdated: () => void;
  onClearAbandoned: () => void;
  onResetAll: () => void;
}

interface ChipSpec {
  key: string;
  label: string;
  excluded?: boolean;
  onRemove: () => void;
}

export const ActiveFiltersBar: React.FC<ActiveFiltersBarProps> = ({
  query,
  version,
  loadersIn,
  loadersOut,
  categoriesIn,
  categoriesOut,
  availableCategories,
  env,
  authorFilter,
  minDownloads,
  updatedWithin,
  hideAbandoned,
  onClearQuery,
  onClearVersion,
  onRemoveLoaderIn,
  onRemoveLoaderOut,
  onRemoveCatIn,
  onRemoveCatOut,
  onClearEnv,
  onClearAuthor,
  onClearMinDownloads,
  onClearUpdated,
  onClearAbandoned,
  onResetAll,
}) => {
  const catLabelMap = new Map<string, string>();
  for (const c of availableCategories) catLabelMap.set(c.id, c.label);

  const chips: ChipSpec[] = [];
  if (query.trim()) {
    chips.push({ key: "q", label: `Поиск: "${query.trim()}"`, onRemove: onClearQuery });
  }
  if (version) {
    chips.push({ key: "ver", label: `MC ${version}`, onRemove: onClearVersion });
  }
  for (const l of loadersIn) {
    chips.push({ key: `lin:${l}`, label: formatLoaderName(l), onRemove: () => onRemoveLoaderIn(l) });
  }
  for (const l of loadersOut) {
    chips.push({ key: `lout:${l}`, label: `Без ${formatLoaderName(l)}`, excluded: true, onRemove: () => onRemoveLoaderOut(l) });
  }
  for (const c of categoriesIn) {
    chips.push({ key: `cin:${c}`, label: catLabelMap.get(c) || c, onRemove: () => onRemoveCatIn(c) });
  }
  for (const c of categoriesOut) {
    chips.push({ key: `cout:${c}`, label: `Без ${catLabelMap.get(c) || c}`, excluded: true, onRemove: () => onRemoveCatOut(c) });
  }
  if (env) {
    const envLbl =
      env === "client_only" || env === "client"
        ? "Только клиент"
        : env === "server_only" || env === "server"
          ? "Только сервер"
          : env === "both"
            ? "Клиент и сервер"
            : env;
    chips.push({ key: "env", label: envLbl, onRemove: onClearEnv });
  }
  if (authorFilter.trim()) {
    chips.push({ key: "author", label: `Автор: ${authorFilter.trim()}`, onRemove: onClearAuthor });
  }
  if (minDownloads > 0) {
    chips.push({ key: "mindl", label: `Загрузок ≥ ${formatNumber(minDownloads)}`, onRemove: onClearMinDownloads });
  }
  if (updatedWithin > 0) {
    chips.push({ key: "upd", label: `Обновлён ≤ ${updatedWithin} дн.`, onRemove: onClearUpdated });
  }
  if (hideAbandoned) {
    chips.push({ key: "abn", label: "Без заброшенных", onRemove: onClearAbandoned });
  }

  const [leavingKeys, setLeavingKeys] = useState<Set<string>>(() => new Set());
  const pendingRemovalsRef = useRef<Map<string, () => void>>(new Map());
  const timersRef = useRef<Map<string, number>>(new Map());

  useEffect(() => {
    const activeKeys = new Set(chips.map((c) => c.key));
    setLeavingKeys((prev) => {
      const next = new Set(prev);
      for (const k of next) {
        if (activeKeys.has(k)) {
          next.delete(k);
          const tm = timersRef.current.get(k);
          if (tm) window.clearTimeout(tm);
          timersRef.current.delete(k);
          pendingRemovalsRef.current.delete(k);
        }
      }
      return next;
    });
  }, [chips.length]);

  const triggerChipRemove = (ch: ChipSpec) => {
    if (leavingKeys.has(ch.key)) return;
    pendingRemovalsRef.current.set(ch.key, ch.onRemove);
    setLeavingKeys((prev) => {
      const next = new Set(prev);
      next.add(ch.key);
      return next;
    });
    const tid = window.setTimeout(() => {
      timersRef.current.delete(ch.key);
      const fn = pendingRemovalsRef.current.get(ch.key);
      pendingRemovalsRef.current.delete(ch.key);
      setLeavingKeys((prev) => {
        const next = new Set(prev);
        next.delete(ch.key);
        return next;
      });
      fn?.();
    }, 190);
    timersRef.current.set(ch.key, tid);
  };

  const shown = chips.length > 0;

  return (
    <div className={`reveal-slot reveal-slot-tight${shown ? " is-shown" : ""}`}>
      <div className="reveal-inner">
        <div className="active-filters-bar" aria-label="Активные фильтры">
          {chips.map((ch) => {
            const isLeaving = leavingKeys.has(ch.key);
            return (
              <span
                key={ch.key}
                className={`af-chip${ch.excluded ? " is-excluded" : ""}${isLeaving ? " is-leaving" : ""}`}
              >
                <span>{ch.label}</span>
                <button
                  type="button"
                  className="af-chip-x"
                  aria-label="Убрать"
                  onClick={() => triggerChipRemove(ch)}
                >
                  <CloseIcon size={11} />
                </button>
              </span>
            );
          })}
          {chips.length >= 2 && (
            <button type="button" className="af-reset-btn" onClick={onResetAll}>
              Сбросить все
            </button>
          )}
        </div>
      </div>
    </div>
  );
};
