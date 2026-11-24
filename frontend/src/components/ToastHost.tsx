import React, { useEffect, useState } from "react";
import { subscribeToasts } from "../toastBus";
import type { ToastMessage } from "../types";

const ToastIcon: React.FC<{ kind: ToastMessage["kind"] }> = ({ kind }) => {
  if (kind === "ok") {
    return (
      <svg className="toast-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="9.5" />
        <polyline points="8.5 12.5 11 15 16 9.5" />
      </svg>
    );
  }
  if (kind === "err") {
    return (
      <svg className="toast-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <circle cx="12" cy="12" r="9.5" />
        <line x1="12" y1="8" x2="12" y2="12.5" />
        <line x1="12" y1="16" x2="12.01" y2="16" />
      </svg>
    );
  }
  return (
    <svg className="toast-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
      <circle cx="12" cy="12" r="9.5" />
      <line x1="12" y1="11" x2="12" y2="16" />
      <line x1="12" y1="8" x2="12.01" y2="8" />
    </svg>
  );
};

export const ToastHost: React.FC = () => {
  const [toasts, setToasts] = useState<ToastMessage[]>([]);

  useEffect(() => subscribeToasts(setToasts), []);

  return (
    <div className="toast-container" aria-live="polite">
      {toasts.map((t) => (
        <div key={t.id} className={`toast ${t.kind} toast-${t.kind}${t.leaving ? " is-leaving" : ""}`}>
          <div className="toast-row">
            <ToastIcon kind={t.kind} />
            <span className="toast-msg">{t.text}</span>
          </div>
          <span className="toast-timer-bar" aria-hidden="true" />
        </div>
      ))}
    </div>
  );
};
