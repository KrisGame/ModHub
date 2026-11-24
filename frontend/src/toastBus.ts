import type { ToastMessage } from "./types";

type Listener = (toasts: ToastMessage[]) => void;

let seq = 0;
let list: ToastMessage[] = [];
const listeners = new Set<Listener>();

function emit() {
  for (const fn of listeners) fn(list);
}

export function pushToast(text: string, kind: "info" | "ok" | "err" = "info") {
  const id = ++seq;
  list = [...list.slice(-3), { id, text, kind, leaving: false }];
  emit();
  window.setTimeout(() => {
    list = list.map((t) => (t.id === id ? { ...t, leaving: true } : t));
    emit();
    window.setTimeout(() => {
      list = list.filter((t) => t.id !== id);
      emit();
    }, 260);
  }, 3150);
}

export function subscribeToasts(fn: Listener): () => void {
  listeners.add(fn);
  fn(list);
  return () => {
    listeners.delete(fn);
  };
}
