import { useLayoutEffect, useRef } from "react";

const focusable = 'button, input, select, textarea, a[href], [tabindex], [contenteditable="true"]';

export function useModalFocus(open: boolean, onClose: () => void, busy = false) {
  const dialogRef = useRef<HTMLDivElement>(null);
  const action = useRef({ onClose, busy });
  useLayoutEffect(() => { action.current = { onClose, busy }; }, [onClose, busy]);

  useLayoutEffect(() => {
    const dialog = dialogRef.current;
    if (!open || !dialog) return;
    const previous = document.activeElement;
    dialog.focus();

    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopPropagation();
        if (!action.current.busy) action.current.onClose();
      } else if (event.key === "Tab") {
        const items = [...dialog.querySelectorAll<HTMLElement>(focusable)].filter((item) =>
          item.tabIndex >= 0 && !item.matches(":disabled") && item.getClientRects().length > 0 && getComputedStyle(item).visibility === "visible");
        const active = items.indexOf(document.activeElement as HTMLElement);
        if (active < 0 || (event.shiftKey ? active === 0 : active === items.length - 1)) {
          event.preventDefault();
          (items[event.shiftKey ? items.length - 1 : 0] ?? dialog).focus();
        }
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("keydown", onKeyDown);
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus();
    };
  }, [open]);
  return dialogRef;
}
