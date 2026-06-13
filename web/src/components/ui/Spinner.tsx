// Spinner — the app-wide loading indicator. Inherits currentColor so it tints to
// whatever text color the caller sits in (a button, a panel header, an inline
// status). Sizes map to the type scale so it lines up with adjacent text. The
// spin is a CSS animation (index.css `.spinner-spin`) that respects
// prefers-reduced-motion (falls back to a static three-quarter ring).
export type SpinnerSize = "xs" | "sm" | "md";

const PX: Record<SpinnerSize, number> = { xs: 12, sm: 14, md: 18 };

export function Spinner({
  size = "sm",
  className = "",
  label = "Loading",
}: {
  size?: SpinnerSize;
  className?: string;
  label?: string;
}) {
  const px = PX[size];
  return (
    <svg
      data-testid="spinner"
      className={`spinner-spin ${className}`}
      width={px}
      height={px}
      viewBox="0 0 24 24"
      fill="none"
      role="status"
      aria-label={label}
    >
      <circle cx="12" cy="12" r="9" stroke="currentColor" strokeOpacity="0.25" strokeWidth="3" />
      <path
        d="M21 12a9 9 0 0 0-9-9"
        stroke="currentColor"
        strokeWidth="3"
        strokeLinecap="round"
      />
    </svg>
  );
}
