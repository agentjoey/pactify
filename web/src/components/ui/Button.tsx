import type { ButtonHTMLAttributes } from "react";

// Button — the sole app-wide button primitive (spec §1 "Primitives", board1).
// variant: primary (role-design bg, dark text) / ghost (transparent, subtle
// border, text-2) / danger (danger-tinted). size: sm | md. Disabled drops to
// 40% opacity and removes pointer events. focus-visible shows a 2px role-design
// ring at 40% alpha. Motion uses the shared 120ms micro var.
export type ButtonVariant = "primary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md";

const VARIANT: Record<ButtonVariant, string> = {
  primary:
    "bg-[var(--color-role-design)] text-[var(--color-bg-page)] font-medium hover:brightness-110",
  ghost:
    "bg-transparent border border-[var(--color-border-subtle)] text-[var(--color-text-2)] hover:text-[var(--color-text-1)] hover:border-[var(--color-border-strong)]",
  danger:
    "bg-[color-mix(in_srgb,var(--color-danger)_16%,transparent)] text-[var(--color-danger)] border border-[color-mix(in_srgb,var(--color-danger)_40%,transparent)] hover:bg-[color-mix(in_srgb,var(--color-danger)_26%,transparent)]",
};

const SIZE: Record<ButtonSize, string> = {
  sm: "text-[11px] px-2.5 py-1",
  md: "text-xs px-3 py-1.5",
};

export function Button({
  variant = "primary",
  size = "md",
  className = "",
  type = "button",
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
}) {
  return (
    <button
      type={type}
      className={[
        "inline-flex items-center justify-center gap-1.5 rounded-md font-medium",
        "transition-[background-color,color,border-color,filter] duration-[var(--motion-micro)] ease-[var(--motion-ease)]",
        "outline-none focus-visible:ring-2 focus-visible:ring-[color-mix(in_srgb,var(--color-role-design)_40%,transparent)]",
        "disabled:opacity-40 disabled:pointer-events-none disabled:cursor-not-allowed",
        VARIANT[variant],
        SIZE[size],
        className,
      ].join(" ")}
      {...rest}
    />
  );
}
