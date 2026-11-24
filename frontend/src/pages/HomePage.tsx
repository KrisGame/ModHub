import React from "react";
import type { AppSection } from "../components/AppNavRail";
import type { DesktopAppInfo } from "../services/wails";
import type { AccountView, DownloadQueueSummary, ProjectCard, PublicConfig } from "../types";
import { cachedIconUrl, fallbackIconDataUri, formatNumber } from "../utils";

interface HomePageProps {
  appInfo: DesktopAppInfo | null;
  config: PublicConfig;
  instancesCount: number;
  runningGamesCount: number;
  activeAccount: AccountView | null;
  downloadSummary: DownloadQueueSummary;
  recentProjects: ProjectCard[];
  onNavigate: (section: AppSection) => void;
  onOpenProject: (item: ProjectCard) => void;
  onOpenAbout: () => void;
}

export const HomePage: React.FC<HomePageProps> = ({
  appInfo,
  config,
  instancesCount,
  runningGamesCount,
  activeAccount,
  downloadSummary,
  recentProjects,
  onNavigate,
  onOpenProject,
  onOpenAbout,
}) => {
  const activeDownloads = downloadSummary.active_count + downloadSummary.queued_count;
  const accountReady = Boolean(
    activeAccount &&
      (activeAccount.account_type === "offline" ||
        (activeAccount.auth_status === "authorized" && activeAccount.license_status === "owned"))
  );

  return (
    <div className="mh-page mh-page-enter">
      <header className="mh-page-hero">
        <div className="mh-page-hero-copy">
          <span className="mh-page-eyebrow mono-num">
            {appInfo ? `ModHub v${appInfo.version}` : "ModHub Launcher"}
          </span>
          <h1 className="mh-page-title">Добро пожаловать в ModHub</h1>
          <p className="mh-page-subtitle">
            Единый каталог модов Modrinth и CurseForge, игровые профили Minecraft и быстрый запуск игры.
          </p>
        </div>
        <div className="mh-page-hero-actions">
          <button type="button" className="btn btn-primary btn-sm" onClick={() => onNavigate("instances")}>
            Профили ({instancesCount})
          </button>
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onNavigate("catalog")}>
            Каталог
          </button>
        </div>
      </header>

      <section className="mh-home-stats-grid" aria-label="Сводка состояния">
        <div className="mh-stat-card">
          <span className="mh-stat-label">Источники каталога</span>
          <div className="mh-stat-sources">
            <span className="mh-stat-pill is-ok">
              <img src="/assets/modrinth.svg" width={14} height={14} alt="" />
              <span>Modrinth</span>
            </span>
            <span className={`mh-stat-pill ${config.curseforge_configured ? "is-ok" : "is-warn"}`}>
              <img src="/assets/curseforge.svg" width={14} height={14} alt="" />
              <span>{config.curseforge_configured ? "CurseForge" : "CF не подключён"}</span>
            </span>
          </div>
          {!config.curseforge_configured && (
            <button type="button" className="link-btn mh-stat-link" onClick={() => onNavigate("settings")}>
              Подключить ключ CurseForge →
            </button>
          )}
        </div>

        <div className="mh-stat-card">
          <span className="mh-stat-label">Игровой аккаунт</span>
          <div className="mh-stat-value" style={{ fontSize: 16 }}>
            {activeAccount ? activeAccount.minecraft_username || activeAccount.xbox_gamertag || "Подключён" : "Не выполнен вход"}
          </div>
          <span className="mh-stat-sub">
            {accountReady
              ? activeAccount?.account_type === "offline"
                ? "Офлайн-профиль готов к игре"
                : "Лицензия Minecraft подтверждена"
              : activeAccount
                ? "Требуется проверка или повторный вход"
                : "Войдите или создайте профиль игрока"}
          </span>
          <button type="button" className="link-btn mh-stat-link" onClick={() => onNavigate("accounts")}>
            Управление аккаунтами →
          </button>
        </div>

        <div className="mh-stat-card">
          <span className="mh-stat-label">Профили Minecraft</span>
          <div className="mh-stat-value mono-num">{instancesCount}</div>
          <span className="mh-stat-sub">
            {runningGamesCount > 0 ? `Сейчас запущено игр: ${runningGamesCount}` : "Изолированные сборки с модами"}
          </span>
          <button type="button" className="link-btn mh-stat-link" onClick={() => onNavigate("instances")}>
            Открыть профили →
          </button>
        </div>
      </section>

      <div className="mh-home-columns">
        <section className="mh-panel">
          <div className="mh-panel-head">
            <h2>Недавние проекты</h2>
            <button type="button" className="btn btn-ghost btn-xs" onClick={() => onNavigate("catalog")}>
              Весь каталог →
            </button>
          </div>
          {recentProjects.length === 0 ? (
            <div className="mh-panel-empty">
              Откройте каталог, чтобы найти моды, ресурспаки, шейдеры или датапаки.
            </div>
          ) : (
            <div className="mh-recent-grid">
              {recentProjects.slice(0, 8).map((item) => {
                const fb = fallbackIconDataUri(item.title);
                return (
                  <button
                    key={item.group_id}
                    type="button"
                    className="mh-recent-card"
                    onClick={() => onOpenProject(item)}
                  >
                    <img
                      width={38}
                      height={38}
                      loading="lazy"
                      src={item.icon_url ? cachedIconUrl(item.icon_url, 84) : fb}
                      alt=""
                      onError={(e) => {
                        e.currentTarget.src = fb;
                      }}
                    />
                    <div className="mh-recent-card-meta">
                      <span className="mh-recent-card-title">{item.title}</span>
                      <span className="mh-recent-card-sub mono-num">
                        ↓ {formatNumber(item.downloads)} • {(item.authors || [])[0] || "—"}
                      </span>
                    </div>
                  </button>
                );
              })}
            </div>
          )}
        </section>
      </div>

      <section className="mh-panel mh-roadmap-panel" aria-label="Быстрые переходы">
        <div className="mh-panel-head">
          <h2>Быстрые действия</h2>
          <button type="button" className="btn btn-ghost btn-xs" onClick={onOpenAbout}>
            О проекте
          </button>
        </div>
        <div className="mh-roadmap-chips">
          <button type="button" className="mh-roadmap-chip" onClick={() => onNavigate("instances")}>
            Профили: {runningGamesCount > 0 ? `в игре (${runningGamesCount})` : `${instancesCount} шт.`}
          </button>
          <button type="button" className="mh-roadmap-chip" onClick={() => onNavigate("downloads")}>
            Загрузки: {activeDownloads > 0 ? `активно ${activeDownloads}` : "очередь пуста"}
          </button>
          <button type="button" className="mh-roadmap-chip" onClick={() => onNavigate("settings")}>
            Настройки лаунчера →
          </button>
        </div>
      </section>
    </div>
  );
};
