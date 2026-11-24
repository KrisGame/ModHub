let targetY = 0;
let currentY = 0;
let rafId = 0;
let lastTs = 0;
let animating = false;

let bgScrollLocked = false;
let lockedScrollY = 0;

const ISOLATED_SCROLL_SELECTORS = [
  ".filters-sidebar",
  ".modal-window",
  ".modal-backdrop",
  ".drawer-panel",
  ".drawer-backdrop",
  ".lightbox-backdrop",
  ".preload-gate-backdrop",
  ".custom-select-menu",
  ".deps-suggest-list",
  ".card-ctx-menu",
  ".recent-bar-list",
  ".mh-nav-rail",
  ".mh-nav-accounts-list",
];

export function getPageScroller(): HTMLElement | null {
  if (typeof document === "undefined") return null;
  return document.querySelector<HTMLElement>(".mh-shell-workspace");
}

export function getPageScrollTop(): number {
  const scroller = getPageScroller();
  return scroller ? scroller.scrollTop : window.scrollY;
}

export function setPageScrollTop(y: number) {
  const scroller = getPageScroller();
  if (scroller) {
    scroller.scrollTop = y;
  } else {
    window.scrollTo(0, y);
  }
}

export function getPageMaxScrollTop(): number {
  const scroller = getPageScroller();
  if (scroller) {
    return Math.max(0, scroller.scrollHeight - scroller.clientHeight);
  }
  return Math.max(0, document.documentElement.scrollHeight - window.innerHeight);
}

export function getPageScrollBottomDistance(): number {
  const scroller = getPageScroller();
  if (scroller) {
    return scroller.scrollHeight - (scroller.scrollTop + scroller.clientHeight);
  }
  return document.documentElement.scrollHeight - (window.scrollY + window.innerHeight);
}

export function subscribePageScroll(listener: () => void): () => void {
  const scroller = getPageScroller();
  if (scroller) {
    scroller.addEventListener("scroll", listener, { passive: true });
    return () => scroller.removeEventListener("scroll", listener);
  }
  window.addEventListener("scroll", listener, { passive: true });
  return () => window.removeEventListener("scroll", listener);
}

function maxScrollY(): number {
  return getPageMaxScrollTop();
}

/**
 * Sticky-safe background scroll lock: locks workspace scroll position without changing
 * `overflow`, so sticky headers inside `.mh-shell-workspace` never shift or break.
 */
export function setBackgroundScrollLocked(locked: boolean) {
  if (locked === bgScrollLocked) return;
  bgScrollLocked = locked;
  if (locked) {
    stopSmoothScroll();
    lockedScrollY = getPageScrollTop();
  }
}

function findIsolatedScrollTarget(startEl: Element | null): { isolated: boolean; scrollEl: HTMLElement | null } {
  let el: Element | null = startEl;
  let nearestScrollable: HTMLElement | null = null;
  let insideIsolated = false;
  const pageScroller = getPageScroller();

  while (el && el !== pageScroller && el !== document.body && el !== document.documentElement) {
    if (!nearestScrollable && el instanceof HTMLElement) {
      const style = window.getComputedStyle(el);
      const oy = style.overflowY;
      if ((oy === "auto" || oy === "scroll") && el.scrollHeight > el.clientHeight + 1) {
        nearestScrollable = el;
      }
    }
    for (const sel of ISOLATED_SCROLL_SELECTORS) {
      if (el.matches(sel)) {
        insideIsolated = true;
        break;
      }
    }
    if (insideIsolated && nearestScrollable) {
      break;
    }
    el = el.parentElement;
  }

  return {
    isolated: insideIsolated || nearestScrollable !== null,
    scrollEl: nearestScrollable,
  };
}

function step(ts: number) {
  if (bgScrollLocked) {
    stopSmoothScroll();
    return;
  }
  const dt = lastTs ? Math.min(0.05, (ts - lastTs) / 1000) : 0.016;
  lastTs = ts;

  const diff = targetY - currentY;
  if (Math.abs(diff) < 0.75) {
    currentY = targetY;
    setPageScrollTop(Math.round(currentY));
    animating = false;
    rafId = 0;
    lastTs = 0;
    return;
  }

  const alpha = 1 - Math.exp(-dt * 19);
  currentY += diff * alpha;
  setPageScrollTop(currentY);
  rafId = requestAnimationFrame(step);
}

export function scrollToTopSmooth() {
  if (bgScrollLocked) return;
  const cur = getPageScrollTop();
  if (cur <= 2) return;
  currentY = cur;
  targetY = 0;
  if (!animating) {
    animating = true;
    lastTs = 0;
    rafId = requestAnimationFrame(step);
  }
}

export function stopSmoothScroll() {
  if (rafId) {
    cancelAnimationFrame(rafId);
    rafId = 0;
  }
  animating = false;
  lastTs = 0;
  const cur = getPageScrollTop();
  currentY = cur;
  targetY = cur;
}

export function initSmoothScroll(): () => void {
  const reducedMotion =
    window.matchMedia && window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  const onWheel = (e: WheelEvent) => {
    if (e.defaultPrevented || e.ctrlKey || e.metaKey || e.altKey) return;
    if (Math.abs(e.deltaY) < 0.5 || Math.abs(e.deltaX) > Math.abs(e.deltaY)) return;

    const targetEl = e.target instanceof Element ? e.target : null;
    const { isolated, scrollEl } = findIsolatedScrollTarget(targetEl);

    if (isolated) {
      if (!scrollEl) {
        e.preventDefault();
        return;
      }
      const atTop = scrollEl.scrollTop <= 0;
      const atBottom = scrollEl.scrollTop + scrollEl.clientHeight >= scrollEl.scrollHeight - 1;
      if ((e.deltaY < 0 && atTop) || (e.deltaY > 0 && atBottom)) {
        e.preventDefault();
      }
      return;
    }

    if (bgScrollLocked) {
      e.preventDefault();
      return;
    }

    if (reducedMotion) return;
    if (e.deltaMode === 0 && Math.abs(e.deltaY) < 28) return;

    e.preventDefault();

    let delta = e.deltaY;
    if (e.deltaMode === 1) delta *= 36;
    else if (e.deltaMode === 2) delta *= (getPageScroller()?.clientHeight || window.innerHeight) * 0.85;

    if (!animating) {
      currentY = getPageScrollTop();
      targetY = currentY;
    }
    targetY = Math.max(0, Math.min(maxScrollY(), targetY + delta * 1.12));

    if (!animating) {
      animating = true;
      lastTs = 0;
      rafId = requestAnimationFrame(step);
    }
  };

  const onTouchMove = (e: TouchEvent) => {
    if (!bgScrollLocked) return;
    const targetEl = e.target instanceof Element ? e.target : null;
    const { scrollEl } = findIsolatedScrollTarget(targetEl);
    if (!scrollEl) {
      e.preventDefault();
    }
  };

  const onScrollGuard = () => {
    if (bgScrollLocked && Math.abs(getPageScrollTop() - lockedScrollY) > 0.5) {
      setPageScrollTop(lockedScrollY);
    }
  };

  const onManualInteract = () => {
    if (animating) stopSmoothScroll();
  };

  const onKeyDown = (e: KeyboardEvent) => {
    const isScrollKey =
      e.key === "PageDown" ||
      e.key === "PageUp" ||
      e.key === "Home" ||
      e.key === "End" ||
      e.key === "ArrowDown" ||
      e.key === "ArrowUp" ||
      e.key === " ";

    if (!isScrollKey) return;

    if (bgScrollLocked) {
      const target = e.target as HTMLElement | null;
      const isEditable =
        target &&
        (target.tagName === "INPUT" ||
          target.tagName === "TEXTAREA" ||
          target.tagName === "SELECT" ||
          target.isContentEditable);
      if (!isEditable) {
        const { scrollEl } = findIsolatedScrollTarget(target);
        if (!scrollEl) {
          e.preventDefault();
        }
      }
      return;
    }

    if (e.key !== " ") {
      onManualInteract();
    }
  };

  window.addEventListener("wheel", onWheel, { passive: false });
  window.addEventListener("touchmove", onTouchMove, { passive: false });
  const unsubScrollGuard = subscribePageScroll(onScrollGuard);
  window.addEventListener("mousedown", onManualInteract, { passive: true });
  window.addEventListener("touchstart", onManualInteract, { passive: true });
  window.addEventListener("keydown", onKeyDown);

  return () => {
    stopSmoothScroll();
    bgScrollLocked = false;
    window.removeEventListener("wheel", onWheel);
    window.removeEventListener("touchmove", onTouchMove);
    unsubScrollGuard();
    window.removeEventListener("mousedown", onManualInteract);
    window.removeEventListener("touchstart", onManualInteract);
    window.removeEventListener("keydown", onKeyDown);
  };
}
