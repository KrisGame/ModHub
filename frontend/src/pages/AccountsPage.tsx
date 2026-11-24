import React, { useState } from "react";
import { api } from "../api";
import type { AppSection } from "../components/AppNavRail";
import { SkinHead } from "../components/SkinHead";
import { SkinModal } from "../components/SkinModal";
import { useEscapeKey, useOverlayTransition } from "../hooks";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type {
  AccountAuthStatus,
  AccountLicenseStatus,
  AccountView,
  AuthConfigStatus,
  AuthFlowProgress,
} from "../types";
import { formatDate } from "../utils";

interface AccountsPageProps {
  accounts: AccountView[];
  activeAccount: AccountView | null;
  authConfig: AuthConfigStatus;
  flow: AuthFlowProgress;
  loading: boolean;
  onStartLogin: (reauthAccountId?: string) => Promise<void>;
  onCancelLogin: () => Promise<void>;
  onAddOffline: (username: string) => Promise<AccountView>;
  onSetActive: (id: string) => Promise<AccountView>;
  onRefreshAuth: (id: string, force?: boolean) => Promise<AccountView>;
  onCheckLicense: (id: string) => Promise<AccountView>;
  onSignOut: (id: string) => Promise<AccountView>;
  onDelete: (id: string) => Promise<void>;
  onConfigSaved?: (cfg: import("../types").PublicConfig) => void | Promise<void>;
  onNavigate?: (section: AppSection) => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

function authStatusMeta(st: AccountAuthStatus, lic: AccountLicenseStatus): { label: string; cls: string } {
  switch (st) {
    case "authorized":
      // Show green ONLY when full auth chain succeeded AND license is confirmed
      return lic === "owned"
        ? { label: "Авторизован", cls: "is-ok" }
        : { label: "Авторизован (без лицензии)", cls: "is-paused" };
    case "authenticating":
      return { label: "Выполняется авторизация", cls: "is-active" };
    case "reauth_required":
      return { label: "Требуется повторный вход", cls: "is-err" };
    case "service_error":
      return { label: "Временная ошибка сервиса", cls: "is-paused" };
    default:
      return { label: "Не авторизован", cls: "is-muted" };
  }
}

function licenseStatusMeta(st: AccountLicenseStatus): { label: string; cls: string } {
  switch (st) {
    case "owned":
      return { label: "Лицензия подтверждена", cls: "is-ok" };
    case "not_owned":
      return { label: "Лицензия не подтверждена", cls: "is-err" };
    case "checking":
      return { label: "Проверяется лицензия", cls: "is-active" };
    case "network_error":
      return { label: "Ошибка сети при проверке", cls: "is-paused" };
    case "service_unavailable":
      return { label: "Сервис временно недоступен", cls: "is-paused" };
    case "invalid_response":
      return { label: "Некорректный ответ сервиса", cls: "is-err" };
    default:
      return { label: "Не проверялась", cls: "is-muted" };
  }
}

function flowStageTitle(stage: string): string {
  switch (stage) {
    case "started":
      return "Подготовка Microsoft OAuth 2.0 (PKCE)";
    case "browser_opened":
      return "Ожидание входа в системном браузере";
    case "oauth_completed":
      return "Обмен кода авторизации Microsoft";
    case "xbox_started":
      return "Авторизация Xbox Live и XSTS";
    case "xbox_completed":
      return "Xbox Live и XSTS подтверждены";
    case "minecraft_started":
      return "Авторизация Minecraft Services";
    case "license_checked":
      return "Проверка лицензии Minecraft Java Edition";
    case "profile_loaded":
      return "Загрузка профиля Minecraft";
    case "completed":
      return "Авторизация завершена";
    case "cancelled":
      return "Авторизация отменена";
    case "error":
      return "Ошибка авторизации";
    default:
      return "Авторизация Microsoft";
  }
}

function fmtUnix(ts: number): string {
  if (!ts || ts <= 0) return "—";
  return formatDate(new Date(ts * 1000).toISOString());
}

function accountDisplayName(a: AccountView): string {
  return a.minecraft_username || a.profile_name || a.xbox_gamertag || "Аккаунт Microsoft";
}

export const AccountsPage: React.FC<AccountsPageProps> = ({
  accounts,
  activeAccount,
  authConfig,
  flow,
  loading,
  onStartLogin,
  onCancelLogin,
  onAddOffline,
  onSetActive,
  onRefreshAuth,
  onCheckLicense,
  onSignOut,
  onDelete,
  onToast,
}) => {
  const [busyId, setBusyId] = useState("");
  const [detailAccount, setDetailAccount] = useState<AccountView | null>(null);
  const [skinTarget, setSkinTarget] = useState<AccountView | null>(null);
  const [deleteTarget, setDeleteTarget] = useState<AccountView | null>(null);
  const [deleting, setDeleting] = useState(false);
  const [showOfflineModal, setShowOfflineModal] = useState(false);
  const [offlineNick, setOfflineNick] = useState("");
  const [offlineErr, setOfflineErr] = useState("");
  const [addingOffline, setAddingOffline] = useState(false);

  const detailTrans = useOverlayTransition(Boolean(detailAccount), 220);
  const deleteTrans = useOverlayTransition(Boolean(deleteTarget), 220);
  const offlineTrans = useOverlayTransition(showOfflineModal, 220);
  useEscapeKey(Boolean(detailAccount), () => setDetailAccount(null));
  useEscapeKey(Boolean(deleteTarget) && !deleting, () => setDeleteTarget(null));
  useEscapeKey(showOfflineModal && !addingOffline, () => setShowOfflineModal(false));

  const handleCreateOffline = async (e: React.FormEvent) => {
    e.preventDefault();
    const nick = offlineNick.trim();
    if (!nick) {
      setOfflineErr("Введите никнейм");
      return;
    }
    if (nick.length < 3 || nick.length > 16) {
      setOfflineErr("Никнейм должен содержать от 3 до 16 символов");
      return;
    }
    if (!/^[a-zA-Z0-9_]+$/.test(nick)) {
      setOfflineErr("Разрешены только латинские буквы, цифры и символ подчеркивания");
      return;
    }
    setAddingOffline(true);
    setOfflineErr("");
    try {
      await onAddOffline(nick);
      setShowOfflineModal(false);
      setOfflineNick("");
    } catch (err) {
      setOfflineErr(String((err as Error).message || err));
    } finally {
      setAddingOffline(false);
    }
  };

  const handleLoginClick = async (reauthId = "") => {
    try {
      await onStartLogin(reauthId);
    } catch (err) {
      onToast(String((err as Error).message || err), "err");
    }
  };

  const runAction = async (id: string, fn: () => Promise<unknown>) => {
    setBusyId(id);
    try {
      await fn();
    } catch (err) {
      onToast(String((err as Error).message || err), "err");
    } finally {
      setBusyId("");
    }
  };

  const handleConfirmDelete = async () => {
    if (!deleteTarget) return;
    setDeleting(true);
    try {
      await onDelete(deleteTarget.id);
      if (detailAccount?.id === deleteTarget.id) setDetailAccount(null);
      setDeleteTarget(null);
    } catch (err) {
      onToast(String((err as Error).message || err), "err");
    } finally {
      setDeleting(false);
    }
  };

  return (
    <div className="mh-page mh-page-enter">
      <header className="mh-page-head-row">
        <div>
          <h1 className="mh-page-title">Управление аккаунтами</h1>
          <p className="mh-page-subtitle">
            Аккаунты Microsoft и локальные офлайн-аккаунты для игры.
          </p>
        </div>
        <div style={{ display: "flex", gap: 8, alignItems: "center" }}>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={() => {
              setOfflineNick("");
              setOfflineErr("");
              setShowOfflineModal(true);
            }}
          >
            + Офлайн-аккаунт
          </button>
          <button
            type="button"
            className="btn btn-primary btn-sm"
            disabled={flow.active}
            title="Открыть вход Microsoft в системном браузере"
            onClick={() => void handleLoginClick("")}
          >
            Войти через Microsoft
          </button>
        </div>
      </header>

      {/* Live OAuth / Xbox / Minecraft Auth Progress */}
      {flow.active && (
        <section className="mh-panel mh-install-live-panel" role="status">
          <div className="mh-panel-head">
            <div>
              <h2>{flowStageTitle(flow.stage)}</h2>
              <p className="mh-muted-meta" style={{ margin: "2px 0 0" }}>{flow.message}</p>
            </div>
            <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
              {flow.auth_url && (
                <>
                  <button
                    type="button"
                    className="btn btn-secondary btn-xs"
                    onClick={() => {
                      void desktopBridge.openExternalUrl(flow.auth_url!).catch((err) => {
                        onToast(`Не удалось открыть браузер: ${String((err as Error).message || err)}`, "err");
                      });
                    }}
                  >
                    Открыть браузер
                  </button>
                  <button
                    type="button"
                    className="btn btn-ghost btn-xs"
                    onClick={() => {
                      void navigator.clipboard?.writeText(flow.auth_url!).then(
                        () => onToast("Ссылка авторизации скопирована", "ok"),
                        () => onToast("Не удалось скопировать ссылку", "err")
                      );
                    }}
                  >
                    Копировать ссылку
                  </button>
                </>
              )}
              <button type="button" className="btn btn-danger-ghost btn-xs" onClick={() => void onCancelLogin()}>
                Отменить вход
              </button>
            </div>
          </div>
          <div className="mh-dl-progress-track">
            <div className="mh-dl-progress-fill is-indeterminate" style={{ width: "35%" }} />
          </div>
        </section>
      )}

      {!flow.active && flow.stage === "error" && flow.error && (
        <div className="mh-dl-error-note" role="alert">
          <strong>Ошибка авторизации:</strong> {flow.error}
        </div>
      )}

      {/* Active Account Reauth Banner */}
      {activeAccount && activeAccount.auth_status === "reauth_required" && (
        <div className="mh-inst-issue is-err" role="alert">
          <strong>Активный аккаунт «{accountDisplayName(activeAccount)}» требует повторного входа:</strong>{" "}
          <span>{activeAccount.last_error || "сессия истекла или токены недоступны."}</span>{" "}
            <button
              type="button"
              className="btn btn-primary btn-xs"
              style={{ marginLeft: 8 }}
              disabled={flow.active}
              onClick={() => handleLoginClick(activeAccount.id)}
            >
              Войти снова
            </button>
          </div>
        )}

        {loading && accounts.length === 0 ? (
          <div className="empty-state">Загрузка учётных записей…</div>
        ) : accounts.length === 0 ? (
          <section className="mh-panel mh-placeholder-card">
            <h2>Нет добавленных аккаунтов</h2>
            <p className="mh-placeholder-text">
              Вы можете создать локальный офлайн-аккаунт с любым ником для быстрой игры без интернета, либо войти через официальный аккаунт Microsoft для игры с подтверждённой лицензией.
            </p>
            <div className="mh-placeholder-actions">
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={() => {
                  setOfflineNick("");
                  setOfflineErr("");
                  setShowOfflineModal(true);
                }}
              >
                + Создать офлайн-аккаунт
              </button>
              <button
                type="button"
                className="btn btn-primary btn-sm"
                disabled={flow.active}
                onClick={() => handleLoginClick("")}
              >
                Войти через Microsoft
              </button>
            </div>
          </section>
      ) : (
        <div className="mh-acct-grid">
          {accounts.map((acc) => {
            const isOffline = acc.account_type === "offline";
            const name = accountDisplayName(acc);
            const authMeta = authStatusMeta(acc.auth_status, acc.license_status);
            const licMeta = licenseStatusMeta(acc.license_status);
            const isBusy = busyId === acc.id;

            return (
              <article key={acc.id} className={`mh-acct-card${acc.active ? " is-active-acct" : ""}`}>
                <div className="mh-acct-head">
                  <SkinHead skinUrl={acc.skin_url} username={name} size={44} />
                  <div className="mh-acct-head-info">
                    <div className="mh-acct-name-row">
                      <h3 className="mh-acct-name" title={name}>{name}</h3>
                      {acc.active && <span className="pill pill-installed">Активный</span>}
                    </div>
                    <div className="mh-acct-badges">
                      {isOffline ? (
                        <span className="mh-dl-status-pill is-ok">Офлайн-режим</span>
                      ) : (
                        <>
                          <span className={`mh-dl-status-pill ${authMeta.cls}`}>{authMeta.label}</span>
                          <span className={`mh-dl-status-pill ${licMeta.cls}`}>{licMeta.label}</span>
                        </>
                      )}
                    </div>
                  </div>
                </div>

                <div className="mh-acct-meta mono-num">
                  <div>UUID: {acc.formatted_uuid || (isOffline ? "Офлайн UUID" : "Профиль Minecraft не создан")}</div>
                  <div>{isOffline ? "Локальный профиль (без интернета)" : `Последняя проверка: ${fmtUnix(acc.last_license_check_at || acc.last_auth_at)}`}</div>
                </div>

                {acc.last_error && (
                  <div className="mh-dl-error-note">{acc.last_error}</div>
                )}

                <div className="mh-acct-actions">
                  {!acc.active && (
                    <button
                      type="button"
                      className="btn btn-primary btn-xs"
                      disabled={isBusy}
                      onClick={() => void runAction(acc.id, () => onSetActive(acc.id))}
                    >
                      Сделать активным
                    </button>
                  )}
                  <button type="button" className="btn btn-ghost btn-xs" onClick={() => setSkinTarget(acc)}>
                    Скин / Модель
                  </button>
                  <button type="button" className="btn btn-ghost btn-xs" onClick={() => setDetailAccount(acc)}>
                    Подробнее
                  </button>
                  {!isOffline && (
                    <>
                      <button
                        type="button"
                        className={`btn btn-ghost btn-xs${isBusy ? " is-loading" : ""}`}
                        disabled={isBusy || acc.auth_status === "unauthorized" || acc.auth_status === "reauth_required"}
                        onClick={() => void runAction(acc.id, () => onCheckLicense(acc.id))}
                      >
                        Проверить лицензию
                      </button>
                      <button
                        type="button"
                        className="btn btn-ghost btn-xs"
                        disabled={isBusy || flow.active}
                        onClick={() => {
                          if (acc.auth_status === "reauth_required" || acc.auth_status === "unauthorized" || !acc.has_vault_secret) {
                            void handleLoginClick(acc.id);
                          } else {
                            void runAction(acc.id, () => onRefreshAuth(acc.id, true));
                          }
                        }}
                      >
                        {acc.auth_status === "reauth_required" || acc.auth_status === "unauthorized" ? "Войти снова" : "Обновить"}
                      </button>
                      {acc.auth_status !== "unauthorized" && (
                        <button
                          type="button"
                          className="btn btn-ghost btn-xs"
                          disabled={isBusy}
                          onClick={() => void runAction(acc.id, () => onSignOut(acc.id))}
                        >
                          Выйти
                        </button>
                      )}
                    </>
                  )}
                  <button
                    type="button"
                    className="btn btn-danger-ghost btn-xs"
                    disabled={isBusy}
                    onClick={() => setDeleteTarget(acc)}
                  >
                    Удалить
                  </button>
                </div>
              </article>
            );
          })}
        </div>
      )}

      {/* Account Details Modal (strictly token-free!) */}
      {detailTrans.mounted && detailAccount && (
        <div
          className={`modal-backdrop overlay-animated${detailTrans.shown ? " is-open" : ""}`}
          role="dialog"
          aria-modal="true"
          onClick={() => setDetailAccount(null)}
        >
          <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div style={{ display: "flex", gap: "12px", alignItems: "center" }}>
                <SkinHead
                  skinUrl={detailAccount.skin_url}
                  username={detailAccount.minecraft_username || "Player"}
                  size={48}
                />
                <div>
                  <h2 className="modal-title">{accountDisplayName(detailAccount)}</h2>
                  <p className="modal-subtitle">Подробная информация об учётной записи (секретные токены скрыты в зашифрованном хранилище).</p>
                </div>
              </div>
              <button type="button" className="icon-close" aria-label="Закрыть" onClick={() => setDetailAccount(null)}>
                <CloseIcon size={15} />
              </button>
            </div>
            <div className="modal-body mh-form-stack">
              <div className="mh-data-dir-box mono-num">
                <div><strong>Никнейм:</strong> {detailAccount.minecraft_username || "—"}</div>
                <div><strong>UUID:</strong> {detailAccount.formatted_uuid || "—"}</div>
                {detailAccount.xbox_gamertag && <div><strong>Xbox Gamertag:</strong> {detailAccount.xbox_gamertag}</div>}
                <div><strong>Статус:</strong> {authStatusMeta(detailAccount.auth_status, detailAccount.license_status).label}</div>
                <div><strong>Лицензия:</strong> {licenseStatusMeta(detailAccount.license_status).label}</div>
                <div><strong>Последний вход:</strong> {fmtUnix(detailAccount.last_auth_at)}</div>
              </div>
              <div className="mh-modal-actions-end">
                <button type="button" className="btn btn-primary btn-sm" onClick={() => setDetailAccount(null)}>
                  Закрыть
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Delete Account Confirmation Modal */}
      {deleteTrans.mounted && deleteTarget && (
        <div
          className={`modal-backdrop overlay-animated${deleteTrans.shown ? " is-open" : ""}`}
          role="dialog"
          aria-modal="true"
          onClick={() => setDeleteTarget(null)}
        >
          <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div>
                <h2 className="modal-title">Удалить аккаунт «{accountDisplayName(deleteTarget)}» из ModHub?</h2>
                <p className="modal-subtitle">Локальная запись и зашифрованные токены будут полностью удалены из ModHub.</p>
              </div>
              <button type="button" className="icon-close" aria-label="Закрыть" onClick={() => setDeleteTarget(null)}>
                <CloseIcon size={15} />
              </button>
            </div>
            <div className="modal-body mh-form-stack">
              <div className="mh-info-callout">
                Эта операция удаляет аккаунт <strong>только из ModHub</strong> — ваш Microsoft Account и учётная запись Minecraft не удаляются. Игровые профили, установленные версии Minecraft, моды и сохранения останутся нетронутыми.
              </div>
              <div className="mh-modal-actions-end">
                <button type="button" className="btn btn-ghost btn-sm" disabled={deleting} onClick={() => setDeleteTarget(null)}>
                  Отмена
                </button>
                <button
                  type="button"
                  className={`btn btn-danger-ghost btn-sm${deleting ? " is-loading" : ""}`}
                  disabled={deleting}
                  onClick={handleConfirmDelete}
                >
                  {deleting ? "Удаление…" : "Удалить из ModHub"}
                </button>
              </div>
            </div>
          </div>
        </div>
      )}

      {/* Offline Account Creation Modal */}
      {offlineTrans.mounted && (
        <div
          className={`modal-backdrop overlay-animated${offlineTrans.shown ? " is-open" : ""}`}
          role="dialog"
          aria-modal="true"
          onClick={() => !addingOffline && setShowOfflineModal(false)}
        >
          <div className="modal-window modal-sm" onClick={(e) => e.stopPropagation()}>
            <div className="modal-header">
              <div>
                <h2 className="modal-title">Новый офлайн-аккаунт</h2>
                <p className="modal-subtitle">Создание локального профиля для свободной игры без учётной записи Microsoft.</p>
              </div>
              <button
                type="button"
                className="icon-close"
                aria-label="Закрыть"
                disabled={addingOffline}
                onClick={() => setShowOfflineModal(false)}
              >
                <CloseIcon size={15} />
              </button>
            </div>
            <form onSubmit={handleCreateOffline}>
              <div className="modal-body mh-form-stack">
                <div className="mh-form-field">
                  <label htmlFor="offlineNick">Игровой никнейм</label>
                  <input
                    id="offlineNick"
                    type="text"
                    className="text-input"
                    placeholder="Например: Steve или Player_1"
                    value={offlineNick}
                    autoFocus
                    maxLength={16}
                    onChange={(e) => {
                      setOfflineNick(e.target.value);
                      setOfflineErr("");
                    }}
                  />
                  <p className="mh-muted-meta" style={{ marginTop: 6, fontSize: "0.8rem" }}>
                    От 3 до 16 символов: латиница (A-Z, a-z), цифры (0-9) и знак подчёркивания (_).
                  </p>
                </div>
                {offlineErr && (
                  <div className="mh-dl-error-note" role="alert">
                    {offlineErr}
                  </div>
                )}
                <div className="mh-modal-actions-end">
                  <button
                    type="button"
                    className="btn btn-ghost btn-sm"
                    disabled={addingOffline}
                    onClick={() => setShowOfflineModal(false)}
                  >
                    Отмена
                  </button>
                  <button
                    type="submit"
                    className={`btn btn-primary btn-sm${addingOffline ? " is-loading" : ""}`}
                    disabled={addingOffline || !offlineNick.trim()}
                  >
                    Создать и выбрать
                  </button>
                </div>
              </div>
            </form>
          </div>
        </div>
      )}

      {/* Skin & Model Manager Modal */}
      {skinTarget && (
        <SkinModal
          account={skinTarget}
          onClose={() => setSkinTarget(null)}
          onSave={async (skinUrl, variant, capeUrl, capeAlias) => {
            const updated = await api.updateAccountSkin(skinTarget.id, skinUrl, variant, capeUrl, capeAlias);
            if (detailAccount?.id === skinTarget.id) setDetailAccount(updated);
            if (skinTarget.active) {
              await onSetActive(skinTarget.id);
            }
          }}
          onToast={onToast}
        />
      )}
    </div>
  );
};
