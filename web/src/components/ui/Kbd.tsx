import type { ReactNode } from "react";

// Kbd — a tiny mono key chip (board3 .seg kbd style). Used for ⌘K hints and the
// 1/2/3 segmented-control shortcuts.
export function Kbd({
  children,
  className = "",
}: {
  children: ReactNode;
  className?: string;
}) {
  return (
    <kbd
      className={[
        "inline-flex items-center rounded-[3.5px] border border-[var(--color-border-strong)]",
        "bg-[rgba(18,22,31,.07)] px-1.5 text-[9px] leading-[15px] text-[var(--color-text-3)] font-mono",
        className,
      ].join(" ")}
    >
      {children}
    </kbd>
  );
}
