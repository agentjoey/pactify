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

