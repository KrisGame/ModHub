import React, { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";

interface AnchorBox {
  left: number;
  right: number;
  top: number;
  bottom: number;
  width: number;
  height: number;
}

interface TipState {
  visible: boolean;
  text: string;
  anchor: AnchorBox | null;
}

interface TipPos {
  left: number;
  top: number;
  place: "top" | "bottom" | "left" | "right";
  ready: boolean;
}

const VIEWPORT_MARGIN = 10;
const OFFSET_GAP = 8;

function normalizeTipText(raw: string): string {
  return raw.replace(/\s+/g, " ").trim();
}

export const TooltipHost: React.FC = () => {
  const [tip, setTip] = useState<TipState>({
    visible: false,
    text: "",
    anchor: null,
  });
  const [pos, setPos] = useState<TipPos>({
    left: -9999,
    top: -9999,
    place: "top",
    ready: false,
  });

  const tipRef = useRef<HTMLDivElement | null>(null);
  const activeElRef = useRef<HTMLElement | null>(null);
  const savedTitleRef = useRef<string | null>(null);
  const timerRef = useRef<number | null>(null);
  const visibleRef = useRef(false);

  useEffect(() => {
    visibleRef.current = tip.visible;
  }, [tip.visible]);

  useLayoutEffect(() => {
    if (!tip.visible || !tip.anchor || !tipRef.current) {
      setPos((prev) => (prev.ready ? { ...prev, ready: false } : prev));
      return;
    }

    const el = tipRef.current;
    const vw = window.innerWidth;
    const vh = window.innerHeight;
    const a = tip.anchor;

    const tipW = el.offsetWidth;
    const tipH = el.offsetHeight;

    // Prefer top; flip to bottom if top overflows; flip to right/left if vertical space is exhausted
    let place: TipPos["place"] = "top";
    let top = a.top - OFFSET_GAP - tipH;
    let left = a.left + a.width / 2 - tipW / 2;

    if (top < VIEWPORT_MARGIN) {
      const bottomTop = a.bottom + OFFSET_GAP;
      if (bottomTop + tipH <= vh - VIEWPORT_MARGIN) {
        place = "bottom";
        top = bottomTop;
      } else {
        // Try horizontal placement beside anchor if neither top nor bottom fits cleanly
        const rightLeft = a.right + OFFSET_GAP;
        const leftLeft = a.left - OFFSET_GAP - tipW;
        if (rightLeft + tipW <= vw - VIEWPORT_MARGIN) {
          place = "right";
          left = rightLeft;
          top = a.top + a.height / 2 - tipH / 2;
        } else if (leftLeft >= VIEWPORT_MARGIN) {
          place = "left";
          left = leftLeft;
          top = a.top + a.height / 2 - tipH / 2;
        } else {
          place = bottomTop + tipH - vh < VIEWPORT_MARGIN - top ? "bottom" : "top";
          top = place === "bottom" ? bottomTop : top;
        }
      }
    }

    // Strictly clamp inside viewport margins so tooltip NEVER clips or overflows
    const maxLeft = Math.max(VIEWPORT_MARGIN, vw - tipW - VIEWPORT_MARGIN);
    const maxTop = Math.max(VIEWPORT_MARGIN, vh - tipH - VIEWPORT_MARGIN);
    left = Math.max(VIEWPORT_MARGIN, Math.min(maxLeft, left));
    top = Math.max(VIEWPORT_MARGIN, Math.min(maxTop, top));

    setPos({
      left: Math.round(left),
      top: Math.round(top),
      place,
      ready: true,
    });
  }, [tip.visible, tip.text, tip.anchor]);

  useEffect(() => {
    const clearTimer = () => {
      if (timerRef.current !== null) {
        window.clearTimeout(timerRef.current);
        timerRef.current = null;
      }
    };

    const restoreTitle = () => {
      const el = activeElRef.current;
      const orig = savedTitleRef.current;
      if (el && orig !== null && el.isConnected && !el.hasAttribute("title")) {
        el.setAttribute("title", orig);
      }
      activeElRef.current = null;
      savedTitleRef.current = null;
    };

    const hideTip = () => {
      clearTimer();
      restoreTitle();
      setTip((prev) => (prev.visible ? { ...prev, visible: false, anchor: null } : prev));
    };

    const showFor = (el: HTMLElement, text: string, delayMs: number) => {
      clearTimer();
      const cleaned = normalizeTipText(text);
      if (!cleaned) {
        hideTip();
        return;
      }
      const showNow = () => {
        if (!el.isConnected) {
          hideTip();
          return;
        }
        const rect = el.getBoundingClientRect();
        if (rect.width === 0 && rect.height === 0) {
          hideTip();
          return;
        }
        setTip({
          visible: true,
          text: cleaned,
          anchor: {
            left: rect.left,
            right: rect.right,
            top: rect.top,
            bottom: rect.bottom,
            width: rect.width,
            height: rect.height,
          },
        });
      };

      if (delayMs <= 0) {
        showNow();
      } else {
        timerRef.current = window.setTimeout(showNow, delayMs);
      }
    };

    const findTipTarget = (node: EventTarget | null): HTMLElement | null => {
      if (!(node instanceof Element)) return null;
      return node.closest<HTMLElement>("[data-tip], [title]");
    };

    const onPointerOver = (e: PointerEvent) => {
      if (e.pointerType === "touch") return;
      const target = findTipTarget(e.target);
      if (!target) {
        if (activeElRef.current) hideTip();
        return;
      }
      if (target === activeElRef.current) return;

      restoreTitle();
      const dataTip = target.getAttribute("data-tip");
      const nativeTitle = target.getAttribute("title");
      const text = (dataTip || nativeTitle || "").trim();
      if (!text) {
        hideTip();
        return;
      }

      activeElRef.current = target;
      if (nativeTitle !== null) {
        savedTitleRef.current = nativeTitle;
        target.removeAttribute("title");
      } else {
        savedTitleRef.current = null;
      }

      showFor(target, text, visibleRef.current ? 35 : 190);
    };

    const onPointerOut = (e: PointerEvent) => {
      const active = activeElRef.current;
      if (!active) return;
      const related = e.relatedTarget as Node | null;
      if (related && active.contains(related)) return;
      hideTip();
    };

    const onFocusIn = (e: FocusEvent) => {
      const target = findTipTarget(e.target);
      if (!target) return;
      try {
        if (!target.matches(":focus-visible")) return;
      } catch {
        return;
      }
      restoreTitle();
      const dataTip = target.getAttribute("data-tip");
      const nativeTitle = target.getAttribute("title");
      const text = (dataTip || nativeTitle || "").trim();
      if (!text) return;
      activeElRef.current = target;
      if (nativeTitle !== null) {
        savedTitleRef.current = nativeTitle;
        target.removeAttribute("title");
      }
      showFor(target, text, 110);
    };

    document.addEventListener("pointerover", onPointerOver, true);
    document.addEventListener("pointerout", onPointerOut, true);
    document.addEventListener("pointerdown", hideTip, true);
    document.addEventListener("focusin", onFocusIn, true);
    document.addEventListener("focusout", hideTip, true);
    window.addEventListener("scroll", hideTip, { passive: true, capture: true });
    window.addEventListener("resize", hideTip, { passive: true });
    window.addEventListener("keydown", hideTip, { passive: true });
    window.addEventListener("blur", hideTip);

    return () => {
      clearTimer();
      restoreTitle();
      document.removeEventListener("pointerover", onPointerOver, true);
      document.removeEventListener("pointerout", onPointerOut, true);
      document.removeEventListener("pointerdown", hideTip, true);
      document.removeEventListener("focusin", onFocusIn, true);
      document.removeEventListener("focusout", hideTip, true);
      window.removeEventListener("scroll", hideTip, true);
      window.removeEventListener("resize", hideTip);
      window.removeEventListener("keydown", hideTip);
      window.removeEventListener("blur", hideTip);
    };
  }, []);

  const isShown = tip.visible && pos.ready;

  return createPortal(
    <div
      ref={tipRef}
      className={`mh-tooltip place-${pos.place}${isShown ? " is-visible" : ""}`}
      role="tooltip"
      aria-hidden={!isShown}
      style={{ left: `${pos.left}px`, top: `${pos.top}px` }}
    >
      <span className="mh-tooltip-text">{tip.text}</span>
    </div>,
    document.body
  );
};
