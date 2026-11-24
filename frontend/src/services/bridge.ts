import type { CartItem, JavaRuntimeInfo } from "../types";
import type {
  DesktopAppInfo,
  DesktopSavePackZIPResult,
  DesktopScanInstalledResult,
  WailsDesktopBridge,
} from "./wails";

export interface BackendEventMessage {
  name: string;
  timestamp: number;
  payload?: unknown;
}

export function getWailsBridge(): WailsDesktopBridge | null {
  if (typeof window === "undefined") return null;
  return window.go?.desktop?.Bridge ?? null;
}

export function isWailsDesktop(): boolean {
  return getWailsBridge() !== null;
}

export function subscribeBackendEvents(handler: (ev: BackendEventMessage) => void): () => void {
  if (typeof window === "undefined" || !window.runtime?.EventsOn) {
    return () => {};
  }
  const off = window.runtime.EventsOn("modhub:event", (raw: unknown) => {
    if (raw && typeof raw === "object" && "name" in raw) {
      handler(raw as BackendEventMessage);
    }
  });
  return () => {
    if (typeof off === "function") off();
    else window.runtime?.EventsOff("modhub:event");
  };
}

export const desktopBridge = {
  isDesktop: isWailsDesktop,

  async getAppInfo(): Promise<DesktopAppInfo | null> {
    const b = getWailsBridge();
    if (!b) return null;
    return b.GetAppInfo();
  },

  async openDataDir(): Promise<void> {
    const b = getWailsBridge();
    if (!b) {
      throw new Error("Открытие папки доступно только в desktop-версии ModHub");
    }
    await b.OpenDataDir();
  },

  async openExternalUrl(rawUrl: string): Promise<void> {
    const b = getWailsBridge();
    if (b) {
      await b.OpenExternalURL(rawUrl);
      return;
    }
    window.open(rawUrl, "_blank", "noopener,noreferrer");
  },

  async scanInstalledFolder(mcVersion: string, loader: string): Promise<DesktopScanInstalledResult | null> {
    const b = getWailsBridge();
    if (!b) return null;
    return b.ScanInstalledFolder(mcVersion, loader);
  },

  async savePackZipNative(packName: string, items: CartItem[]): Promise<DesktopSavePackZIPResult | null> {
    const b = getWailsBridge();
    if (!b) return null;
    return b.SavePackZIP(packName, items);
  },

  async browseCustomJavaPath(): Promise<JavaRuntimeInfo | null> {
    const b = getWailsBridge();
    if (!b) return null;
    const rt = await b.BrowseCustomJavaPath();
    if (!rt || !rt.executable_path) return null;
    return rt;
  },

  async openJavaRuntimeDir(id = ""): Promise<void> {
    const b = getWailsBridge();
    if (!b) {
      throw new Error("Открытие папки доступно только в desktop-версии ModHub");
    }
    await b.OpenJavaRuntimeDir(id);
  },

  async openInstanceDir(id: string, subdir = ""): Promise<void> {
    const b = getWailsBridge();
    if (!b) {
      throw new Error("Открытие папки профиля доступно только в desktop-версии ModHub");
    }
    await b.OpenInstanceDir(id, subdir);
  },

  async browseInstanceFolder(): Promise<string> {
    const b = getWailsBridge();
    if (!b) return "";
    return (await b.BrowseInstanceFolder()) || "";
  },

  async openInstanceLogFile(instanceId: string): Promise<void> {
    const b = getWailsBridge();
    if (!b) {
      throw new Error("Открытие лог-файла доступно только в desktop-версии ModHub");
    }
    await b.OpenInstanceLogFile(instanceId);
  },

  async openCrashReport(instanceId: string, filename: string): Promise<void> {
    const b = getWailsBridge();
    if (!b) {
      throw new Error("Открытие crash report доступно только в desktop-версии ModHub");
    }
    await b.OpenCrashReport(instanceId, filename);
  },

  async browseModpackArchive(): Promise<string> {
    const b = getWailsBridge();
    if (!b) return "";
    return (await b.BrowseModpackArchive()) || "";
  },

  subscribeFileDrop(onDrop: (paths: string[]) => void): () => void {
    if (typeof window === "undefined" || !window.runtime?.OnFileDrop) {
      return () => {};
    }
    window.runtime.OnFileDrop((_x, _y, paths) => {
      if (Array.isArray(paths) && paths.length > 0) onDrop(paths);
    }, false);
    return () => {
      window.runtime?.OnFileDropOff?.();
    };
  },

  async minimizeWindow(): Promise<void> {
    const b = getWailsBridge();
    if (b) await b.MinimizeWindow();
  },

  async hideWindow(): Promise<void> {
    const b = getWailsBridge();
    if (b) await b.HideWindow();
  },

  async showWindow(): Promise<void> {
    const b = getWailsBridge();
    if (b) await b.ShowWindow();
  },

  async toggleMaximizeWindow(): Promise<void> {
    const b = getWailsBridge();
    if (b) await b.ToggleMaximizeWindow();
  },

  async closeWindow(): Promise<void> {
    const b = getWailsBridge();
    if (b) await b.CloseWindow();
    else window.close();
  },

  async trimMemory(): Promise<void> {
    const b = getWailsBridge();
    if (b) await b.TrimMemory();
  },

  async enterPhantomMode(): Promise<void> {
    const b = getWailsBridge();
    if (b) await b.EnterPhantomMode();
  },
};
