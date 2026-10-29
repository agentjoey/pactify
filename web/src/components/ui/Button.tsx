import type { ButtonHTMLAttributes, ReactNode } from "react";
import { Spinner } from "./Spinner";

// Shared outline, primary and destructive controls; labels keep their original casing in the DOM.
export type ButtonVariant = "primary" | "ghost" | "danger";
export type ButtonSize = "sm" | "md";

export function Button({
  variant = "ghost",
  size = "md",
  className = "",
  type = "button",
  loading = false,
  icon,
  disabled,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  loading?: boolean;
  icon?: ReactNode;
}) {
  const lead = loading ? <Spinner size={size === "sm" ? "xs" : "sm"} /> : icon;
  return (
    <button
      type={type}
      disabled={disabled || loading}
      aria-busy={loading || undefined}
      className={[
        `ui-button ui-button-${variant} ui-button-${size}`,
        className,
      ].join(" ")}
      {...rest}
    >
      {lead}
      {children}
    </button>
  );
}
