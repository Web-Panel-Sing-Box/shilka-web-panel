import { useEffect, useLayoutEffect, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { AnimatePresence, m } from "framer-motion";
import { Check, ChevronDown } from "lucide-react";

import { dropdownVariants } from "@/lib/motion";
import { cn } from "@/lib/utils";
import type { SelectOption } from "./select";

type MultiSelectProps<T extends string> = {
  values: T[];
  options: SelectOption<T>[];
  onChange: (values: T[]) => void;
  placeholder?: string;
  className?: string;
  disabled?: boolean;
};

// MultiSelect mirrors Select but lets the user pick several options via a
// checkbox-style menu that stays open while toggling (SIN-11 multi-inbound).
export function MultiSelect<T extends string>({
  values,
  options,
  onChange,
  placeholder = "Select…",
  className,
  disabled,
}: MultiSelectProps<T>) {
  const [open, setOpen] = useState(false);
  const [menuRect, setMenuRect] = useState({ left: 0, top: 0, width: 0 });
  const ref = useRef<HTMLDivElement>(null);
  const menuRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function onClick(e: MouseEvent) {
      const target = e.target as Node;
      if (!ref.current?.contains(target) && !menuRef.current?.contains(target)) setOpen(false);
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") setOpen(false);
    }
    window.addEventListener("mousedown", onClick);
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("mousedown", onClick);
      window.removeEventListener("keydown", onKey);
    };
  }, [open]);

  useLayoutEffect(() => {
    if (!open || !ref.current) return;
    const update = () => {
      const rect = ref.current?.getBoundingClientRect();
      if (!rect) return;
      setMenuRect({ left: rect.left, top: rect.bottom + 6, width: rect.width });
    };
    update();
    window.addEventListener("resize", update);
    window.addEventListener("scroll", update, true);
    return () => {
      window.removeEventListener("resize", update);
      window.removeEventListener("scroll", update, true);
    };
  }, [open]);

  const selectedSet = new Set(values);
  const selectedLabels = options.filter((o) => selectedSet.has(o.value)).map((o) => o.label);

  function toggle(v: T) {
    if (selectedSet.has(v)) onChange(values.filter((x) => x !== v));
    else onChange([...values, v]);
  }

  const menu =
    typeof document === "undefined"
      ? null
      : createPortal(
          <AnimatePresence>
            {open ? (
              <m.div
                ref={menuRef}
                initial="initial"
                animate="animate"
                exit="exit"
                variants={dropdownVariants}
                role="listbox"
                aria-multiselectable="true"
                className="fixed z-[120] overflow-hidden rounded-lg border border-subtle bg-elevated shadow-pop"
                style={{ left: menuRect.left, top: menuRect.top, width: menuRect.width }}
              >
                <ul className="max-h-72 overflow-y-auto py-1">
                  {options.map((opt) => {
                    const active = selectedSet.has(opt.value);
                    return (
                      <li key={opt.value}>
                        <button
                          type="button"
                          role="option"
                          aria-selected={active}
                          onClick={() => toggle(opt.value)}
                          className={cn(
                            "flex w-full items-start justify-between gap-3 px-3 py-2 text-left text-sm transition-colors duration-150 hover:bg-hover",
                            active ? "text-ink-primary" : "text-ink-secondary",
                          )}
                        >
                          <div className="min-w-0 flex-1">
                            <div className="truncate text-ink-primary">{opt.label}</div>
                            {opt.description ? (
                              <div className="truncate text-xs text-ink-tertiary">{opt.description}</div>
                            ) : null}
                          </div>
                          {active ? <Check size={14} className="mt-1 text-brand" /> : null}
                        </button>
                      </li>
                    );
                  })}
                </ul>
              </m.div>
            ) : null}
          </AnimatePresence>,
          document.body,
        );

  return (
    <div ref={ref} className={cn("relative", className)}>
      <button
        type="button"
        aria-haspopup="listbox"
        aria-expanded={open}
        disabled={disabled}
        onClick={() => setOpen((v) => !v)}
        className={cn(
          "flex h-10 w-full items-center justify-between gap-2 rounded-lg border border-white/10 bg-elevated px-3 text-left text-sm text-ink-primary transition-colors duration-150",
          open ? "border-white" : "hover:border-white/20",
          disabled && "cursor-not-allowed opacity-50",
        )}
      >
        <span className={cn("truncate", selectedLabels.length === 0 && "text-ink-tertiary")}>
          {selectedLabels.length > 0 ? selectedLabels.join(", ") : placeholder}
        </span>
        <ChevronDown
          size={16}
          className={cn("shrink-0 text-ink-secondary transition-transform duration-150", open && "rotate-180")}
        />
      </button>
      {menu}
    </div>
  );
}
