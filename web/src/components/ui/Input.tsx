import type { InputHTMLAttributes } from "react";

// Input — surface bg, subtle border that strengthens on focus, text-1 fill.
// Consistent paddings/radii with the rest of the ui/ set (spec §1).
export function Input({
  className = "",
  ...rest
}: InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={[
        "w-full rounded-md bg-[var(--color-bg-surface)] px-2.5 py-1.5 text-xs text-[var(--color-text-1)]",
        "border border-[var(--color-border-subtle)] outline-none",
        "transition-colors duration-[var(--motion-micro)] ease-[var(--motion-ease)]",
        "focus:border-[var(--color-border-strong)]",
        "placeholder:text-[var(--color-text-3)] disabled:opacity-50",
        className,
      ].join(" ")}
      {...rest}
    />
  );
}
