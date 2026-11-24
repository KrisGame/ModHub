import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { isWailsDesktop, subscribeBackendEvents } from "../services/bridge";
import { pushToast } from "../toastBus";
import type { AccountsSnapshot, AccountView, AuthFlowProgress } from "../types";

const EMPTY_FLOW: AuthFlowProgress = {
  active: false,
  stage: "idle",
  message: "",
  updated_at: 0,
};

export function useAccounts() {
  const [snap, setSnap] = useState<AccountsSnapshot>({
    accounts: [],
    active_account_id: "",
    config: { configured: false, source: "none", vault_backend: "", redirect_note: "" },
    flow: EMPTY_FLOW,
  });
  const [loading, setLoading] = useState(true);

  // Защита жизненного цикла от обновлений после unmount (RAII)
  const isMountedRef = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const next = await api.getAccounts();
      if (!isMountedRef.current) return;
      setSnap((prev) => {
        const normalized: AccountsSnapshot = {
          accounts: next.accounts || [],
          active_account_id: next.active_account_id || "",
          config: next.config || { configured: false, source: "none", vault_backend: "", redirect_note: "" },
          flow: next.flow || EMPTY_FLOW,
        };
        if (JSON.stringify(prev) === JSON.stringify(normalized)) {
          return prev;
        }
        return normalized;
      });
    } catch {
      // Игнорируем переходные ошибки сети при старте лаунчера
    } finally {
      if (isMountedRef.current) {
        setLoading(false);
      }
    }
  }, []);

  useEffect(() => {
    isMountedRef.current = true;
    void refresh();

    return () => {
      isMountedRef.current = false;
    };
  }, [refresh]);

  // Подписка на IPC события авторизации Microsoft и изменения аккаунтов
  useEffect(() => {
    const unsubscribe = subscribeBackendEvents((ev) => {
      if (!isMountedRef.current) return;

      try {
        switch (ev.name) {
          case "auth.started":
          case "auth.browser_opened":
          case "auth.oauth_completed":
          case "auth.xbox_started":
          case "auth.xbox_completed":
          case "auth.minecraft_started":
          case "auth.license_checked":
          case "auth.profile_loaded":
          case "auth.error":
          case "auth.reauth_required": {
            if (ev.payload && typeof ev.payload === "object" && "stage" in ev.payload) {
              const p = ev.payload as AuthFlowProgress;
              setSnap((prev) => ({ ...prev, flow: p }));
              if (ev.name === "auth.error" && p.error) {
                pushToast(p.error, "err");
                void refresh();
              } else if (ev.name === "auth.reauth_required" && p.message) {
                pushToast(p.message, "err");
                void refresh();
              }
            }
            break;
          }
          case "account.updated":
          case "account.active_changed":
          case "account.removed":
          case "config.updated": {
            void refresh();
            break;
          }
        }
      } catch (err) {
        console.error("[useAccounts] Ошибка обработки IPC события аккаунтов:", err);
      }
    });

    return () => {
      unsubscribe();
    };
  }, [refresh]);

  // Фоновый опрос для web-режима (с подавлением при неактивном окне браузера)
  useEffect(() => {
    if (isWailsDesktop() || !snap.flow.active) return;

    let tid: number | null = null;
    const startPolling = () => {
      if (tid === null && document.visibilityState !== "hidden") {
        tid = window.setInterval(() => {
          void refresh();
        }, 800);
      }
    };
    const stopPolling = () => {
      if (tid !== null) {
        window.clearInterval(tid);
        tid = null;
      }
    };

    const handleVisibilityChange = () => {
      if (document.visibilityState === "hidden") {
        stopPolling();
      } else {
        startPolling();
      }
    };

    startPolling();
    document.addEventListener("visibilitychange", handleVisibilityChange);

    return () => {
      stopPolling();
      document.removeEventListener("visibilitychange", handleVisibilityChange);
    };
  }, [snap.flow.active, refresh]);

  const startLogin = useCallback(async (reauthAccountId = "") => {
    await api.startMicrosoftLogin(reauthAccountId);
    if (!isMountedRef.current) return;
    setSnap((prev) => ({
      ...prev,
      flow: {
        active: true,
        account_id: reauthAccountId,
        stage: "started",
        message: "Запуск официальной авторизации Microsoft OAuth 2.0 (PKCE)...",
        updated_at: Math.floor(Date.now() / 1000),
      },
    }));
  }, []);

  const cancelLogin = useCallback(async () => {
    await api.cancelMicrosoftLogin();
    pushToast("Авторизация Microsoft отменена", "info");
    await refresh();
  }, [refresh]);

  const addOffline = useCallback(async (username: string): Promise<AccountView> => {
    const v = await api.addOfflineAccount(username);
    await refresh();
    pushToast(`Офлайн-аккаунт добавлен: ${v.minecraft_username}`, "ok");
    return v;
  }, [refresh]);

  const setActive = useCallback(async (id: string): Promise<AccountView> => {
    const v = await api.setActiveAccount(id);
    await refresh();
    pushToast(`Активный аккаунт: ${v.minecraft_username || v.xbox_gamertag || v.id}`, "ok");
    return v;
  }, [refresh]);

  const refreshAuth = useCallback(async (id: string, force = true): Promise<AccountView> => {
    const v = await api.refreshAccountAuth(id, force);
    await refresh();
    pushToast("Токены авторизации обновлены", "ok");
    return v;
  }, [refresh]);

  const checkLicense = useCallback(async (id: string): Promise<AccountView> => {
    try {
      const v = await api.checkAccountLicense(id);
      await refresh();
      if (v.license_status === "owned") {
        pushToast("Лицензия Minecraft Java Edition подтверждена", "ok");
      } else if (v.license_status === "not_owned") {
        pushToast("Лицензия Minecraft Java Edition не найдена на этом аккаунте", "err");
      } else {
        pushToast("Проверка лицензии не завершилась из-за ошибки сервиса", "err");
      }
      return v;
    } catch (err) {
      await refresh();
      throw err;
    }
  }, [refresh]);

  const signOut = useCallback(async (id: string): Promise<AccountView> => {
    const v = await api.signOutAccount(id);
    await refresh();
    pushToast("Сессия аккаунта в ModHub завершена, токены удалены", "info");
    return v;
  }, [refresh]);

  const remove = useCallback(async (id: string): Promise<void> => {
    await api.deleteAccount(id);
    await refresh();
    pushToast("Аккаунт и его защищённые токены удалены из ModHub", "info");
  }, [refresh]);

  const activeAccount = useMemo(() => {
    return snap.accounts.find((a) => a.active) || null;
  }, [snap.accounts]);

  return {
    accounts: snap.accounts,
    activeAccount,
    activeAccountId: snap.active_account_id || "",
    config: snap.config,
    flow: snap.flow,
    loading,
    refresh,
    startLogin,
    cancelLogin,
    addOffline,
    setActive,
    refreshAuth,
    checkLicense,
    signOut,
    remove,
  };
}
