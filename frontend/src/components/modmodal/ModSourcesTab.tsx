import React from "react";
import { LoaderIcon, PlatformPill } from "../../icons";
import type { ProjectCard } from "../../types";
import { filterRealLoaders, formatLoaderName, formatNumber } from "../../utils";

interface ModSourcesTabProps {
  activeItem: ProjectCard;
  license?: string;
  secondaryLinks: Array<{ key: string; label: string; url: string }>;
  onClose: () => void;
  onAuthorClick: (author: string) => void;
}

export const ModSourcesTab: React.FC<ModSourcesTabProps> = ({
  activeItem,
  license,
  secondaryLinks,
  onClose,
  onAuthorClick,
}) => {
  return (
    <div>
      {(activeItem.sources || []).map((s) => {
        const sLoaders = filterRealLoaders(s.loaders || [], activeItem.content_type || "mod");
        return (
          <div key={s.source_key} className="source-box">
            <div style={{ minWidth: 0, flex: 1 }}>
              <div style={{ display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                <PlatformPill platform={s.platform} size={14} />
                <strong style={{ fontSize: 15, color: "var(--text-primary)" }}>{s.title || activeItem.title}</strong>
              </div>
              <div className="modal-subtitle" style={{ marginTop: 6 }}>
                Авторы:{" "}
                {(s.authors?.length ? s.authors : activeItem.authors || []).slice(0, 4).map((a, i) => (
                  <React.Fragment key={a}>
                    {i > 0 && ", "}
                    <button
                      type="button"
                      className="author-link"
                      onClick={() => {
                        onClose();
                        onAuthorClick(a);
                      }}
                    >
                      {a}
                    </button>
                  </React.Fragment>
                ))}
                {" • "}Скачиваний: <span className="mono-num">{formatNumber(s.downloads)}</span>
              </div>
              <div className="source-id-row mono-num">
                <span>ID: {s.id}</span>
                {s.slug && <span>• slug: {s.slug}</span>}
              </div>
              {sLoaders.length > 0 && (
                <div style={{ marginTop: 8, display: "flex", gap: 5, flexWrap: "wrap" }}>
                  {sLoaders.map((l) => (
                    <span key={l} className={`pill pill-loader ${l}`}>
                      <LoaderIcon loader={l} size={11.5} />
                      <span>{formatLoaderName(l)}</span>
                    </span>
                  ))}
                </div>
              )}
            </div>
            <a className="btn btn-primary btn-sm" href={s.url} target="_blank" rel="noopener noreferrer" style={{ flexShrink: 0 }}>
              Открыть оригинал ↗
            </a>
          </div>
        );
      })}

      <div className="sources-security-note">
        <strong>Происхождение и безопасность:</strong> файлы загружаются напрямую с официальных серверов Modrinth или CurseForge без изменений. ModHub не проводит собственное антивирусное сканирование архивов — ориентируйтесь на официальную модерацию платформ и проверенных авторов.
      </div>

      {(secondaryLinks.length > 0 || license) && (
        <div className="sources-secondary-footer">
          {secondaryLinks.length > 0 && (
            <div className="sources-secondary-links">
              {secondaryLinks.map((lnk) => (
                <a
                  key={lnk.key}
                  className="source-mini-link"
                  href={lnk.url}
                  target="_blank"
                  rel="noopener noreferrer"
                >
                  <span>{lnk.label}</span>
                  <span aria-hidden="true">↗</span>
                </a>
              ))}
            </div>
          )}
          {license && (
            <span className="sources-license-note mono-num">
              Лицензия: {license}
            </span>
          )}
        </div>
      )}
    </div>
  );
};
