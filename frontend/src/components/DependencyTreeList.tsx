import React from "react";
import { PlatformPill } from "../icons";
import type { DepResolutionStatus, DepTreeNode, UnresolvedDep } from "../types";
import { cachedIconUrl, fallbackIconDataUri } from "../utils";

interface DependencyTreeListProps {
  tree: DepTreeNode[];
  unresolved?: UnresolvedDep[];
  warnings?: string[];
  errors?: Record<string, string>;
  status?: DepResolutionStatus;
  loading?: boolean;
  fatalError?: string;
  compact?: boolean;
  grouped?: boolean;
  emptyTitle?: string;
  emptyHint?: string;
  onRetry?: () => void;
}

function depTypeLabel(t: string, short = false): string {
  switch (t) {
    case "required":
      return short ? "обяз." : "Обязательная";
    case "recommended":
      return short ? "реком." : "Рекомендуемая";
    case "integration":
      return short ? "интегр." : "Интеграция";
    case "incompatible":
      return short ? "конфликт" : "Несовместима";
    case "embedded":
      return short ? "встр." : "Встроенная";
    case "include":
      return short ? "вкл." : "Включено";
    case "tool":
      return short ? "инстр." : "Инструмент";
    default:
      return short ? "опц." : "Опциональная";
  }
}

function depTypeBadgeClass(t: string): string {
  switch (t) {
    case "required":
      return "dep-badge-req";
    case "recommended":
      return "dep-badge-rec";
    case "integration":
      return "dep-badge-int";
    case "incompatible":
      return "dep-badge-inc";
    case "embedded":
    case "include":
      return "dep-badge-emb";
    default:
      return "dep-badge-opt";
  }
}

function confidenceLabel(c?: string): string {
  switch (c) {
    case "high":
      return "Высокая";
    case "medium":
      return "Средняя";
    case "low":
      return "Низкая";
    default:
      return "";
  }
}

function contentTypeLabel(ct?: string): string {
  switch (ct) {
    case "resourcepack":
      return "Ресурспак";
    case "datapack":
      return "Датапак";
    case "shader":
      return "Шейдер";
    default:
      return "Мод";
  }
}

const SourceBadge: React.FC<{ node: DepTreeNode }> = ({ node }) => {
  if (node.source === "manifest") {
    return (
      <span
        className="pill dep-badge-manifest"
        title={`Обнаружено в официальном манифесте файла (${node.manifest_file || "JAR"})`}
      >
        Манифест
      </span>
    );
  }
  if (node.source === "api+manifest") {
    return (
      <span
        className="pill dep-badge-manifest"
        title={`Подтверждено в API и в манифесте файла (${node.manifest_file || "JAR"})`}
      >
        API + Манифест
      </span>
    );
  }
  if (node.source === "text") {
    const confLbl = confidenceLabel(node.confidence);
    return (
      <>
        <span
          className="pill dep-badge-text"
          title={node.evidence ? `Найдено анализом текста (${node.text_origin || "описание"}): «${node.evidence}»` : "Найдено интеллектуальным анализом описания"}
        >
          Из описания
        </span>
        {confLbl && (
          <span
            className={`pill dep-conf-${node.confidence}`}
            title="Уровень уверенности автоматического определения по тексту"
          >
            {confLbl}
          </span>
        )}
      </>
    );
  }
  return null;
};

const RecursiveChildren: React.FC<{ childrenNodes: DepTreeNode[]; parentKey: string; depth: number }> = ({
  childrenNodes,
  parentKey,
  depth,
}) => {
  if (!childrenNodes.length || depth > 3) return null;
  return (
    <div className="dep-sub-list">
      {childrenNodes.map((ch) => {
        const k = `${parentKey}->${ch.source_key}`;
        return (
          <div key={k} className="dep-node-branch">
            <div className="dep-sub-node">
              <div className="dep-node-main">
                <span className="dep-sub-branch-line" aria-hidden="true">↳</span>
                <img
                  className="dep-node-icon is-sub"
                  src={ch.icon_url ? cachedIconUrl(ch.icon_url, 48) : fallbackIconDataUri(ch.title)}
                  alt=""
                  onError={(e) => {
                    e.currentTarget.src = fallbackIconDataUri(ch.title);
                  }}
                />
                <span className="dep-node-title" style={{ cursor: "default" }}>{ch.title}</span>
                <span className={`pill ${depTypeBadgeClass(ch.type)}`}>{depTypeLabel(ch.type)}</span>
                <SourceBadge node={ch} />
              </div>
              {ch.url && (
                <a className="btn btn-ghost btn-xs" href={ch.url} target="_blank" rel="noopener noreferrer">
                  Открыть ↗
                </a>
              )}
            </div>
            {ch.children && ch.children.length > 0 && (
              <RecursiveChildren childrenNodes={ch.children} parentKey={k} depth={depth + 1} />
            )}
          </div>
        );
      })}
    </div>
  );
};

export const DependencyTreeList: React.FC<DependencyTreeListProps> = ({
  tree,
  unresolved = [],
  warnings = [],
  errors = {},
  status = "none",
  loading = false,
  fatalError = "",
  compact = false,
  grouped = false,
  emptyTitle = "Зависимости не обнаружены",
  emptyHint = "Для выбранной версии не объявлено зависимостей в API, манифесте и описании.",
  onRetry,
}) => {
  const errLines = [
    ...warnings,
    ...Object.values(errors).filter(Boolean),
  ];

  if (loading) {
    return (
      <div className={compact ? "qd-deps-loading" : "empty-state"}>
        <span className="spinner spinner-xs" aria-hidden="true" />
        <span>Анализ зависимостей (API, манифест и описание)…</span>
      </div>
    );
  }

  if (fatalError && tree.length === 0) {
    return (
      <div className={compact ? "qd-deps-warning" : "dep-error-row"} role="status">
        <span>{fatalError}</span>
        {onRetry && (
          <button type="button" className="btn btn-ghost btn-xs" onClick={onRetry}>
            Повторить
          </button>
        )}
      </div>
    );
  }

  const noticeBlock = (errLines.length > 0 || unresolved.length > 0 || status === "partial") && (
    <div className="dep-warn-note" role="status">
      {errLines.map((msg, idx) => (
        <div key={`w-${idx}`}>{msg}</div>
      ))}
      {unresolved.length > 0 && (
        <div>
          Не удалось однозначно сопоставить ({unresolved.length}):{" "}
          {unresolved.map((u) => `${u.mod_id} (${depTypeLabel(u.type, true)})`).join(", ")}.
        </div>
      )}
    </div>
  );

  if (tree.length === 0 && unresolved.length === 0) {
    if (compact) {
      return errLines.length > 0 ? <>{noticeBlock}</> : null;
    }
    return (
      <div className="empty-state">
        {noticeBlock}
        <h3>{emptyTitle}</h3>
        <p>{emptyHint}</p>
        {onRetry && status === "error" && (
          <button type="button" className="btn btn-ghost btn-sm" style={{ marginTop: 8 }} onClick={onRetry}>
            Повторить
          </button>
        )}
      </div>
    );
  }

  if (compact) {
    return (
      <div>
        {noticeBlock}
        <div className="qd-deps-list">
          {tree.slice(0, 12).map((d) => (
            <a
              key={d.source_key}
              href={d.url || "#"}
              target="_blank"
              rel="noopener noreferrer"
              className={`qd-dep-pill dep-type-${d.type}`}
              onClick={(e) => {
                if (!d.url) e.preventDefault();
              }}
              title={`${d.title} (${depTypeLabel(d.type)}${d.source === "manifest" ? " • манифест" : d.source === "text" ? ` • из описания (${confidenceLabel(d.confidence)}): «${d.evidence || ""}»` : ""})`}
            >
              <img
                width={16}
                height={16}
                src={d.icon_url ? cachedIconUrl(d.icon_url, 48) : fallbackIconDataUri(d.title)}
                alt=""
                onError={(e) => {
                  e.currentTarget.src = fallbackIconDataUri(d.title);
                }}
              />
              <span>{d.title}</span>
              <em className="qd-dep-kind">
                {depTypeLabel(d.type, true)}
                {d.source === "text" && d.confidence ? ` · ${d.confidence === "high" ? "выс." : d.confidence === "medium" ? "ср." : "низ."}` : ""}
              </em>
            </a>
          ))}
        </div>
      </div>
    );
  }

  const renderNode = (dn: DepTreeNode) => (
    <div key={dn.source_key} className="dep-node-branch">
      <div className="dep-node-card">
        <div className="dep-node">
          <div className="dep-node-main">
            <img
              className="dep-node-icon"
              src={dn.icon_url ? cachedIconUrl(dn.icon_url, 64) : fallbackIconDataUri(dn.title)}
              alt=""
              onError={(e) => {
                e.currentTarget.src = fallbackIconDataUri(dn.title);
              }}
            />
            <span className="dep-node-title" style={{ cursor: "default" }}>{dn.title}</span>
            {(dn.platform === "modrinth" || dn.platform === "curseforge") && (
              <PlatformPill platform={dn.platform} size={11.5} />
            )}
            <span className="pill pill-cat">{contentTypeLabel(dn.content_type)}</span>
            <span className={`pill ${depTypeBadgeClass(dn.type)}`}>{depTypeLabel(dn.type)}</span>
            <SourceBadge node={dn} />
          </div>
          {dn.url && (
            <a className="btn btn-ghost btn-xs" href={dn.url} target="_blank" rel="noopener noreferrer">
              Открыть ↗
            </a>
          )}
        </div>
        {dn.source === "text" && dn.evidence && (
          <div className="dep-evidence-line" title={dn.evidence}>
            Основание ({dn.text_origin === "title" ? "название" : dn.text_origin === "changelog" ? "changelog" : "описание"}): «{dn.evidence}»
          </div>
        )}
      </div>
      {dn.children && dn.children.length > 0 && (
        <RecursiveChildren childrenNodes={dn.children} parentKey={dn.source_key} depth={1} />
      )}
    </div>
  );

  if (!grouped) {
    return (
      <div>
        {noticeBlock}
        <div className="dep-tree">{tree.map(renderNode)}</div>
      </div>
    );
  }

  const groups: Record<string, DepTreeNode[]> = {
    required: [],
    recommended: [],
    optional: [],
    integration: [],
    embedded: [],
    incompatible: [],
    other: [],
  };
  for (const n of tree) {
    if (n.type === "required") groups.required.push(n);
    else if (n.type === "recommended") groups.recommended.push(n);
    else if (n.type === "optional") groups.optional.push(n);
    else if (n.type === "integration") groups.integration.push(n);
    else if (n.type === "embedded" || n.type === "include") groups.embedded.push(n);
    else if (n.type === "incompatible") groups.incompatible.push(n);
    else groups.other.push(n);
  }

  const specs = [
    { key: "required", title: "Обязательные (основные моды и библиотеки)" },
    { key: "recommended", title: "Рекомендуемые" },
    { key: "optional", title: "Опциональные" },
    { key: "integration", title: "Совместимость и интеграции" },
    { key: "embedded", title: "Встроенные библиотеки" },
    { key: "incompatible", title: "Несовместимые проекты" },
    { key: "other", title: "Прочие связи" },
  ] as const;

  return (
    <div className="own-deps-groups">
      {noticeBlock}
      {specs.map((spec) => {
        const list = groups[spec.key];
        if (!list.length) return null;
        return (
          <div key={spec.key} className="own-dep-group">
            <div className="own-dep-group-title">
              <span>{spec.title}</span>
              <span className="badge-count mono-num">{list.length}</span>
            </div>
            <div className="dep-tree">{list.map(renderNode)}</div>
          </div>
        );
      })}
    </div>
  );
};
