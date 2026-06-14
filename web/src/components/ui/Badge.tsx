import type { ReactNode } from "react";

// Badge — a pill colored by a token name. `color` maps to a --color-<name>
// CSS var (spec §1 / board1): 15%-alpha background + the full-strength color as
// the text. Any role-/semantic token name in tokens.css is valid.
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
  | "success";

export function Badge({
  color = "role-design",
  colorVar,
  className = "",
  children,
}: {
  color?: BadgeColor;
  // Escape hatch for runtime-computed colors (e.g. `var(${roleColorVar(roles)})`).
  // Takes precedence over `color`; the token union stays the encouraged path.
  colorVar?: string;
  className?: string;
  children: ReactNode;
}) {
  const token = colorVar ?? `var(--color-${color})`;
  return (
    <span
      className={[
        // UPPERCASE so badges (roles / capability tags) read distinctly from the
        // lowercase pact-state pills — state = lowercase, badge = UPPERCASE.
        "inline-flex items-center rounded-full px-2 py-px text-[10px] font-medium uppercase tracking-[.4px] leading-[18px]",
        className,
      ].join(" ")}
      style={{
        color: token,
        background: `color-mix(in srgb, ${token} 15%, transparent)`,
      }}
    >
      {children}
    </span>
  );
}
