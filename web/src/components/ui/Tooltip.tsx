import { useEffect, useRef, useState, type ReactNode } from "react";

// Tooltip — shows `label` on a 400ms delayed hover/focus over its single child
// (spec §1). bg-raised, micro text. The pending show timer is cancelled when the
// pointer/focus leaves before the delay elapses.
export function Tooltip({
  label,
  children,
  side = "top",
}: {
  label: ReactNode;
  children: ReactNode;
  side?: "top" | "bottom";
}) {
  const [shown, setShown] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout> | null>(null);

  const clear = () => {
    if (timer.current) {
      clearTimeout(timer.current);
      timer.current = null;
    }
  };
  const schedule = () => {
    clear();
    timer.current = setTimeout(() => setShown(true), 400);
  };
  const hide = () => {
    clear();
    setShown(false);
  };

  useEffect(() => clear, []);

  return (
    <span
      className="relative inline-flex"
      onMouseEnter={schedule}
      onMouseLeave={hide}
      onFocus={schedule}
      onBlur={hide}
    >
      {children}
      {shown && (
        <span
          role="tooltip"
          className={[
            "pointer-events-none absolute left-1/2 z-[70] -translate-x-1/2 whitespace-nowrap",
            "rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-2 py-1",
            "text-[11px] text-[var(--color-text-1)] shadow-[var(--shadow-raised)]",
            side === "top" ? "bottom-full mb-1.5" : "top-full mt-1.5",
          ].join(" ")}
        >
          {label}
        </span>
      )}
    </span>
  );
}
