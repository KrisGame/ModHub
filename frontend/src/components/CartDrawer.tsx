import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { api } from "../api";
import { useOverlayTransition, useScrollFade } from "../hooks";
import { triggerBlobDownload } from "../hooks/usePackCart";
import { CloseIcon } from "../icons";
import { desktopBridge } from "../services/bridge";
import type { CartItem, SavedPack } from "../types";
import { cachedIconUrl, detectCartConflicts, fallbackIconDataUri, formatBytes, isImageUrlLoaded, markImageUrlLoaded } from "../utils";

interface CartDrawerProps {
  open: boolean;
  cart: CartItem[];
  savedPacks: SavedPack[];
  mcVersion: string;
  loader: string;
  onClose: () => void;
  onRemoveAt: (idx: number) => void;
  onRemoveByKey?: (folder: string, filename: string) => void;
  onClearCart: () => void;
  onLoadPack: (pack: SavedPack) => void;
  onPacksUpdated: (packs: SavedPack[]) => void;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

const FOLDER_ORDER = ["mods", "resourcepacks", "shaderpacks", "datapacks"];

const FOLDER_LABELS: Record<string, string> = {
  mods: "Моды",
  resourcepacks: "Ресурспаки",
  shaderpacks: "Шейдеры",
  datapacks: "Датапаки",
};

export const CartDrawer: React.FC<CartDrawerProps> = ({
  open,
  cart,
  savedPacks,
  mcVersion,
  loader,
  onClose,
  onRemoveAt,
  onRemoveByKey,
  onClearCart,
  onLoadPack,
  onPacksUpdated,
  onToast,
}) => {
  const { mounted, shown } = useOverlayTransition(open, 260);
  const [packName, setPackName] = useState("My-Modpack");
  const [zipPct, setZipPct] = useState(0);
  const [exporting, setExporting] = useState(false);
  const [zipFb, setZipFb] = useState<"" | "success" | "error">("");
  const [savingPack, setSavingPack] = useState(false);
  const [saveFb, setSaveFb] = useState<"" | "success" | "error">("");
  const [removingKeys, setRemovingKeys] = useState<Set<string>>(() => new Set());
  const [loadedIcons, setLoadedIcons] = useState<Set<string>>(() => new Set());
  const bodyRef = useRef<HTMLDivElement | null>(null);
  const itemRefs = useRef<Map<string, HTMLDivElement>>(new Map());
  const prevTopsRef = useRef<Map<string, number>>(new Map());

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [open, onClose]);

  useScrollFade(bodyRef, [mounted, cart.length, savedPacks.length, removingKeys.size]);

  const conflicts = useMemo(() => detectCartConflicts(cart), [cart]);

  const groupedFolders = useMemo(() => {
    const map = new Map<string, Array<{ item: CartItem; index: number; key: string }>>();
    cart.forEach((item, index) => {
      const f = item.folder || "mods";
      const key = `${f}:${item.filename}:${index}`;
      const arr = map.get(f);
      if (arr) arr.push({ item, index, key });
      else map.set(f, [{ item, index, key }]);
    });
    const keys = Array.from(map.keys()).sort((a, b) => {
      const ia = FOLDER_ORDER.indexOf(a);
      const ib = FOLDER_ORDER.indexOf(b);
      return (ia === -1 ? 99 : ia) - (ib === -1 ? 99 : ib);
    });
    return keys.map((folder) => ({
      folder,
      label: FOLDER_LABELS[folder] || folder,
      entries: map.get(folder)!,
    }));
  }, [cart]);

  useLayoutEffect(() => {
    const prevTops = prevTopsRef.current;
    const nextTops = new Map<string, number>();

    itemRefs.current.forEach((el, k) => {
      if (!el.isConnected) return;
      const top = el.getBoundingClientRect().top;
      nextTops.set(k, top);
      const oldTop = prevTops.get(k);
      if (oldTop !== undefined && Math.abs(oldTop - top) > 1) {
        const dy = oldTop - top;
        el.style.transition = "none";
        el.style.transform = `translate3d(0, ${dy.toFixed(1)}px, 0)`;
        requestAnimationFrame(() => {
          el.style.transition = "transform 0.24s cubic-bezier(0.16, 1, 0.3, 1), border-color 0.14s ease, background-color 0.14s ease";
          el.style.transform = "translate3d(0, 0, 0)";
        });
      }
    });

    prevTopsRef.current = nextTops;
  }, [cart.length]);

  const handleAnimatedRemove = (idx: number, folder: string, filename: string, key: string) => {
    if (removingKeys.has(key)) return;
    setRemovingKeys((prev) => {
      const next = new Set(prev);
      next.add(key);
      return next;
    });
    window.setTimeout(() => {
      setRemovingKeys((prev) => {
        const next = new Set(prev);
        next.delete(key);
        return next;
      });
      if (onRemoveByKey) onRemoveByKey(folder, filename);
      else onRemoveAt(idx);
    }, 210);
  };

  const handleSavePack = async () => {
    if (!cart.length || savingPack) return;
    setSavingPack(true);
    setSaveFb("");
    try {
      const packs = await api.savePack({
        id: `pack_${Date.now()}`,
        name: packName.trim() || "My-Modpack",
        mc_version: mcVersion,
        loader,
        created_at: new Date().toISOString(),
        items: cart,
      });
      onPacksUpdated(packs);
      setSaveFb("success");
      window.setTimeout(() => setSaveFb(""), 1600);
      onToast("Сборка сохранена в data/packs.json", "ok");
    } catch (err) {
      setSaveFb("error");
      window.setTimeout(() => setSaveFb(""), 1800);
      onToast(`Ошибка сохранения: ${(err as Error).message}`, "err");
    } finally {
      setSavingPack(false);
    }
  };

  const handleDeletePack = async (id: string) => {
    try {
      const packs = await api.deletePack(id);
      onPacksUpdated(packs);
      onToast("Сборка удалена", "info");
    } catch (err) {
      onToast(String(err), "err");
    }
  };

  const handleDownloadZip = async () => {
    if (!cart.length || exporting) return;
    setExporting(true);
    setZipPct(8);
    const targetRef = { current: 22 };
    const shownRef = { current: 8 };
    const animTimer = window.setInterval(() => {
      if (targetRef.current < 82) {
        targetRef.current = Math.min(82, targetRef.current + 0.35);
      }
      shownRef.current += (targetRef.current - shownRef.current) * 0.2;
      setZipPct(Math.min(99, Math.round(shownRef.current)));
    }, 40);

    try {
      const cleanName = packName.trim() || "My-Modpack";
      const nativeRes = await desktopBridge.savePackZipNative(cleanName, cart);
      if (nativeRes) {
        if (nativeRes.cancelled) {
          return;
        }
        targetRef.current = 100;
        shownRef.current = 100;
        setZipPct(100);
        if (nativeRes.status === "partial" || nativeRes.failed > 0) {
          setZipFb("error");
          window.setTimeout(() => setZipFb(""), 2600);
          const detail = (nativeRes.failed_reasons || []).slice(0, 2).join("; ");
          onToast(
            `ZIP сохранён частично (${nativeRes.downloaded} из ${nativeRes.total}). Ошибок: ${nativeRes.failed}${detail ? ` — ${detail}` : ""}`,
            "err"
          );
        } else {
          setZipFb("success");
          window.setTimeout(() => setZipFb(""), 1800);
          onToast(`ZIP-сборка сохранена (${nativeRes.downloaded} файлов): ${nativeRes.path}`, "ok");
        }
        return;
      }

      const rep = await api.downloadPackZipWithReport(cleanName, cart, (p) => {
        targetRef.current = Math.max(targetRef.current, p);
      });
      targetRef.current = 100;
      shownRef.current = 100;
      setZipPct(100);
      const safeZipName = `${cleanName.replace(/[^a-zA-Z0-9_-]+/g, "-") || "My-Modpack"}.zip`;
      triggerBlobDownload(rep.blob, safeZipName);
      if (rep.status === "partial" || rep.failed > 0) {
        setZipFb("error");
        window.setTimeout(() => setZipFb(""), 2600);
        const detail = rep.failedReasons.slice(0, 2).join("; ");
        onToast(
          `ZIP скачан частично (${rep.downloaded} из ${rep.total}). Ошибок: ${rep.failed}${detail ? ` — ${detail}` : ""}`,
          "err"
        );
      } else {
        setZipFb("success");
        window.setTimeout(() => setZipFb(""), 1800);
        onToast(`Готовая .ZIP сборка скачана (${rep.downloaded} файлов)`, "ok");
      }
    } catch (err) {
      setZipFb("error");
      window.setTimeout(() => setZipFb(""), 2000);
      onToast(`Ошибка ZIP: ${(err as Error).message}`, "err");
    } finally {
      window.clearInterval(animTimer);
      window.setTimeout(() => {
        setExporting(false);
        setZipPct(0);
      }, 380);
    }
  };

  if (!mounted) return null;

  return (
    <div className={`drawer-backdrop overlay-animated${shown ? " is-open" : ""}`} aria-hidden={!shown} onClick={onClose}>
      <aside className="drawer-panel" onClick={(e) => e.stopPropagation()}>
        <div className="drawer-header">
          <div className="drawer-header-text">
            <h2>Сборка</h2>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <div className={`reveal-slot reveal-slot-flush drawer-slot${conflicts.length > 0 ? " is-shown" : ""}`} aria-hidden={conflicts.length === 0}>
          <div className="reveal-inner">
            <div className="cart-conflict-banner" role="status">
              <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
              <div className="cart-conflict-lines">
                <strong>Возможный конфликт в сборке</strong>
                <div>{conflicts.join(" • ")}</div>
              </div>
            </div>
          </div>
        </div>

        <div className="drawer-pack-bar">
          <input
            type="text"
            className="text-input"
            placeholder="Название сборки (например: NeoForge 1.21.1)"
            value={packName}
            onChange={(e) => setPackName(e.target.value)}
          />
          <button
            type="button"
            className={`btn btn-ghost btn-sm${savingPack ? " is-loading" : ""}${saveFb ? ` is-${saveFb}` : ""}`}
            disabled={!cart.length || savingPack}
            aria-busy={savingPack}
            onClick={handleSavePack}
          >
            {savingPack ? (
              <><span className="spinner spinner-xs" aria-hidden="true" /><span>Сохранение…</span></>
            ) : saveFb === "success" ? (
              "Сохранено ✓"
            ) : saveFb === "error" ? (
              "Ошибка"
            ) : (
              "Сохранить"
            )}
          </button>
        </div>

        {savedPacks.length > 0 && (
          <div className="drawer-presets">
            {savedPacks.map((p) => (
              <div key={p.id} className="preset-chip">
                <button type="button" className="preset-load-btn" onClick={() => onLoadPack(p)} title={`Загрузить «${p.name}»`}>
                  <span className="preset-load-name">{p.name}</span>
                  <span className="preset-load-cnt mono-num">{p.items.length}</span>
                </button>
                <button type="button" className="preset-del-btn" aria-label="Удалить сборку" onClick={() => handleDeletePack(p.id)}>
                  <CloseIcon size={11} />
                </button>
              </div>
            ))}
          </div>
        )}

        <div className="drawer-body" ref={bodyRef}>
          {!cart.length ? (
            <div className="empty-state" style={{ padding: "38px 14px" }}>
              <svg className="empty-state-svg" viewBox="0 0 48 48" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                <path d="M12 10L8 16v22a4 4 0 0 0 4 4h24a4 4 0 0 0 4-4V16l-4-6H12z" />
                <path d="M8 16h32" />
                <path d="M31 23a7 7 0 0 1-14 0" />
              </svg>
              <h3>Сборка пуста</h3>
              <p>Выберите версию и загрузчик, затем нажмите «+ В сборку» на карточке мода.</p>
            </div>
          ) : (
            groupedFolders.map(({ folder, label, entries }) => (
              <div key={folder} className="cart-folder-group">
                <div className="cart-folder-title">
                  <span className="cart-folder-title-main">
                    <span className={`cart-comp-dot seg-${folder}`} />
                    <span>{label}</span>
                  </span>
                </div>
                {entries.map(({ item: c, index: idx, key }) => {
                  const fb = fallbackIconDataUri(c.title);
                  const isAutoDep = Boolean(c.auto_dep_for && c.auto_dep_for.length > 0);
                  const isRemoving = removingKeys.has(key);
                  return (
                    <div
                      key={key}
                      ref={(el) => {
                        if (el) itemRefs.current.set(key, el);
                        else itemRefs.current.delete(key);
                      }}
                      className={`cart-item${isAutoDep ? " is-auto-dep" : ""}${isRemoving ? " is-removing" : ""}`}
                    >
                      {(() => {
                        const icoUrl = c.icon_url ? cachedIconUrl(c.icon_url, 72) : fb;
                        const icoReady = isImageUrlLoaded(icoUrl) || loadedIcons.has(icoUrl);
                        const markReady = () => {
                          markImageUrlLoaded(icoUrl);
                          if (!loadedIcons.has(icoUrl)) {
                            setLoadedIcons((prev) => {
                              const next = new Set(prev);
                              next.add(icoUrl);
                              return next;
                            });
                          }
                        };
                        return (
                          <img
                            width={36}
                            height={36}
                            className={`cart-item-icon icon-smooth-img${icoReady ? " is-loaded" : ""}`}
                            src={icoUrl}
                            alt=""
                            onLoad={markReady}
                            ref={(el) => {
                              if (el && el.complete && el.naturalWidth > 0 && !icoReady) markReady();
                            }}
                            onError={(e) => {
                              e.currentTarget.src = fb;
                              markReady();
                            }}
                          />
                        );
                      })()}
                      <div className="cart-item-info">
                        <div className="cart-item-title-row">
                          <span className="cart-item-title" title={c.title}>{c.title}</span>
                          {isAutoDep && (
                            <span className="cart-dep-tag" title="Добавлено автоматически как обязательная зависимость">
                              Завис.
                            </span>
                          )}
                        </div>
                        <div className="cart-item-sub" title={`${c.folder}/${c.filename}`}>
                          {c.filename} • {formatBytes(c.file_size)}
                        </div>
                      </div>
                      <button
                        type="button"
                        className="icon-close cart-item-del"
                        aria-label="Удалить"
                        onClick={() => handleAnimatedRemove(idx, c.folder || "mods", c.filename, key)}
                      >
                        <CloseIcon size={13} />
                      </button>
                    </div>
                  );
                })}
              </div>
            ))
          )}
        </div>

        <div className="drawer-footer">
          <button
            type="button"
            className={`btn btn-primary btn-block btn-zip-progress${exporting ? " is-downloading is-loading" : ""}${zipFb ? ` is-${zipFb}` : ""}`}
            disabled={!cart.length || exporting}
            aria-busy={exporting}
            onClick={handleDownloadZip}
          >
            <span className="btn-zip-fill" style={{ width: `${zipPct}%` }} aria-hidden="true" />
            <span className="btn-zip-label">
              {exporting ? (
                <span className="spinner spinner-xs" aria-hidden="true" />
              ) : (
                <svg className="ui-ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              )}
              <span>
                {exporting
                  ? `Сборка .ZIP (${zipPct}%)…`
                  : zipFb === "success"
                    ? "Сборка скачана ✓"
                    : zipFb === "error"
                      ? "Ошибка сборки — повторить"
                      : "Скачать сборку с файлами (.ZIP)"}
              </span>
            </span>
          </button>
          <div className="drawer-footer-row">
            <button type="button" className="btn btn-danger-ghost btn-sm btn-block" disabled={!cart.length} onClick={onClearCart}>
              <span>Очистить сборку</span>
            </button>
          </div>
        </div>
      </aside>
    </div>
  );
};
