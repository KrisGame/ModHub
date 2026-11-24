import React from "react";

export interface InstanceIconPreset {
  id: string;
  label: string;
  bg: string;
  fg: string;
  symbol: string;
}

export const INSTANCE_ICON_PRESETS: InstanceIconPreset[] = [
  { id: "grass", label: "Трава", bg: "linear-gradient(135deg, #2d5a37, #1f3b26)", fg: "#9ee6ad", symbol: "G" },
  { id: "diamond", label: "Алмаз", bg: "linear-gradient(135deg, #1f5b6b, #143642)", fg: "#82e4f8", symbol: "D" },
  { id: "emerald", label: "Изумруд", bg: "linear-gradient(135deg, #1b5e3f, #103826)", fg: "#6ee7b7", symbol: "E" },
  { id: "nether", label: "Незер", bg: "linear-gradient(135deg, #6b2727, #3e1616)", fg: "#fca5a5", symbol: "N" },
  { id: "ender", label: "Энд", bg: "linear-gradient(135deg, #4b2d6b, #28173b)", fg: "#d8b4fe", symbol: "P" },
  { id: "gold", label: "Золото", bg: "linear-gradient(135deg, #6e521e, #3f2e10)", fg: "#fcd34d", symbol: "Au" },
  { id: "redstone", label: "Редстоун", bg: "linear-gradient(135deg, #752222, #421212)", fg: "#f87171", symbol: "R" },
  { id: "obsidian", label: "Обсидиан", bg: "linear-gradient(135deg, #282c36, #15181e)", fg: "#cbd5e1", symbol: "O" },
];

export const InstanceIcon: React.FC<{ icon?: string; name?: string; size?: number }> = ({
  icon = "grass",
  name = "M",
  size = 44,
}) => {
  const preset = INSTANCE_ICON_PRESETS.find((p) => p.id === icon) || INSTANCE_ICON_PRESETS[0];
  const letter = (name || preset.symbol).trim().charAt(0).toUpperCase() || preset.symbol;
  return (
    <span
      className="mh-inst-icon"
      style={{
        width: size,
        height: size,
        background: preset.bg,
        color: preset.fg,
        fontSize: Math.round(size * 0.42),
      }}
      aria-hidden="true"
    >
      {letter}
    </span>
  );
};
