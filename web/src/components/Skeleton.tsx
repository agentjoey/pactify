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

