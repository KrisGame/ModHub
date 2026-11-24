import { useEffect, useState, type RefObject } from "react";

/**
 * Coordinates mount -> double-RAF open -> smooth exit -> unmount so every overlay
 * plays both its enter AND exit CSS transitions smoothly with zero snapping.
 */
export function useOverlayTransition(open: boolean, exitMs = 240): { mounted: boolean; shown: boolean } {
  const [mounted, setMounted] = useState(open);
  const [shown, setShown] = useState(false);

  useEffect(() => {
    let raf1 = 0;
    let raf2 = 0;
    let timer = 0;

    if (open) {
      setMounted(true);
      raf1 = requestAnimationFrame(() => {
        raf2 = requestAnimationFrame(() => {
          setShown(true);
        });
      });
    } else {
      setShown(false);
      timer = window.setTimeout(() => {
        setMounted(false);
      }, exitMs);
    }

    return () => {
      if (raf1) cancelAnimationFrame(raf1);
      if (raf2) cancelAnimationFrame(raf2);
      if (timer) window.clearTimeout(timer);
    };
  }, [open, exitMs]);

  return { mounted, shown };
}

/**
 * Toggles `scroll-fade-top` / `scroll-fade-bottom` classes directly on a scrollable
 * container via RAF + ResizeObserver (zero React re-renders during scroll).
 */
export function useScrollFade(
  ref: RefObject<HTMLElement | null>,
  deps: readonly unknown[] = []
): void {
  useEffect(() => {
    const el = ref.current;
    if (!el) return;

    let raf = 0;
    const sync = () => {
      raf = 0;
      const canScroll = el.scrollHeight > el.clientHeight + 4;
      const top = canScroll && el.scrollTop > 4;
      const bot = canScroll && el.scrollTop + el.clientHeight < el.scrollHeight - 4;
      el.classList.toggle("scroll-fade-top", top);
      el.classList.toggle("scroll-fade-bottom", bot);
    };

    const schedule = () => {
      if (!raf) raf = requestAnimationFrame(sync);
    };

    schedule();
    const ro = new ResizeObserver(schedule);
    ro.observe(el);
    el.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule, { passive: true });

    return () => {
      if (raf) cancelAnimationFrame(raf);
      ro.disconnect();
      el.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
      el.classList.remove("scroll-fade-top", "scroll-fade-bottom");
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}

/**
 * Closes active dialogs/overlays cleanly when the user presses Escape.
 */
export function useEscapeKey(active: boolean, onEscape: () => void): void {
  useEffect(() => {
    if (!active) return;
    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onEscape();
      }
    };
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, [active, onEscape]);
}
