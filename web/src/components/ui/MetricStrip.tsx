export interface MetricItem {
  label: string;        // uppercase, e.g. RUN / TOK / ITER
  value: string;        // formatted, e.g. 3m02s / 12.4k / ×2
  live?: boolean;       // in-flight → design-blue value (active accumulation)
  est?: boolean;        // not-yet-run estimate → italic faint value
}

// MetricStrip — the compact JetBrains-Mono metric row (handoff §stat strip). Items
// are separated by a middot at .4 opacity; labels are text-3, values text-2; live
// values flip to design-blue to signal active accumulation; estimates render italic.
export function MetricStrip({ items, className = "" }: { items: MetricItem[]; className?: string }) {
  return (
    <div
      data-testid="metric-strip"
      className={`mono flex items-center ${className}`}
      style={{
        gap: 7,
        fontSize: 8.5,
        fontFamily: "var(--font-mono, monospace)",
        fontWeight: 600,
        letterSpacing: ".3px",
        color: "var(--color-text-3)",
      }}
    >
      {items.map((it, i) => (
        <span key={i} className="inline-flex items-center" style={{ gap: 4 }}>
          {i > 0 && (
            <span data-testid="metric-sep" aria-hidden style={{ opacity: 0.4, marginRight: 3 }}>·</span>
          )}
          {it.label && <span style={{ color: "var(--color-text-3)" }}>{it.label}</span>}
          <span
            style={{
              color: it.live ? "var(--color-role-design)" : "var(--color-text-2)",
              fontStyle: it.est ? "italic" : "normal",
            }}
          >
            {it.value}
          </span>
        </span>
      ))}
    </div>
  );
}
