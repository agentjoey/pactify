import type { ReactNode } from "react";

// Capability and metadata tags use a quiet outline in the supplied token color.
export type BadgeColor =
  | "role-product"
  | "role-design"
  | "role-dev"
  | "role-qa"
  | "role-pm"
  | "role-designer"
  | "role-research"
  | "role-ops"
  | "danger"
  | "warn"
  | "success"
  // Muted informational color (tier badges).
  | "text-2";

export function Badge({
  color = "role-design",
  colorVar,
  className = "",
  title,
  ariaLabel,
  "data-testid": dataTestId,
  children,
}: {
  color?: BadgeColor;
  // Escape hatch for runtime-computed colors (e.g. `var(${roleColorVar(roles)})`).
  // Takes precedence over `color`; the token union stays the encouraged path.
  colorVar?: string;
  className?: string;
  title?: string;
  // Accessible name. A bare <span> is name-prohibited in ARIA 1.2, so when
  // (and only when) this is set the root also gets role="img".
  ariaLabel?: string;
  "data-testid"?: string;
  children: ReactNode;
}) {
  const token = colorVar ?? `var(--color-${color})`;
  return (
    <span
      className={[
        "ui-badge",
        className,
      ].join(" ")}
      style={{
        color: token,
        borderColor: token,
      }}
      title={title}
      role={ariaLabel ? "img" : undefined}
      aria-label={ariaLabel || undefined}
      data-testid={dataTestId}
    >
      {children}
    </span>
  );
}
