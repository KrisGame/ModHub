import React, { useState } from "react";
import { SkinHead } from "./SkinHead";
import { CloseIcon } from "../icons";
import type { AccountView } from "../types";

interface SkinModalProps {
  account: AccountView;
  onClose: () => void;
  onSave: (skinUrl: string, variant: string, capeUrl: string, capeAlias: string) => Promise<void>;
  onToast: (text: string, kind?: "info" | "ok" | "err") => void;
}

interface SkinPreset {
  id: string;
  name: string;
  variant: "CLASSIC" | "SLIM";
  url: string;
}

const PRESET_SKINS: SkinPreset[] = [
  {
    id: "steve",
    name: "Стив (Классика)",
    variant: "CLASSIC",
    url: "https://textures.minecraft.net/texture/1a2fcdd12d315e1286b72a6b47c87c7a369e59891823eb5aa648be71ec5aa2",
  },
  {
    id: "alex",
    name: "Алекс (Тонкие руки)",
    variant: "SLIM",
    url: "https://textures.minecraft.net/texture/a35f29c4ed23807c96366f076c8c4da047eb68b3ee581b7a66c4c9e88d447477",
  },
  {
    id: "cyberpunk",
    name: "Киберпанк",
    variant: "CLASSIC",
    url: "https://textures.minecraft.net/texture/e558fc9c0eb1a7cf7350718d7f7ddab0b33c06d812ffea2d79a296aa700cf18a",
  },
  {
    id: "knight",
    name: "Рыцарь",
    variant: "CLASSIC",
    url: "https://textures.minecraft.net/texture/5f7956b6279f7ca457d9b935d88667c1e3cd29bb6bc2cf4cae8f764a883713",
  },
];

const PRESET_CAPES = [
  { id: "none", name: "Без плаща", url: "", alias: "" },
  {
    id: "migrator",
    name: "Migrator Cape",
    url: "https://textures.minecraft.net/texture/2340c0e03dd66dd11d17a8ab13e4421635e85d84533be9d16a53a79d1b824eb",
    alias: "Migrator",
  },
  {
    id: "vanilla",
    name: "Vanilla Cape",
    url: "https://textures.minecraft.net/texture/02f0a1c1256ab57a7da9310cae7f1b6ee97939b03be3a7f88414be3985a539eb",
    alias: "Vanilla",
  },
];

export const SkinModal: React.FC<SkinModalProps> = ({
  account,
  onClose,
  onSave,
  onToast,
}) => {
  const [skinUrl, setSkinUrl] = useState(account.skin_url || PRESET_SKINS[0].url);
  const [variant, setVariant] = useState<"CLASSIC" | "SLIM">((account.skin_variant === "SLIM" ? "SLIM" : "CLASSIC"));
  const [capeUrl, setCapeUrl] = useState(account.cape_url || "");
  const [capeAlias, setCapeAlias] = useState(account.cape_alias || "");
  const [saving, setSaving] = useState(false);

  const handleFileUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    if (!file.type.includes("png")) {
      onToast("Скин должен быть файлом в формате PNG", "err");
      return;
    }
    if (file.size > 2 * 1024 * 1024) {
      onToast("Размер файла скина не должен превышать 2 МБ", "err");
      return;
    }

    const reader = new FileReader();
    reader.onload = (event) => {
      const dataUrl = event.target?.result as string;
      if (!dataUrl) return;

      const img = new Image();
      img.onload = () => {
        if ((img.width === 64 && img.height === 64) || (img.width === 64 && img.height === 32)) {
          setSkinUrl(dataUrl);
          onToast(`Скин «${file.name}» (${img.width}x${img.height}) успешно загружен`, "ok");
        } else {
          onToast(`Нестандартное разрешение скина: ${img.width}x${img.height}. Рекомендуется 64x64 или 64x32.`, "err");
        }
      };
      img.src = dataUrl;
    };
    reader.readAsDataURL(file);
  };

  const handleCapeUpload = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    if (!file) return;

    if (!file.type.includes("png")) {
      onToast("Плащ должен быть в формате PNG", "err");
      return;
    }

    const reader = new FileReader();
    reader.onload = (event) => {
      const dataUrl = event.target?.result as string;
      if (!dataUrl) return;
      setCapeUrl(dataUrl);
      setCapeAlias(file.name.replace(/\.png$/i, ""));
      onToast(`Плащ «${file.name}» загружен`, "ok");
    };
    reader.readAsDataURL(file);
  };

  const handleApply = async () => {
    setSaving(true);
    try {
      await onSave(skinUrl, variant, capeUrl, capeAlias);
      onToast("Внешний вид аккаунта успешно обновлён", "ok");
      onClose();
    } catch (err) {
      onToast(err instanceof Error ? err.message : String(err), "err");
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="modal-backdrop overlay-animated is-open" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="modal-window modal-md" onClick={(e) => e.stopPropagation()}>
        <div className="modal-header">
          <div>
            <h2 className="modal-title">Кастомизация скина и плаща</h2>
            <p className="modal-subtitle">
              Аккаунт: <strong>{account.minecraft_username || account.xbox_gamertag || account.id}</strong>
              {account.account_type === "offline" ? " (Офлайн-режим)" : " (Microsoft)"}
            </p>
          </div>
          <button type="button" className="icon-close" aria-label="Закрыть" onClick={onClose}>
            <CloseIcon size={15} />
          </button>
        </div>

        <div className="modal-body mh-form-stack">
          {/* Preview & Variant */}
          <div style={{ display: "flex", gap: "20px", alignItems: "center", background: "rgba(255,255,255,0.03)", padding: "16px", borderRadius: "8px", border: "1px solid rgba(255,255,255,0.08)" }}>
            <SkinHead
              skinUrl={skinUrl}
              username={account.minecraft_username || "Player"}
              size={72}
            />
            <div style={{ flex: 1 }}>
              <div style={{ fontSize: "14px", fontWeight: 600, marginBottom: "4px" }}>
                {account.minecraft_username || "Player"}
              </div>
              <div className="mono-num" style={{ fontSize: "12px", color: "var(--text-muted, #94a3b8)", marginBottom: "10px" }}>
                Модель: <strong>{variant === "CLASSIC" ? "Classic (Стив 4px)" : "Slim (Алекс 3px)"}</strong>
                {capeAlias && ` • Плащ: ${capeAlias}`}
              </div>
              <div style={{ display: "flex", gap: "8px" }}>
                <button
                  type="button"
                  className={`btn btn-xs ${variant === "CLASSIC" ? "btn-primary" : "btn-ghost"}`}
                  onClick={() => setVariant("CLASSIC")}
                >
                  Classic (4px)
                </button>
                <button
                  type="button"
                  className={`btn btn-xs ${variant === "SLIM" ? "btn-primary" : "btn-ghost"}`}
                  onClick={() => setVariant("SLIM")}
                >
                  Slim (3px)
                </button>
              </div>
            </div>
          </div>

          {/* Skin Presets */}
          <div className="mh-form-field">
            <label>Готовые скины</label>
            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(130px, 1fr))", gap: "8px" }}>
              {PRESET_SKINS.map((p) => (
                <button
                  key={p.id}
                  type="button"
                  className={`btn btn-xs ${skinUrl === p.url ? "btn-primary" : "btn-ghost"}`}
                  style={{ display: "flex", alignItems: "center", gap: "6px", justifyContent: "flex-start", padding: "6px 8px" }}
                  onClick={() => {
                    setSkinUrl(p.url);
                    setVariant(p.variant);
                  }}
                >
                  <SkinHead skinUrl={p.url} size={20} />
                  <span>{p.name}</span>
                </button>
              ))}
            </div>
          </div>

          {/* Upload Custom Skin */}
          <div className="mh-form-field">
            <label>Загрузить свой скин (.png)</label>
            <div style={{ display: "flex", gap: "10px", alignItems: "center" }}>
              <label className="btn btn-secondary btn-sm" style={{ cursor: "pointer", display: "inline-flex", alignItems: "center", gap: "6px" }}>
                <span>📁 Выбрать файл .png…</span>
                <input
                  type="file"
                  accept="image/png"
                  style={{ display: "none" }}
                  onChange={handleFileUpload}
                />
              </label>
              <span className="mh-muted-meta" style={{ fontSize: "12px" }}>
                Поддерживаются скины 64×64 и 64×32 пикселя
              </span>
            </div>
          </div>

          {/* Capes Selection */}
          <div className="mh-form-field">
            <label>Плащ</label>
            <div style={{ display: "flex", gap: "8px", flexWrap: "wrap", alignItems: "center" }}>
              {PRESET_CAPES.map((c) => (
                <button
                  key={c.id}
                  type="button"
                  className={`btn btn-xs ${capeUrl === c.url ? "btn-primary" : "btn-ghost"}`}
                  onClick={() => {
                    setCapeUrl(c.url);
                    setCapeAlias(c.alias);
                  }}
                >
                  {c.name}
                </button>
              ))}
              <label className="btn btn-ghost btn-xs" style={{ cursor: "pointer" }}>
                <span>+ Свой плащ (.png)</span>
                <input
                  type="file"
                  accept="image/png"
                  style={{ display: "none" }}
                  onChange={handleCapeUpload}
                />
              </label>
            </div>
          </div>

          {/* Footer Actions */}
          <div className="mh-modal-actions-end" style={{ marginTop: "16px" }}>
            <button type="button" className="btn btn-ghost btn-sm" disabled={saving} onClick={onClose}>
              Отмена
            </button>
            <button
              type="button"
              className={`btn btn-primary btn-sm${saving ? " is-loading" : ""}`}
              disabled={saving}
              onClick={handleApply}
            >
              {saving ? "Сохранение…" : "Сохранить скин"}
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};
