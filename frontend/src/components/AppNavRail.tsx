import React from "react";
import type { AccountView, DownloadQueueSummary, GameInstance, ThemeMode } from "../types";
import { SkinHead } from "./SkinHead";

export type AppSection = "home" | "instances" | "catalog" | "downloads" | "accounts" | "settings";

interface AppNavRailProps {
  activeSection: AppSection;
  onSelectSection: (section: AppSection) => void;
  instancesCount: number;
  modInstallTarget?: GameInstance | null;
  runningInstanceIds?: Set<string>;
  downloadSummary: DownloadQueueSummary;
  accounts: AccountView[];
  activeAccount: AccountView | null;
  onSelectAccount?: (accountId: string) => void;
  cfStatus: "ok" | "warn" | "err";
  isDesktop?: boolean;
  theme: ThemeMode;
  onToggleTheme: (e: React.MouseEvent<HTMLButtonElement>) => void;
  onOpenAbout: () => void;
}

function getAccountName(acc: AccountView | null): string {
  if (!acc) return "Аккаунты";
  return acc.minecraft_username || acc.xbox_gamertag || acc.profile_name || "Аккаунт";
}

function getAccountSubtitle(acc: AccountView | null, count: number): string {
  if (!acc) {
    return count > 0 ? "Выбрать аккаунт" : "Войти или создать";
  }
  if (acc.account_type === "offline") {
    return "Офлайн-профиль";
  }
  if (acc.auth_status === "reauth_required") {
    return "Требуется вход";
  }
  if (acc.license_status === "owned") {
    return "Лицензия Microsoft";
  }
  return "Microsoft аккаунт";
}

function getAccountDot(acc: AccountView | null): "ok" | "warn" | "err" | undefined {
  if (!acc) return undefined;
  if (acc.auth_status === "reauth_required") return "err";
  if (acc.account_type === "offline" || (acc.auth_status === "authorized" && acc.license_status === "owned")) {
    return "ok";
  }
  return "warn";
}

export const AppNavRail: React.FC<AppNavRailProps> = React.memo(({
  activeSection,
  onSelectSection,
  instancesCount,
  modInstallTarget = null,
  runningInstanceIds,
  downloadSummary,
  accounts,
  activeAccount,
  onSelectAccount,
  cfStatus,
  theme,
  onToggleTheme,
  onOpenAbout,
}) => {
  const activeName = getAccountName(activeAccount);
  const activeDot = getAccountDot(activeAccount);
  const runningCount = runningInstanceIds?.size || 0;

  const navItems: Array<{
    id: AppSection;
    label: string;
    badge?: string;
    dot?: "ok" | "warn" | "err";
    icon: React.ReactNode;
  }> = [
    {
      id: "home",
      label: "Главная",
      icon: (
        <svg className="mh-nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M3 10.5 12 3l9 7.5" />
          <path d="M5 9.5V20a1 1 0 0 0 1 1h4.5v-6h3v6H18a1 1 0 0 0 1-1V9.5" />
        </svg>
      ),
    },
    {
      id: "instances",
      label: "Профили",
      badge: instancesCount > 0 ? String(instancesCount) : undefined,
      dot: runningCount > 0 ? "ok" : modInstallTarget ? "warn" : undefined,
      icon: (
        <svg className="mh-nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <polygon points="12 2 2 7 12 12 22 7 12 2" />
          <polyline points="2 12 12 17 22 12" />
          <polyline points="2 17 12 22 22 17" />
        </svg>
      ),
    },
    {
      id: "catalog",
      label: "Каталог",
      icon: (
        <svg className="mh-nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <rect x="3" y="3" width="7" height="7" rx="1.5" />
          <rect x="14" y="3" width="7" height="7" rx="1.5" />
          <rect x="3" y="14" width="7" height="7" rx="1.5" />
          <rect x="14" y="14" width="7" height="7" rx="1.5" />
        </svg>
      ),
    },
    {
      id: "downloads",
      label: "Загрузки",
      badge:
        downloadSummary.active_count + downloadSummary.queued_count > 0
          ? String(downloadSummary.active_count + downloadSummary.queued_count)
          : undefined,
      dot:
        downloadSummary.error_count > 0
          ? "err"
          : downloadSummary.active_count > 0
            ? "ok"
            : undefined,
      icon: (
        <svg className="mh-nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4" />
          <polyline points="7 10 12 15 17 10" />
          <line x1="12" y1="15" x2="12" y2="3" />
        </svg>
      ),
    },
    {
      id: "settings",
      label: "Настройки",
      dot: cfStatus,
      icon: (
        <svg className="mh-nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <line x1="4" y1="6" x2="20" y2="6" />
          <line x1="4" y1="12" x2="20" y2="12" />
          <line x1="4" y1="18" x2="20" y2="18" />
          <circle cx="9" cy="6" r="2" />
          <circle cx="15" cy="12" r="2" />
          <circle cx="11" cy="18" r="2" />
        </svg>
      ),
    },
  ];

  return (
    <aside className="mh-nav-rail" aria-label="Навигация приложения">
      {/* Accounts Hero Widget above "Главная" + Quick Account Switcher */}
      <div className="mh-nav-accounts-box">
        <button
          type="button"
          className={`mh-nav-accounts-hero${activeSection === "accounts" ? " is-active" : ""}`}
          aria-current={activeSection === "accounts" ? "page" : undefined}
          onClick={() => onSelectSection("accounts")}
          title={activeAccount ? `Аккаунт: ${activeName} — открыть управление аккаунтами` : "Открыть управление аккаунтами"}
        >
          <span className="mh-nav-accounts-avatar" aria-hidden="true">
            {activeAccount ? (
              <SkinHead skinUrl={activeAccount.skin_url} username={activeName} size={30} />
            ) : (
              <span className="mh-nav-accounts-ico">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                  <circle cx="12" cy="8" r="4" />
                  <path d="M4 20a8 8 0 0 1 16 0" />
                </svg>
              </span>
            )}
            {activeDot && <span className={`mh-nav-accounts-status-dot status-dot ${activeDot}`} />}
          </span>
          <div className="mh-nav-accounts-copy">
            <span className="mh-nav-accounts-title">{activeName}</span>
            <span className="mh-nav-accounts-sub">{getAccountSubtitle(activeAccount, accounts.length)}</span>
          </div>
          {accounts.length > 0 && (
            <span className="mh-nav-accounts-count mono-num">{accounts.length}</span>
          )}
        </button>

        {accounts.length > 1 && (
          <div className="mh-nav-accounts-list" role="list" aria-label="Быстрое переключение аккаунта">
            {accounts.map((acc) => {
              const name = getAccountName(acc);
              const isOffline = acc.account_type === "offline";
              return (
                <button
                  key={acc.id}
                  type="button"
                  role="listitem"
                  className={`mh-nav-account-pill${acc.active ? " is-selected" : ""}`}
                  title={`${name} (${isOffline ? "Офлайн" : "Microsoft"})${acc.active ? " — активный" : " — нажми для выбора"}`}
                  onClick={() => {
                    if (acc.active) {
                      onSelectSection("accounts");
                    } else if (onSelectAccount) {
                      onSelectAccount(acc.id);
                    } else {
                      onSelectSection("accounts");
                    }
                  }}
                >
                  <SkinHead skinUrl={acc.skin_url} username={name} size={18} />
                  <span className="mh-nav-account-name">{name}</span>
                  <span className="mh-nav-account-kind mono-num">{isOffline ? "OFF" : "MS"}</span>
                  {acc.active && <span className="mh-nav-account-active-dot" aria-hidden="true" />}
                </button>
              );
            })}
          </div>
        )}
      </div>

      <nav className="mh-nav-list" aria-label="Основные разделы">
        {navItems.map((item) => {
          const isActive = activeSection === item.id;
          return (
            <button
              key={item.id}
              type="button"
              className={`mh-nav-item${isActive ? " is-active" : ""}`}
              aria-current={isActive ? "page" : undefined}
              title={item.label}
              onClick={() => onSelectSection(item.id)}
            >
              <span className="mh-nav-ico-wrap">
                {item.icon}
                {item.dot && <span className={`mh-nav-dot status-dot ${item.dot}`} aria-hidden="true" />}
              </span>
              <span className="mh-nav-label">{item.label}</span>
              {item.badge && <span className="mh-nav-badge mono-num">{item.badge}</span>}
            </button>
          );
        })}
      </nav>

      {(downloadSummary.active_count > 0 || downloadSummary.queued_count > 0) && (
        <button
          type="button"
          className="mh-nav-dl-widget"
          title="Открыть страницу активных загрузок"
          onClick={() => onSelectSection("downloads")}
        >
          <div className="mh-nav-dl-top">
            <span className="mh-nav-dl-title">
              Загрузки ({downloadSummary.active_count + downloadSummary.queued_count})
            </span>
            <span className="mono-num mh-nav-dl-pct">
              {Math.round(downloadSummary.overall_progress)}%
            </span>
          </div>
          <div className="mh-nav-dl-bar" aria-hidden="true">
            <div
              className="mh-nav-dl-fill"
              style={{ width: `${Math.max(4, Math.min(100, downloadSummary.overall_progress))}%` }}
            />
          </div>
          {downloadSummary.total_speed_bytes_per_sec > 0 && (
            <div className="mh-nav-dl-speed mono-num">
              {downloadSummary.total_speed_bytes_per_sec >= 1_048_576
                ? `${(downloadSummary.total_speed_bytes_per_sec / 1_048_576).toFixed(1)} МБ/с`
                : `${Math.round(downloadSummary.total_speed_bytes_per_sec / 1_024)} КБ/с`}
            </div>
          )}
        </button>
      )}

      <div className="mh-nav-footer">
        <button
          type="button"
          className="mh-nav-foot-btn"
          onClick={onToggleTheme}
          title={theme === "light" ? "Переключить на тёмную тему" : "Переключить на светлую тему"}
          aria-label="Переключить тему"
        >
          <span className="theme-icon" aria-hidden="true">
            <svg className="theme-morph-svg" viewBox="0 0 24 24" fill="none">
              <defs>
                <mask id="railThemeMoonMask">
                  <rect width="24" height="24" fill="#fff" />
                  <circle className="tm-mask-cut" cx="16.5" cy="7.5" r="6.8" fill="#000" />
                </mask>
              </defs>
              <circle className="tm-core" cx="12" cy="12" r="7.4" fill="currentColor" mask="url(#railThemeMoonMask)" />
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
          <span className="mh-nav-label">{theme === "light" ? "Светлая" : "Тёмная"}</span>
        </button>

        <button
          type="button"
          className="mh-nav-foot-btn"
          onClick={onOpenAbout}
          title="О проекте ModHub"
          aria-label="О проекте ModHub"
        >
          <svg className="mh-nav-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
            <circle cx="12" cy="12" r="10" />
            <line x1="12" y1="16" x2="12" y2="12" />
            <line x1="12" y1="8" x2="12.01" y2="8" />
          </svg>
          <span className="mh-nav-label">О проекте</span>
        </button>
      </div>
    </aside>
  );
});
