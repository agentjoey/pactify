// Skeleton — a single shimmer block (the `.skeleton` class in index.css owns the
// sweep + reduced-motion fallback). Width/height come through as inline style so
// callers can lay out a few bars without bespoke CSS. Decorative, aria-hidden.
export function Skeleton({
  width = "100%",
  height = 12,
  className = "",
  style,
}: {
  width?: number | string;
  height?: number | string;
  className?: string;
  style?: React.CSSProperties;
}) {
  return (
    <div
      data-testid="skeleton"
      aria-hidden
      className={`skeleton ${className}`.trim()}
      style={{ width, height, ...style }}
    />
  );
}

// BoardSkeleton — a few ghost columns of gray bars for the kanban's first paint.
export function BoardSkeleton() {
  return (
    <div
      data-testid="board-skeleton"
      className="grid flex-1 gap-3 overflow-hidden p-4"
      style={{ gridTemplateColumns: "repeat(5, minmax(140px, 1fr))" }}
    >
      {Array.from({ length: 5 }).map((_, c) => (
        <div key={c} className="flex flex-col gap-[9px]">
          <Skeleton width={84} height={10} className="mb-1.5" />
          {Array.from({ length: c === 4 ? 1 : 3 - (c % 3) }).map((_, i) => (
            <Skeleton key={i} height={56} />
          ))}
        </div>
      ))}
    </div>
  );
}

// CanvasSkeleton — a dim stage with a couple of ghost frame outlines so the
// canvas doesn't flash empty before the first snapshot lands.
export function CanvasSkeleton() {
  return (
    <div
      data-testid="canvas-skeleton"
      className="relative flex-1 overflow-hidden p-8"
      style={{ background: "var(--color-bg-page)" }}
    >
      <Skeleton width={220} height={120} className="mb-6" />
      <Skeleton width={180} height={120} />
    </div>
  );
}

// OpsSkeleton — a few stacked gray rows standing in for the registry panels.
export function OpsSkeleton() {
  return (
    <div data-testid="ops-skeleton" className="flex-1 overflow-hidden p-4">
      <Skeleton width={120} height={10} className="mb-3" />
      <div className="flex flex-col gap-2">
        {Array.from({ length: 3 }).map((_, i) => (
          <Skeleton key={i} height={32} />
        ))}
      </div>
    </div>
  );
}
