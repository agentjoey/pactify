import type { ReactNode } from "react";

// EmptyState — centered icon slot + title + hint inside a dashed-border
// container (spec §1 / §6 empty states). Hint uses text-3.
export function EmptyState({
  icon,
  title,
  hint,
  className = "",
}: {
  icon?: ReactNode;
  title: ReactNode;
  hint?: ReactNode;
  className?: string;
}) {
  return (
    <div
      data-testid="empty-state"
      className={[
        "flex flex-col items-center justify-center gap-2 rounded-lg",
        "border border-dashed border-[var(--color-border-subtle)] px-6 py-8 text-center",
        className,
      ].join(" ")}
    >
      {icon && <div className="text-[var(--color-text-2)]">{icon}</div>}
      <div className="text-[13px] font-medium text-[var(--color-text-1)]">{title}</div>
      {hint && <div className="text-[11px] text-[var(--color-text-3)]">{hint}</div>}
    </div>
  );
}
