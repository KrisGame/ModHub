import React, { useCallback, useEffect, useState } from "react";
import { flushSync } from "react-dom";
import type { AccentPreset, BgPreset, ThemeMode, ViewMode } from "../types";

const THEME_KEY = "modhub_theme_v1";
const ACCENT_KEY = "modhub_accent_v1";
const BG_KEY = "modhub_bg_v1";
const VIEW_KEY = "modhub_view_v1";

function resolveInitialTheme(): { theme: ThemeMode; explicit: boolean } {
  try {
    const saved = localStorage.getItem(THEME_KEY);
    if (saved === "light" || saved === "dark") {
      return { theme: saved, explicit: true };
    }
  } catch {}
  const fromAttr = document.documentElement.getAttribute("data-theme");
  if (fromAttr === "light" || fromAttr === "dark") {
    return { theme: fromAttr, explicit: false };
  }
  const sysLight =
    typeof window !== "undefined" &&
    window.matchMedia &&
    window.matchMedia("(prefers-color-scheme: light)").matches;
  return { theme: sysLight ? "light" : "dark", explicit: false };
}

export function useThemeSettings() {
  const [themeState, setThemeState] = useState(resolveInitialTheme);
  const [accent, setAccent] = useState<AccentPreset>(() => {
    try {
      const v = localStorage.getItem(ACCENT_KEY);
      return (v === "graphite" || v === "emerald" || v === "amber" || v === "indigo" ? v : "mono") as AccentPreset;
    } catch {
      return "mono";
    }
  });
  const [bgPreset, setBgPreset] = useState<BgPreset>(() => {
    try {
      const v = localStorage.getItem(BG_KEY);
      return (v === "grid" || v === "noise" || v === "clean" ? v : "dots") as BgPreset;
    } catch {
      return "dots";
    }
  });
  const [viewMode, setViewMode] = useState<ViewMode>(() => {
    try {
      return localStorage.getItem(VIEW_KEY) === "list" ? "list" : "grid";
    } catch {
      return "grid";
    }
  });

  // Follow OS theme automatically while user hasn't chosen an explicit override
  useEffect(() => {
    if (themeState.explicit || typeof window === "undefined" || !window.matchMedia) return;
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const onChange = (e: MediaQueryListEvent) => {
      setThemeState({ theme: e.matches ? "light" : "dark", explicit: false });
    };
    mq.addEventListener("change", onChange);
    return () => mq.removeEventListener("change", onChange);
  }, [themeState.explicit]);

  useEffect(() => {
    document.documentElement.setAttribute("data-theme", themeState.theme);
    if (themeState.explicit) {
      try {
        localStorage.setItem(THEME_KEY, themeState.theme);
      } catch {}
    }
  }, [themeState]);

  useEffect(() => {
    document.documentElement.setAttribute("data-accent", accent);
    try {
      localStorage.setItem(ACCENT_KEY, accent);
    } catch {}
  }, [accent]);

  useEffect(() => {
    document.documentElement.setAttribute("data-bg", bgPreset);
    try {
      localStorage.setItem(BG_KEY, bgPreset);
    } catch {}
  }, [bgPreset]);

  useEffect(() => {
    try {
      localStorage.setItem(VIEW_KEY, viewMode);
    } catch {}
  }, [viewMode]);

  const handleToggleTheme = useCallback((e: React.MouseEvent<HTMLButtonElement>) => {
    const nextTheme: ThemeMode =
      document.documentElement.getAttribute("data-theme") === "light" ? "dark" : "light";

    const doc = document as Document & {
      startViewTransition?: (cb: () => void) => { finished: Promise<void> };
    };
    const reduced =
      window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

    if (typeof doc.startViewTransition !== "function" || reduced) {
      document.documentElement.setAttribute("data-theme", nextTheme);
      setThemeState({ theme: nextTheme, explicit: true });
      return;
    }

    const rect = e.currentTarget.getBoundingClientRect();
    const x = Math.round(rect.left + rect.width / 2);
    const y = Math.round(rect.top + rect.height / 2);
    const maxR = Math.ceil(
      Math.hypot(Math.max(x, window.innerWidth - x), Math.max(y, window.innerHeight - y))
    );

    const rootEl = document.documentElement;
    rootEl.style.setProperty("--vt-x", `${x}px`);
    rootEl.style.setProperty("--vt-y", `${y}px`);
    rootEl.style.setProperty("--vt-r", `${maxR}px`);
    rootEl.classList.add("theme-vt-active");

    const vt = doc.startViewTransition(() => {
      rootEl.setAttribute("data-theme", nextTheme);
      flushSync(() => {
        setThemeState({ theme: nextTheme, explicit: true });
      });
    });

    vt.finished.finally(() => {
      rootEl.classList.remove("theme-vt-active");
    });
  }, []);

  return {
    theme: themeState.theme,
    accent,
    setAccent,
    bgPreset,
    setBgPreset,
    viewMode,
    setViewMode,
    handleToggleTheme,
  };
}
