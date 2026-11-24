import React, { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import { useOverlayTransition } from "../hooks";
import type { ProjectCard } from "../types";

export interface ContextMenuState {
  x: number;
  y: number;
  item: ProjectCard;
}

interface ContextMenuProps {
  menu: ContextMenuState | null;
  onClose: () => void;
  onOpen: (item: ProjectCard) => void;
  onOpenDependents: (item: ProjectCard) => void;
  onAuthorSearch: (author: string) => void;
  onPrimaryAction: (item: ProjectCard) => void;
}

export const ContextMenu: React.FC<ContextMenuProps> = ({
  menu,
  onClose,
  onOpen,
  onOpenDependents,
  onAuthorSearch,
  onPrimaryAction,
}) => {
  const isOpen = Boolean(menu);
  const { mounted, shown } = useOverlayTransition(isOpen, 170);
  const [retained, setRetained] = useState<ContextMenuState | null>(menu);

  useEffect(() => {
    if (menu) setRetained(menu);
  }, [menu]);

  useEffect(() => {
    if (!isOpen) return;
    const close = () => onClose();
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("click", close);
    window.addEventListener("scroll", close, { passive: true });
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("click", close);
      window.removeEventListener("scroll", close);
      window.removeEventListener("keydown", onKey);
    };
  }, [isOpen, onClose]);

  const active = menu || retained;
  if (!mounted || !active) return null;

  const { x, y, item } = active;
  const left = Math.max(8, Math.min(x, window.innerWidth - 256));
  const top = Math.max(8, Math.min(y, window.innerHeight - 290));
  const author = (item.authors || [])[0] || "";
  const mrSrc = (item.sources || []).find((s) => s.platform === "modrinth");
  const cfSrc = (item.sources || []).find((s) => s.platform === "curseforge");

  return createPortal(
    <div
      className={`card-ctx-menu${shown ? " is-open" : ""}`}
      style={{ left: `${left}px`, top: `${top}px` }}
      role="menu"
      aria-hidden={!shown}
      onClick={(e) => e.stopPropagation()}
    >
      <div className="card-ctx-head" title={item.title}>{item.title}</div>
      <button type="button" className="card-ctx-item" onClick={() => { onClose(); onOpen(item); }}>
        <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
        <span>Открыть страницу</span>
      </button>
      <button type="button" className="card-ctx-item" onClick={() => { onClose(); onOpenDependents(item); }}>
        <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="18" cy="18" r="3"/><circle cx="6" cy="6" r="3"/><path d="M6 21V9a9 9 0 0 0 9 9"/></svg>
        <span>Показать зависимые</span>
      </button>
      {author && (
        <button type="button" className="card-ctx-item" onClick={() => { onClose(); onAuthorSearch(author); }}>
          <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/></svg>
          <span>Все моды: {author}</span>
        </button>
      )}
      {item.content_type !== "modpack" && (
        <button type="button" className="card-ctx-item" onClick={() => { onClose(); onPrimaryAction(item); }}>
          <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
          <span>Установить в профиль</span>
        </button>
      )}
      {(mrSrc || cfSrc) && <div className="card-ctx-sep" />}
      {mrSrc && (
        <a className="card-ctx-item" href={mrSrc.url} target="_blank" rel="noopener noreferrer" onClick={onClose}>
          <img className="platform-icon-img platform-icon-mr" src="/assets/modrinth.svg" width={14} height={14} alt="Modrinth" />
          <span>Открыть на Modrinth ↗</span>
        </a>
      )}
      {cfSrc && (
        <a className="card-ctx-item" href={cfSrc.url} target="_blank" rel="noopener noreferrer" onClick={onClose}>
          <img className="platform-icon-img platform-icon-cf" src="/assets/curseforge.svg" width={14} height={14} alt="CurseForge" />
          <span>Открыть на CurseForge ↗</span>
        </a>
      )}
    </div>,
    document.body
  );
};
