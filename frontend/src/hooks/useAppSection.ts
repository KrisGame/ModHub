import { useCallback, useEffect, useState } from "react";
import type { AppSection } from "../components/AppNavRail";
import { desktopBridge } from "../services/bridge";

const sections: readonly string[] = ["home", "instances", "catalog", "downloads", "accounts", "settings"];

function readSection(): AppSection {
  const section = new URLSearchParams(window.location.search).get("section");
  return section && sections.includes(section) ? section as AppSection : "catalog";
}

export function useAppSection() {
  const desktop = desktopBridge.isDesktop();
  const [section, setSection] = useState<AppSection>(() => desktop ? "catalog" : readSection());

  useEffect(() => {
    if (desktop) return;
    const onPopState = () => setSection(readSection());
    window.addEventListener("popstate", onPopState);
    return () => window.removeEventListener("popstate", onPopState);
  }, [desktop]);

  const navigate = useCallback((next: AppSection) => {
    if (!desktop && next !== readSection()) {
      const url = new URL(window.location.href);
      if (next === "catalog") url.searchParams.delete("section");
      else url.searchParams.set("section", next);
      window.history.pushState(null, "", url);
    }
    setSection(next);
  }, [desktop]);

  return [section, navigate] as const;
}
