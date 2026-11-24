import React, { useEffect, useRef, useState } from "react";
import { CloseIcon } from "../icons";
import type { ProjectCard } from "../types";
import { cachedIconUrl, fallbackIconDataUri } from "../utils";

interface RecentBarProps {
  items: ProjectCard[];
  onOpen: (item: ProjectCard) => void;
  onRemoveOne: (groupId: string) => void;
  onClear: () => void;
}

export const RecentBar: React.FC<RecentBarProps> = ({ items, onOpen, onRemoveOne, onClear }) => {
  const listRef = useRef<HTMLDivElement | null>(null);
  const [canLeft, setCanLeft] = useState(false);
  const [canRight, setCanRight] = useState(false);

  const syncArrows = () => {
    const el = listRef.current;
    if (!el) return;
    setCanLeft(el.scrollLeft > 4);
    setCanRight(el.scrollLeft + el.clientWidth < el.scrollWidth - 4);
  };

  useEffect(() => {
    syncArrows();
    const el = listRef.current;
    if (!el) return;

    let vel = 0;
    let raf = 0;

    const stepMomentum = () => {
      if (!listRef.current) {
        raf = 0;
        return;
      }
      const maxX = Math.max(0, listRef.current.scrollWidth - listRef.current.clientWidth);
      if (Math.abs(vel) < 0.35 || (listRef.current.scrollLeft <= 0 && vel < 0) || (listRef.current.scrollLeft >= maxX && vel > 0)) {
        vel = 0;
        raf = 0;
        syncArrows();
        return;
      }
      listRef.current.scrollLeft += vel;
      vel *= 0.89;
      syncArrows();
      raf = requestAnimationFrame(stepMomentum);
    };

    const onNativeWheel = (e: WheelEvent) => {
      if (el.scrollWidth <= el.clientWidth + 2) return;
      const delta = Math.abs(e.deltaY) >= Math.abs(e.deltaX) ? e.deltaY : e.deltaX;
      if (Math.abs(delta) < 0.5) return;
      e.preventDefault();
      e.stopPropagation();
      vel += delta * 0.24;
      if (!raf) raf = requestAnimationFrame(stepMomentum);
    };

    el.addEventListener("wheel", onNativeWheel, { passive: false });
    window.addEventListener("resize", syncArrows, { passive: true });
    return () => {
      if (raf) cancelAnimationFrame(raf);
      el.removeEventListener("wheel", onNativeWheel);
      window.removeEventListener("resize", syncArrows);
    };
  }, [items.length]);

  const scrollStrip = (dir: -1 | 1) => {
    const el = listRef.current;
    if (!el) return;
    el.scrollBy({ left: dir * 240, behavior: "smooth" });
  };

  return (
    <div className={`reveal-slot${items.length > 0 ? " is-shown" : ""}`}>
      <div className="reveal-inner">
        <section className="recent-bar" aria-label="Недавно открытые">
          <span className="recent-bar-label">
            <svg className="ui-ico-xs" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
            <span>Недавние:</span>
          </span>

          <button
            type="button"
            className={`recent-scroll-btn${canLeft ? "" : " is-hidden"}`}
            aria-label="Пролистать влево"
            tabIndex={canLeft ? 0 : -1}
            onClick={() => scrollStrip(-1)}
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round"><polyline points="15 18 9 12 15 6" /></svg>
          </button>

          <div
            ref={listRef}
            className={`recent-bar-list${canLeft ? " fade-left" : ""}${canRight ? " fade-right" : ""}`}
            onScroll={syncArrows}
          >
            {items.map((it, i) => {
              const fb = fallbackIconDataUri(it.title);
              return (
                <span key={it.group_id} className={`recent-pill-wrap${i === 0 ? " is-entering" : ""}`}>
                  <button
                    type="button"
                    className="recent-pill"
                    title={it.title}
                    onClick={() => onOpen(it)}
                  >
                    <img
                      width={18}
                      height={18}
                      src={it.icon_url ? cachedIconUrl(it.icon_url, 48) : fb}
                      alt=""
                      onError={(e) => {
                        e.currentTarget.src = fb;
                      }}
                    />
                    <span>{it.title}</span>
                  </button>
                  <button
                    type="button"
                    className="recent-pill-del"
                    title={`Убрать «${it.title}» из истории`}
                    aria-label={`Убрать ${it.title} из истории`}
                    onClick={(e) => {
                      e.stopPropagation();
                      onRemoveOne(it.group_id);
                    }}
                  >
                    <CloseIcon size={10} />
                  </button>
                </span>
              );
            })}
          </div>

          <button
            type="button"
            className={`recent-scroll-btn${canRight ? "" : " is-hidden"}`}
            aria-label="Пролистать вправо"
            tabIndex={canRight ? 0 : -1}
            onClick={() => scrollStrip(1)}
          >
            <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round"><polyline points="9 18 15 12 9 6" /></svg>
          </button>

          <button type="button" className="btn btn-ghost btn-xs recent-clear-btn" title="Очистить историю" aria-label="Очистить историю" onClick={onClear}>
            <CloseIcon size={13} />
          </button>
        </section>
      </div>
    </div>
  );
};
