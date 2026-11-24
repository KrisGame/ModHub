import React, { memo, useCallback, useState } from "react";
import { desktopBridge } from "../services/bridge";
import type { GameSessionInfo } from "../types";
import { isSessionActive } from "../hooks/useLauncher";

export interface TitleBarProps {
  appVersion?: string;
  activeSession?: GameSessionInfo | null;
  activeInstanceName?: string;
  onTriggerPhantom?: () => void;
}

export const TitleBar: React.FC<TitleBarProps> = memo(({
  appVersion = "0.9.0",
  activeSession,
  activeInstanceName,
  onTriggerPhantom,
}) => {
  const isDesktop = desktopBridge.isDesktop();
  const isGameRunning = Boolean(activeSession && isSessionActive(activeSession.state));
  const [phantomBusy, setPhantomBusy] = useState(false);

  const handleMinimize = useCallback(() => {
    desktopBridge.minimizeWindow().catch((err) => {
      console.warn("Failed to minimize window:", err);
    });
  }, []);

  const handleToggleMaximize = useCallback(() => {
    desktopBridge.toggleMaximizeWindow().catch((err) => {
      console.warn("Failed to toggle maximize window:", err);
    });
  }, []);

  const handleClose = useCallback(() => {
    desktopBridge.closeWindow().catch((err) => {
      console.warn("Failed to close window:", err);
    });
  }, []);

  const handlePhantom = useCallback(async () => {
    if (phantomBusy) return;
    setPhantomBusy(true);
    try {
      if (onTriggerPhantom) {
        onTriggerPhantom();
      } else {
        await desktopBridge.enterPhantomMode();
      }
    } catch (err) {
      console.warn("Failed to enter phantom mode:", err);
    } finally {
      setTimeout(() => setPhantomBusy(false), 800);
    }
  }, [phantomBusy, onTriggerPhantom]);

  return (
    <header className="mh-titlebar" style={{ "--wails-draggable": "drag" } as React.CSSProperties}>
      <div className="mh-titlebar-left" style={{ "--wails-draggable": "no-drag" } as React.CSSProperties}>
        <div className="mh-titlebar-brand" title="ModHub Desktop Launcher">
          <img
            src="/assets/favicon.svg"
            alt="ModHub"
            className="mh-titlebar-logo"
            width={18}
            height={18}
            draggable={false}
          />
          <span className="mh-titlebar-title">ModHub</span>
          <span className="mh-titlebar-version-tag">v{appVersion}</span>
        </div>

        {isGameRunning && (
          <div className="mh-titlebar-game-indicator" title="Игра запущена">
            <span className="mh-titlebar-pulse-dot" />
            <span className="mh-titlebar-game-label">
              {activeInstanceName ? activeInstanceName : "Minecraft"}
            </span>
            <button
              type="button"
              className="mh-titlebar-phantom-btn"
              onClick={handlePhantom}
              disabled={phantomBusy}
              title="Свернуть лаунчер в трей на время игры"
            >
              ⚡ {phantomBusy ? "…" : "В трей"}
            </button>
          </div>
        )}
      </div>

      <div className="mh-titlebar-center" />

      <div className="mh-titlebar-right" style={{ "--wails-draggable": "no-drag" } as React.CSSProperties}>
        {isDesktop && (
          <div className="mh-titlebar-controls">
            <button
              type="button"
              className="mh-titlebar-btn mh-titlebar-btn-min"
              onClick={handleMinimize}
              aria-label="Свернуть окно"
              title="Свернуть"
            >
              <svg width="10" height="1" viewBox="0 0 10 1" fill="currentColor">
                <rect width="10" height="1" />
              </svg>
            </button>
            <button
              type="button"
              className="mh-titlebar-btn mh-titlebar-btn-max"
              onClick={handleToggleMaximize}
              aria-label="Развернуть окно"
              title="Развернуть / Восстановить"
            >
              <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1">
                <rect x="0.5" y="0.5" width="9" height="9" />
              </svg>
            </button>
            <button
              type="button"
              className="mh-titlebar-btn mh-titlebar-btn-close"
              onClick={handleClose}
              aria-label="Закрыть приложение"
              title="Закрыть"
            >
              <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.2">
                <path d="M0.5 0.5L9.5 9.5M9.5 0.5L0.5 9.5" />
              </svg>
            </button>
          </div>
        )}
      </div>
    </header>
  );
});

TitleBar.displayName = "TitleBar";
