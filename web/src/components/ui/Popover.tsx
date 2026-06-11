import { useEffect, useRef, type ReactNode } from "react";

// Popover — an anchored dropdown panel (bg-raised, shadow-raised). The anchor is
// rendered inline; when `open`, the panel renders below it. Closes on
// outside-click (mousedown outside the wrapper) and on Esc. The parent owns the
// open state (controlled).
export function Popover({
  open,
  onClose,
  anchor,
  children,
  align = "left",
  className = "",
}: {
  open: boolean;
  onClose: () => void;
  anchor: ReactNode;
  children: ReactNode;
  align?: "left" | "right";
  className?: string;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const onDown = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        onClose();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    document.addEventListener("mousedown", onDown);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDown);
      document.removeEventListener("keydown", onKey);
    };
  }, [open, onClose]);

  return (
    <div ref={wrapRef} className="relative inline-block">
      {anchor}
      {open && (
        <div
          role="menu"
          className={[
            "absolute top-full z-50 mt-1 min-w-[180px] rounded-lg border border-[var(--color-border-subtle)]",
            "bg-[var(--color-bg-raised)] p-1 shadow-[var(--shadow-raised)]",
            align === "right" ? "right-0" : "left-0",
            className,
          ].join(" ")}
        >
          {children}
        </div>
      )}
    </div>
  );
}
