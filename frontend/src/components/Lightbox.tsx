import React, { useEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import type { GalleryItem } from "../types";

interface LightboxProps {
  items: GalleryItem[];
  index: number;
  onClose: () => void;
  onChangeIndex: (idx: number) => void;
}

function computeUniformBox(natW: number, natH: number): { w: number; h: number } {
  const isMobile = window.innerWidth <= 720;
  const maxW = Math.max(240, window.innerWidth - (isMobile ? 28 : 176));
  const targetH = Math.max(220, Math.min(Math.round(window.innerHeight * (isMobile ? 0.66 : 0.72)), 700));
  if (natW <= 0 || natH <= 0) {
    return { w: Math.min(maxW, Math.round((targetH * 16) / 9)), h: targetH };
  }
  const aspect = natW / natH;
  let h = targetH;
  let w = Math.round(h * aspect);
  if (w > maxW) {
    w = maxW;
    h = Math.max(160, Math.round(w / aspect));
  }
  return { w, h };
}

export const Lightbox: React.FC<LightboxProps> = ({ items, index, onClose, onChangeIndex }) => {
  const open = items.length > 0 && index >= 0 && index < items.length;
  const { mounted, shown } = useOverlayTransition(open, 220);
  const [retained, setRetained] = useState<{ items: GalleryItem[]; index: number }>({ items, index });

  useEffect(() => {
    if (open) setRetained({ items, index });
  }, [open, items, index]);

  const dispItems = open ? items : retained.items;
  const dispIndex = open ? index : retained.index;
  const cur = dispItems.length > 0 && dispIndex >= 0 && dispIndex < dispItems.length ? dispItems[dispIndex] : null;

  const prevIdxRef = useRef<number>(-1);
  const thumbsRef = useRef<HTMLDivElement | null>(null);
  const natRef = useRef<{ w: number; h: number }>({ w: 16, h: 9 });
  const frameRef = useRef<HTMLDivElement | null>(null);
  const [dir, setDir] = useState<"enter" | "next" | "prev">("enter");
  const [frameBox, setFrameBox] = useState<{ w: number; h: number } | null>(null);
  const [zoomScale, setZoomScale] = useState(1);

  const transformRef = useRef({
    scale: 1,
    tx: 0,
    ty: 0,
    swipeX: 0,
    dragging: false,
    mode: "none" as "none" | "pan" | "swipe",
    startX: 0,
    startY: 0,
    originTx: 0,
    originTy: 0,
  });

  const applyFrameTransform = (withSpring: boolean) => {
    const el = frameRef.current;
    if (!el) return;
    const t = transformRef.current;
    const totalX = t.tx + t.swipeX;
    el.style.transition = withSpring
      ? "transform 0.28s cubic-bezier(0.22, 1.18, 0.36, 1)"
      : "none";
    el.style.transform = `translate3d(${totalX.toFixed(1)}px, ${t.ty.toFixed(1)}px, 0) scale(${t.scale.toFixed(3)})`;
  };

  const resetZoomAndSwipe = () => {
    const t = transformRef.current;
    t.scale = 1;
    t.tx = 0;
    t.ty = 0;
    t.swipeX = 0;
    t.dragging = false;
    t.mode = "none";
    setZoomScale(1);
    applyFrameTransform(false);
  };

  useEffect(() => {
    if (!open || !cur?.url) {
      prevIdxRef.current = -1;
      setDir("enter");
      setFrameBox(null);
      resetZoomAndSwipe();
      return;
    }

    resetZoomAndSwipe();
    const prev = prevIdxRef.current;
    if (prev >= 0 && prev !== dispIndex && dispItems.length > 1) {
      const isNext =
        (dispIndex > prev && !(prev === 0 && dispIndex === dispItems.length - 1)) ||
        (prev === dispItems.length - 1 && dispIndex === 0);
      setDir(isNext ? "next" : "prev");
    } else {
      setDir("enter");
    }
    prevIdxRef.current = dispIndex;

    const strip = thumbsRef.current;
    if (strip) {
      const activeBtn = strip.querySelector<HTMLElement>(`[data-thumb-idx="${dispIndex}"]`);
      if (activeBtn) {
        activeBtn.scrollIntoView({ behavior: "smooth", inline: "center", block: "nearest" });
      }
    }

    if (dispItems.length > 1) {
      const nextUrl = dispItems[(dispIndex + 1) % dispItems.length]?.url;
      const prevUrl = dispItems[(dispIndex - 1 + dispItems.length) % dispItems.length]?.url;
      if (nextUrl) {
        const im1 = new Image();
        im1.src = nextUrl;
      }
      if (prevUrl && prevUrl !== nextUrl) {
        const im2 = new Image();
        im2.src = prevUrl;
      }
    }
  }, [open, cur?.url, dispIndex, dispItems]);

  useEffect(() => {
    if (!open) return;
    const onResize = () => {
      if (natRef.current.w > 0 && natRef.current.h > 0) {
        setFrameBox(computeUniformBox(natRef.current.w, natRef.current.h));
      }
    };
    window.addEventListener("resize", onResize, { passive: true });
    return () => window.removeEventListener("resize", onResize);
  }, [open]);

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        if (transformRef.current.scale > 1.02) {
          resetZoomAndSwipe();
        } else {
          onClose();
        }
      } else if (e.key === "ArrowLeft") {
        onChangeIndex((index - 1 + items.length) % items.length);
      } else if (e.key === "ArrowRight") {
        onChangeIndex((index + 1) % items.length);
      }
    };
    window.addEventListener("keydown", onKey, true);
    return () => window.removeEventListener("keydown", onKey, true);
  }, [open, index, items.length, onClose, onChangeIndex]);

  if (!mounted || !cur) return null;

  const animClass =
    dir === "next" ? "lb-img-next" : dir === "prev" ? "lb-img-prev" : "lb-img-enter";

  const syncImgDimensions = (img: HTMLImageElement | null) => {
    if (!img || img.naturalWidth <= 0 || img.naturalHeight <= 0) return;
    natRef.current = { w: img.naturalWidth, h: img.naturalHeight };
    const next = computeUniformBox(img.naturalWidth, img.naturalHeight);
    setFrameBox((prev) => (prev && prev.w === next.w && prev.h === next.h ? prev : next));
  };

  const toggleZoom = (e: React.MouseEvent) => {
    e.stopPropagation();
    const t = transformRef.current;
    if (Math.abs(t.swipeX) > 10) return;
    if (t.scale > 1.05) {
      resetZoomAndSwipe();
    } else {
      t.scale = 1.85;
      t.tx = 0;
      t.ty = 0;
      setZoomScale(1.85);
      applyFrameTransform(true);
    }
  };

  const handleStageWheel = (e: React.WheelEvent) => {
    e.stopPropagation();
    const t = transformRef.current;
    const delta = e.deltaY < 0 ? 0.25 : -0.25;
    const nextScale = Math.max(1, Math.min(3.2, Number((t.scale + delta).toFixed(2))));
    t.scale = nextScale;
    if (nextScale <= 1.01) {
      t.scale = 1;
      t.tx = 0;
      t.ty = 0;
    }
    setZoomScale(t.scale);
    applyFrameTransform(true);
  };

  const handlePointerDown = (e: React.PointerEvent) => {
    if (e.button !== 0 && e.pointerType === "mouse") return;
    e.stopPropagation();
    const t = transformRef.current;
    t.dragging = true;
    t.startX = e.clientX;
    t.startY = e.clientY;
    t.originTx = t.tx;
    t.originTy = t.ty;
    t.mode = t.scale > 1.05 ? "pan" : "swipe";
    (e.currentTarget as HTMLElement).setPointerCapture?.(e.pointerId);
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    const t = transformRef.current;
    if (!t.dragging) return;
    const dx = e.clientX - t.startX;
    const dy = e.clientY - t.startY;

    if (t.mode === "pan") {
      const maxX = 260 * (t.scale - 1);
      const maxY = 180 * (t.scale - 1);
      t.tx = Math.max(-maxX, Math.min(maxX, t.originTx + dx));
      t.ty = Math.max(-maxY, Math.min(maxY, t.originTy + dy));
      applyFrameTransform(false);
    } else if (t.mode === "swipe" && dispItems.length > 1) {
      t.swipeX = dx * 0.62;
      applyFrameTransform(false);
    }
  };

  const handlePointerUp = (e: React.PointerEvent) => {
    const t = transformRef.current;
    if (!t.dragging) return;
    t.dragging = false;
    (e.currentTarget as HTMLElement).releasePointerCapture?.(e.pointerId);

    if (t.mode === "swipe" && dispItems.length > 1) {
      const rawDx = e.clientX - t.startX;
      t.swipeX = 0;
      applyFrameTransform(true);
      if (rawDx < -78) {
        onChangeIndex((dispIndex + 1) % dispItems.length);
      } else if (rawDx > 78) {
        onChangeIndex((dispIndex - 1 + dispItems.length) % dispItems.length);
      }
    }
    t.mode = "none";
  };

  const content = (
    <div
      className={`lightbox-backdrop lightbox-clear overlay-animated${shown ? " is-open" : ""}`}
      role="dialog"
      aria-modal="true"
      aria-hidden={!shown}
      onClick={onClose}
    >
      <div className="lightbox-topbar" onClick={(e) => e.stopPropagation()}>
        <div className="lightbox-counter">
          {dispIndex + 1} / {dispItems.length}
        </div>
        <div className={`lightbox-title${cur.title ? "" : " is-empty"}`}>{cur.title || ""}</div>
        <div className="lightbox-top-actions">
          {zoomScale > 1.02 && (
            <button
              type="button"
              className="lightbox-zoom-reset"
              onClick={resetZoomAndSwipe}
              title="Сбросить масштаб"
            >
              {Math.round(zoomScale * 100)}% • Сброс
            </button>
          )}
          <button type="button" className="lightbox-close-btn" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={18} />
          </button>
        </div>
      </div>

      {dispItems.length > 1 && (
        <button
          className="lightbox-nav lightbox-prev"
          type="button"
          aria-label="Предыдущая фотография"
          onClick={(e) => {
            e.stopPropagation();
            onChangeIndex((dispIndex - 1 + dispItems.length) % dispItems.length);
          }}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="15 18 9 12 15 6" />
          </svg>
        </button>
      )}

      <div className="lightbox-stage" onClick={onClose} onWheel={handleStageWheel}>
        <div
          ref={frameRef}
          key={`${dispIndex}-${cur.url}`}
          className={`lightbox-img-frame ${animClass}${zoomScale > 1.02 ? " is-zoomed" : ""}`}
          style={frameBox ? { width: `${frameBox.w}px`, height: `${frameBox.h}px` } : undefined}
          onClick={toggleZoom}
          onPointerDown={handlePointerDown}
          onPointerMove={handlePointerMove}
          onPointerUp={handlePointerUp}
        >
          <img
            src={cur.url}
            alt={cur.title || ""}
            className="lb-img-single"
            draggable={false}
            onLoad={(e) => syncImgDimensions(e.currentTarget)}
            ref={(el) => {
              if (el && el.complete && el.naturalWidth > 0) syncImgDimensions(el);
            }}
          />
        </div>
      </div>

      {dispItems.length > 1 && (
        <button
          className="lightbox-nav lightbox-next"
          type="button"
          aria-label="Следующая фотография"
          onClick={(e) => {
            e.stopPropagation();
            onChangeIndex((dispIndex + 1) % dispItems.length);
          }}
        >
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.4" strokeLinecap="round" strokeLinejoin="round">
            <polyline points="9 18 15 12 9 6" />
          </svg>
        </button>
      )}

      <div className="lightbox-bottombar" onClick={(e) => e.stopPropagation()}>
        {cur.description && <p className="lightbox-caption">{cur.description}</p>}
        {dispItems.length > 1 && (
          <div className="lightbox-thumbs" ref={thumbsRef}>
            {dispItems.map((g, i) => (
              <button
                key={`${g.url}-${i}`}
                data-thumb-idx={i}
                type="button"
                className={`lightbox-thumb${i === dispIndex ? " active" : ""}`}
                onClick={() => onChangeIndex(i)}
              >
                <img src={g.url} alt="" loading="lazy" />
              </button>
            ))}
          </div>
        )}
      </div>
    </div>
  );

  return createPortal(content, document.body);
};
