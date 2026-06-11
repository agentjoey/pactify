import type { SelectHTMLAttributes } from "react";

// Select — matches Input styling (surface bg, subtle→strong focus border,
// text-1). Children are <option> elements (spec §1).
export function Select({
  className = "",
  children,
  ...rest
}: SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={[
        "w-full rounded-md bg-[var(--color-bg-surface)] px-2.5 py-1.5 text-xs text-[var(--color-text-1)]",
        "border border-[var(--color-border-subtle)] outline-none",
        "transition-colors duration-[var(--motion-micro)] ease-[var(--motion-ease)]",
        "focus:border-[var(--color-border-strong)] disabled:opacity-50",
        className,
      ].join(" ")}
      {...rest}
    >
      {children}
    </select>
  );
}
