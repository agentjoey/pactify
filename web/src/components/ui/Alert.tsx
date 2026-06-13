import type { ReactNode } from "react";

// Alert — a persistent inline banner for state that a transient toast would lose:
// a failed fetch, an empty-with-reason panel, a heads-up. Four tones map to the
// semantic tokens (danger/warn/success) and the design accent (info). Tone tints
// the left rail, icon, and a faint surface wash; the body stays text-1 for
// legibility. Optional onRetry renders a trailing action (e.g. "Retry" after a
// failed load) so the banner is actionable, not just informational.
export type AlertTone = "info" | "warn" | "danger" | "success";

const TONE: Record<AlertTone, { color: string; icon: string }> = {
  info: { color: "var(--color-role-design)", icon: "ⓘ" },
  warn: { color: "var(--color-warn)", icon: "⚠" },
  danger: { color: "var(--color-danger)", icon: "⊘" },
  success: { color: "var(--color-success)", icon: "✓" },
};

export function Alert({
  tone = "info",
  title,
  children,
  onRetry,
  retryLabel = "Retry",
  className = "",
}: {
  tone?: AlertTone;
  title?: ReactNode;
  children?: ReactNode;
  onRetry?: () => void;
  retryLabel?: string;
  className?: string;
}) {
  const t = TONE[tone];
  return (
    <div
      role="alert"
      data-testid="alert"
      data-tone={tone}
      className={[
        "flex items-start gap-2.5 rounded-md border px-3 py-2.5 text-[12px]",
        className,
      ].join(" ")}
      style={{
        borderColor: `color-mix(in srgb, ${t.color} 35%, transparent)`,
        background: `color-mix(in srgb, ${t.color} 8%, transparent)`,
      }}
    >
      <span aria-hidden className="mt-px shrink-0 text-[13px]" style={{ color: t.color }}>
        {t.icon}
      </span>
      <div className="min-w-0 flex-1">
        {title && (
          <div className="font-medium text-[var(--color-text-1)]">{title}</div>
        )}
        {children && (
          <div className="text-[var(--color-text-2)] [overflow-wrap:anywhere]">{children}</div>
        )}
      </div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="shrink-0 self-center rounded px-2 py-0.5 text-[11px] font-medium outline-none transition-colors duration-[var(--motion-micro)] hover:bg-white/10 focus-visible:ring-2"
          style={{ color: t.color }}
        >
          {retryLabel}
        </button>
      )}
    </div>
  );
}
