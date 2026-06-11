import { useEffect, useRef, type ReactNode } from "react";

// Popover — an anchored dropdown panel (bg-raised, shadow-raised). The anchor is
// rendered inline; when `open`, the panel renders below it. Closes on
// outside-click (mousedown outside the wrapper) and on Esc. The parent owns the
// open state (controlled). The panel carries NO ARIA role by default — pass
// `role="menu"` only when the children really are menuitems; aria-expanded/
// aria-haspopup on the trigger are the consumer's job (the anchor is opaque).
export function Popover({
  open,
  onClose,
  anchor,
  children,
  align = "left",
  className = "",
  role,
}: {
  open: boolean;
  onClose: () => void;
  anchor: ReactNode;
  children: ReactNode;
  align?: "left" | "right";
  className?: string;
  role?: string;
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
          role={role}
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
