import React, { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useOverlayTransition } from "../hooks";

export interface CustomSelectOption {
  value: string;
  label: string;
  icon?: React.ReactNode;
  sub?: string;
}

interface CustomSelectProps {
  value: string;
  options: CustomSelectOption[];
  onChange: (value: string) => void;
  placeholder?: string;
  searchable?: boolean;
  searchPlaceholder?: string;
  className?: string;
  mono?: boolean;
  ariaLabel?: string;
  title?: string;
  disabled?: boolean;
  id?: string;
}

export const CustomSelect: React.FC<CustomSelectProps> = ({
  value,
  options,
  onChange,
  placeholder = "Выбрать...",
  searchable = false,
  searchPlaceholder = "Поиск...",
  className = "",
  mono = false,
  ariaLabel,
  title,
  disabled = false,
  id,
}) => {
  const [open, setOpen] = useState(false);
  const { mounted: menuMounted, shown: menuShown } = useOverlayTransition(open, 170);
  const [search, setSearch] = useState("");
  const [activeIdx, setActiveIdx] = useState(0);
  const [pos, setPos] = useState<{ top: number; left: number; width: number; openUp: boolean }>({
    top: 0,
    left: 0,
    width: 200,
    openUp: false,
  });

  const triggerRef = useRef<HTMLButtonElement | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);
  const searchInputRef = useRef<HTMLInputElement | null>(null);

  const selectedOpt = useMemo(
    () => options.find((o) => o.value === value) || null,
    [options, value]
  );

  const filteredOptions = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return options;
    return options.filter(
      (o) => o.label.toLowerCase().includes(q) || o.value.toLowerCase().includes(q)
    );
  }, [options, search]);

  const updatePosition = () => {
    const btn = triggerRef.current;
    if (!btn) return;
    const rect = btn.getBoundingClientRect();
    const estMenuH = Math.min(280, Math.max(120, filteredOptions.length * 34 + (searchable ? 46 : 12)));
    const spaceBelow = window.innerHeight - rect.bottom - 10;
    const spaceAbove = rect.top - 10;
    const openUp = spaceBelow < estMenuH && spaceAbove > spaceBelow;
    const width = Math.max(rect.width, 148);
    const left = Math.max(8, Math.min(rect.left, window.innerWidth - width - 8));
    setPos({
      top: openUp ? rect.top - 6 : rect.bottom + 6,
      left,
      width,
      openUp,
    });
  };

  useLayoutEffect(() => {
    if (!open) return;
    updatePosition();
  }, [open, filteredOptions.length]);

  useEffect(() => {
    if (!open) return;
    setSearch("");
    const curIdx = Math.max(
      0,
      options.findIndex((o) => o.value === value)
    );
    setActiveIdx(curIdx);

    if (searchable) {
      window.setTimeout(() => searchInputRef.current?.focus(), 10);
    }

    const onDocDown = (e: MouseEvent) => {
      const t = e.target as Node;
      if (triggerRef.current?.contains(t) || menuRef.current?.contains(t)) return;
      setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        setOpen(false);
        triggerRef.current?.focus();
      }
    };
    const onReposition = () => updatePosition();

    document.addEventListener("mousedown", onDocDown);
    window.addEventListener("keydown", onKey, true);
    window.addEventListener("resize", onReposition, { passive: true });
    window.addEventListener("scroll", onReposition, { passive: true, capture: true });
    return () => {
      document.removeEventListener("mousedown", onDocDown);
      window.removeEventListener("keydown", onKey, true);
      window.removeEventListener("resize", onReposition);
      window.removeEventListener("scroll", onReposition, true);
    };
  }, [open]);

  const handleSelect = (val: string) => {
    onChange(val);
    setOpen(false);
  };

  const handleKeyDown = (e: React.KeyboardEvent) => {
    if (disabled) return;
    if (!open) {
      if (e.key === "Enter" || e.key === " " || e.key === "ArrowDown") {
        e.preventDefault();
        setOpen(true);
      }
      return;
    }
    if (e.key === "ArrowDown") {
      e.preventDefault();
      setActiveIdx((i) => (filteredOptions.length ? (i + 1) % filteredOptions.length : 0));
    } else if (e.key === "ArrowUp") {
      e.preventDefault();
      setActiveIdx((i) =>
        filteredOptions.length ? (i - 1 + filteredOptions.length) % filteredOptions.length : 0
      );
    } else if (e.key === "Enter") {
      e.preventDefault();
      const hit = filteredOptions[activeIdx];
      if (hit) handleSelect(hit.value);
    }
  };

  return (
    <>
      <button
        ref={triggerRef}
        id={id}
        type="button"
        disabled={disabled}
        aria-label={ariaLabel}
        aria-haspopup="listbox"
        aria-expanded={open}
        title={title}
        className={`custom-select-trigger${open ? " is-open" : ""}${mono ? " mono-num" : ""}${className ? ` ${className}` : ""}`}
        onClick={() => !disabled && setOpen((v) => !v)}
        onKeyDown={handleKeyDown}
      >
        <span className="custom-select-value">
          {selectedOpt?.icon && <span className="custom-select-opt-ico">{selectedOpt.icon}</span>}
          <span className="custom-select-label">{selectedOpt ? selectedOpt.label : placeholder}</span>
        </span>
        <svg
          className="custom-select-chevron"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.2"
          strokeLinecap="round"
          strokeLinejoin="round"
          aria-hidden="true"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      {menuMounted &&
        createPortal(
          <div
            ref={menuRef}
            className={`custom-select-menu${menuShown ? " is-shown" : ""}${pos.openUp ? " is-up" : ""}${mono ? " mono-num" : ""}`}
            style={{
              position: "fixed",
              left: `${pos.left}px`,
              width: `${pos.width}px`,
              ...(pos.openUp
                ? { bottom: `${window.innerHeight - pos.top}px` }
                : { top: `${pos.top}px` }),
            }}
            role="listbox"
            onClick={(e) => e.stopPropagation()}
          >
            {searchable && (
              <div className="custom-select-search-wrap">
                <input
                  ref={searchInputRef}
                  type="search"
                  className="custom-select-search"
                  placeholder={searchPlaceholder}
                  value={search}
                  onChange={(e) => {
                    setSearch(e.target.value);
                    setActiveIdx(0);
                  }}
                  onKeyDown={handleKeyDown}
                />
              </div>
            )}
            <div className="custom-select-list">
              {filteredOptions.length === 0 ? (
                <div className="custom-select-empty">Ничего не найдено</div>
              ) : (
                filteredOptions.map((opt, idx) => {
                  const isSelected = opt.value === value;
                  const isActive = idx === activeIdx;
                  return (
                    <button
                      key={`${opt.value}-${idx}`}
                      type="button"
                      role="option"
                      aria-selected={isSelected}
                      className={`custom-select-option${isSelected ? " is-selected" : ""}${isActive ? " is-active" : ""}`}
                      onMouseEnter={() => setActiveIdx(idx)}
                      onClick={() => handleSelect(opt.value)}
                    >
                      <span className="custom-select-opt-main">
                        {opt.icon && <span className="custom-select-opt-ico">{opt.icon}</span>}
                        <span className="custom-select-opt-text">{opt.label}</span>
                        {opt.sub && <span className="custom-select-opt-sub">{opt.sub}</span>}
                      </span>
                      <span className="custom-select-check" aria-hidden="true">
                        {isSelected ? "✓" : ""}
                      </span>
                    </button>
                  );
                })
              )}
            </div>
          </div>,
          document.body
        )}
    </>
  );
};
