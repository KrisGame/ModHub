import React from "react";

// Official Modrinth-style silhouette/stroke loader icons
export const LoaderIcon: React.FC<{ loader: string; size?: number }> = ({ loader, size = 13 }) => {
  const low = String(loader || "").toLowerCase();
  if (low === "neoforge") {
    // Modrinth NeoForge geometric fox silhouette
    return (
      <svg className="ui-ico-inline" width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M4.2 3.2c-.6-.3-1.2.1-1.2.8v8.4c0 2.7 1.4 5.2 3.7 6.6l4.6 2.8c.4.2 1 .2 1.4 0l4.6-2.8c2.3-1.4 3.7-3.9 3.7-6.6V4c0-.7-.6-1.1-1.2-.8l-4.5 2.5a1 1 0 0 1-1 0L12 4.3 9.7 5.7a1 1 0 0 1-1 0L4.2 3.2Zm3.1 8.1 2.4 1.3-1.1 1.6-1.3-2.9Zm9.4 0-1.3 2.9-1.1-1.6 2.4-1.3ZM12 15.8l2 1.4-2 1.4-2-1.4 2-1.4Z" />
      </svg>
    );
  }
  if (low === "fabric") {
    // Modrinth Fabric rolled parchment scroll
    return (
      <svg className="ui-ico-inline" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M18.5 3.5a2.5 2.5 0 0 1 0 5H9a2.5 2.5 0 0 0 0 5h6.5a2.5 2.5 0 0 1 0 5H6.5A2.5 2.5 0 0 1 4 16V6a2.5 2.5 0 0 1 2.5-2.5h12Z" />
        <path d="M8.5 8.5v10" />
      </svg>
    );
  }
  if (low === "forge") {
    // Modrinth Forge classic anvil silhouette
    return (
      <svg className="ui-ico-inline" width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <path d="M2 6.5h20v2.6c0 1.8-1.4 3.4-3.3 3.7l-2.3.4v2.6l2.4 2.4c.4.4.1 1.3-.5 1.3H5.7c-.6 0-.9-.9-.5-1.3l2.4-2.4v-2.6l-1.9-.3C3.5 12.5 2 10.6 2 8.4V6.5Z" />
      </svg>
    );
  }
  if (low === "quilt") {
    // Modrinth Quilt 4-patch interlocking emblem
    return (
      <svg className="ui-ico-inline" width={size} height={size} viewBox="0 0 24 24" fill="currentColor" aria-hidden="true">
        <rect x="3" y="3" width="8" height="8" rx="2" />
        <rect x="13" y="3" width="8" height="8" rx="2" opacity="0.68" />
        <rect x="3" y="13" width="8" height="8" rx="2" opacity="0.68" />
        <rect x="13" y="13" width="8" height="8" rx="2" />
      </svg>
    );
  }
  if (low === "datapack") {
    // Modrinth Datapack isometric package/cube
    return (
      <svg className="ui-ico-inline" width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M12 2.6 20.5 7.4v9.2L12 21.4l-8.5-4.8V7.4L12 2.6Z" />
        <path d="M12 21.4V12" />
        <path d="M20.5 7.4 12 12 3.5 7.4" />
        <path d="M16.2 5 7.8 9.7" />
      </svg>
    );
  }
  return null;
};

// Modrinth (Lucide) crisp category icons
const CAT_PATHS: Record<string, string> = {
  optimization: '<polygon points="13 2 3 14 12 14 11 22 21 10 12 10 13 2"/>',
  technology: '<rect x="4" y="4" width="16" height="16" rx="2"/><rect x="9" y="9" width="6" height="6"/><path d="M9 1v3M15 1v3M9 20v3M15 20v3M20 9h3M20 14h3M1 9h3M1 14h3"/>',
  magic: '<path d="m21.64 3.64-1.28-1.28a1.21 1.21 0 0 0-1.72 0L2.36 18.64a1.21 1.21 0 0 0 0 1.72l1.28 1.28a1.2 1.2 0 0 0 1.72 0L21.64 5.36a1.2 1.2 0 0 0 0-1.72Z"/><path d="m14 7 3 3"/><path d="M5 6v4"/><path d="M19 14v4"/><path d="M10 2v2"/><path d="M7 8H3"/><path d="M21 16h-4"/>',
  adventure: '<circle cx="12" cy="12" r="10"/><polygon points="16.24 7.76 14.12 14.12 7.76 16.24 9.88 9.88 16.24 7.76"/>',
  worldgen: '<path d="m8 3 4 8 5-5 5 15H2L8 3z"/>',
  equipment: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  mobs: '<circle cx="9" cy="10" r="1.4"/><circle cx="15" cy="10" r="1.4"/><path d="M5 11a7 7 0 0 1 14 0v5a3 3 0 0 1-3 3H8a3 3 0 0 1-3-3v-5z"/><path d="M9 15h6"/>',
  storage: '<path d="M21 8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16Z"/><path d="M3.3 7 12 12l8.7-5"/><path d="M12 22V12"/>',
  decoration: '<path d="M3 9.5 12 3l9 6.5V20a1 1 0 0 1-1 1H4a1 1 0 0 1-1-1V9.5Z"/><path d="M9 21V12h6v9"/>',
  food: '<path d="M12 20.94c1.5 0 2.75 1.06 4 1.06 3 0 6-8 6-12.22A4.91 4.91 0 0 0 17 5c-2.22 0-3.2 1.44-5 1.44S9.22 5 7 5a4.91 4.91 0 0 0-5 4.78C2 14 5 22 8 22c1.25 0 2.5-1.06 4-1.06Z"/><path d="M10 2c1 .5 2 2 2 5"/>',
  utility: '<path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>',
  library: '<path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/><path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>',
  transportation: '<rect x="4" y="4" width="16" height="12" rx="2"/><path d="M4 11h16"/><path d="M8 20l2-4"/><path d="M16 20l-2-4"/><circle cx="8.5" cy="14" r="1"/><circle cx="15.5" cy="14" r="1"/>',
  "game-mechanics": '<line x1="4" y1="21" x2="4" y2="14"/><line x1="4" y1="10" x2="4" y2="3"/><line x1="12" y1="21" x2="12" y2="12"/><line x1="12" y1="8" x2="12" y2="3"/><line x1="20" y1="21" x2="20" y2="16"/><line x1="20" y1="12" x2="20" y2="3"/><line x1="1" y1="14" x2="7" y2="14"/><line x1="9" y1="8" x2="15" y2="8"/><line x1="17" y1="16" x2="23" y2="16"/>',
  social: '<path d="M16 21v-2a4 4 0 0 0-4-4H6a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M22 21v-2a4 4 0 0 0-3-3.87"/><path d="M16 3.13a4 4 0 0 1 0 7.75"/>',
  "vanilla-like": '<path d="M12 3l1.9 5.6L19.5 10.5l-5.6 1.9L12 18l-1.9-5.6-5.6-1.9 5.6-1.9L12 3z"/>',
  realistic: '<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7Z"/><circle cx="12" cy="12" r="3"/>',
  "semi-realistic": '<path d="M2 12s3.6-7 10-7 10 7 10 7-3.6 7-10 7-10-7-10-7Z"/><circle cx="12" cy="12" r="3"/>',
  simplistic: '<circle cx="8" cy="8" r="4"/><rect x="12" y="12" width="8" height="8" rx="1.5"/>',
  themed: '<path d="M3 18h18v2H3zM5 18V8l3 2 4-5 4 5 3-2v10"/>',
  tweaks: '<path d="M12 20h9"/><path d="M16.5 3.5a2.121 2.121 0 1 1 3 3L7 19l-4 1 1-4L16.5 3.5z"/>',
  cursed: '<circle cx="12" cy="12" r="10"/><path d="M8 15h8"/><circle cx="9" cy="9" r="1"/><circle cx="15" cy="9" r="1"/>',
  gui: '<rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/>',
  combat: '<polyline points="14.5 17.5 3 6 3 3 6 3 17.5 14.5"/><line x1="13" y1="19" x2="19" y2="13"/><line x1="16" y1="16" x2="20" y2="20"/>',
  audio: '<path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/>',
  models: '<path d="M12 3l9 4.5v9L12 21l-9-4.5v-9L12 3z"/><path d="M12 12l9-4.5"/><path d="M12 12v9"/><path d="M12 12L3 7.5"/>',
  fantasy: '<path d="M12 3l1.9 5.6L19.5 10.5l-5.6 1.9L12 18l-1.9-5.6-5.6-1.9 5.6-1.9L12 3z"/><path d="M5 3v3M3.5 4.5h3"/>',
  cartoon: '<circle cx="12" cy="12" r="10"/><path d="M8 14s1.5 2 4 2 4-2 4-2"/><line x1="9" y1="9" x2="9.01" y2="9"/><line x1="15" y1="9" x2="15.01" y2="9"/>',
  atmosphere: '<path d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9Z"/>',
  bloom: '<circle cx="12" cy="12" r="4"/><path d="M12 2v3M12 19v3M4.93 4.93l2.12 2.12M16.95 16.95l2.12 2.12M2 12h3M19 12h3M4.93 19.07l2.12-2.12M16.95 7.05l2.12-2.12"/>',
  potato: '<path d="M12 14l4-4"/><path d="M3.34 19a10 10 0 1 1 17.32 0"/>',
  "path-tracing": '<circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M6.34 17.66l-1.41 1.41M19.07 4.93l-1.41 1.41"/>',
};

export const CategoryIcon: React.FC<{ id: string; size?: number }> = ({ id, size = 12.5 }) => {
  const paths = CAT_PATHS[String(id || "").toLowerCase()];
  if (!paths) return null;
  return (
    <svg
      className="ui-ico-inline"
      width={size}
      height={size}
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
      aria-hidden="true"
      dangerouslySetInnerHTML={{ __html: paths }}
    />
  );
};

export const BanSmIcon: React.FC = () => (
  <svg width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
    <circle cx="12" cy="12" r="9" />
    <line x1="5.6" y1="5.6" x2="18.4" y2="18.4" />
  </svg>
);

export const PlatformPill: React.FC<{
  platform: string;
  href?: string;
  showArrow?: boolean;
  size?: number;
}> = ({ platform, href, showArrow = false, size = 13 }) => {
  const isMr = platform === "modrinth";
  const label = isMr ? "Modrinth" : "CurseForge";
  const cls = `pill pill-platform ${isMr ? "pill-mr" : "pill-cf"}`;
  const imgCls = `platform-icon-img ${isMr ? "platform-icon-mr" : "platform-icon-cf"}`;
  const inner = (
    <>
      <img className={imgCls} src={`/assets/${platform}.svg`} width={size} height={size} alt={label} />
      <span>{label}{showArrow ? " ↗" : ""}</span>
    </>
  );
  if (href) {
    return (
      <a className={cls} href={href} target="_blank" rel="noopener noreferrer" onClick={(e) => e.stopPropagation()}>
        {inner}
      </a>
    );
  }
  return <span className={cls}>{inner}</span>;
};

// Modrinth-style Environment icons (Monitor = Client, HardDrive/Server = Server, Globe = Both)
export const EnvBadgeIcon: React.FC<{ env: "client" | "server" | "both" | "" }> = ({ env }) => {
  if (!env) return null;
  if (env === "client") {
    return (
      <span className="meta-ico-badge env-client" title="Только клиент">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <rect x="2" y="3" width="20" height="14" rx="2" />
          <line x1="8" y1="21" x2="16" y2="21" />
          <line x1="12" y1="17" x2="12" y2="21" />
        </svg>
      </span>
    );
  }
  if (env === "server") {
    return (
      <span className="meta-ico-badge env-server" title="Только сервер">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <rect x="2" y="3" width="20" height="7" rx="2" />
          <rect x="2" y="14" width="20" height="7" rx="2" />
          <line x1="6" y1="6.5" x2="6.01" y2="6.5" />
          <line x1="6" y1="17.5" x2="6.01" y2="17.5" />
        </svg>
      </span>
    );
  }
  return (
    <span className="meta-ico-badge env-both" title="Клиент и сервер">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <line x1="3" y1="12" x2="21" y2="12" />
        <path d="M12 3a13.5 13.5 0 0 1 3.6 9 13.5 13.5 0 0 1-3.6 9 13.5 13.5 0 0 1-3.6-9 13.5 13.5 0 0 1 3.6-9z" />
      </svg>
    </span>
  );
};


export const CloseIcon: React.FC<{ size?: number }> = ({ size = 15 }) => (
  <svg
    width={size}
    height={size}
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    strokeWidth="2.2"
    strokeLinecap="round"
    strokeLinejoin="round"
    aria-hidden="true"
  >
    <line x1="18" y1="6" x2="6" y2="18" />
    <line x1="6" y1="6" x2="18" y2="18" />
  </svg>
);

export const EnvBadgePill: React.FC<{ env: "client" | "server" | "both" | "" }> = ({ env }) => {
  if (!env) return null;
  if (env === "client") {
    return (
      <span className="tbl-chip-tag tbl-chip-env env-client" title="Только клиент">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <rect x="2" y="3" width="20" height="14" rx="2" />
          <line x1="8" y1="21" x2="16" y2="21" />
          <line x1="12" y1="17" x2="12" y2="21" />
        </svg>
        <span>Клиент</span>
      </span>
    );
  }
  if (env === "server") {
    return (
      <span className="tbl-chip-tag tbl-chip-env env-server" title="Только сервер">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
          <rect x="2" y="3" width="20" height="7" rx="2" />
          <rect x="2" y="14" width="20" height="7" rx="2" />
          <line x1="6" y1="6.5" x2="6.01" y2="6.5" />
          <line x1="6" y1="17.5" x2="6.01" y2="17.5" />
        </svg>
        <span>Сервер</span>
      </span>
    );
  }
  return (
    <span className="tbl-chip-tag tbl-chip-env env-both" title="Клиент и сервер">
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.1" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="9" />
        <line x1="3" y1="12" x2="21" y2="12" />
        <path d="M12 3a13.5 13.5 0 0 1 3.6 9 13.5 13.5 0 0 1-3.6 9 13.5 13.5 0 0 1-3.6-9 13.5 13.5 0 0 1 3.6-9z" />
      </svg>
      <span>Клиент и сервер</span>
    </span>
  );
};
