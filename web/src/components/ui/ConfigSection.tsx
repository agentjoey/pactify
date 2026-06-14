import type { ReactNode } from "react";

// ConfigSection — the dify-style grouped config block: an UPPERCASE label (with
// an optional required *), an optional trailing action (counter / +), and a body
// (usually a soft-inset control card). The shared building block for the
// RightRail, AgentConfig, and any settings panel — so every config surface reads
// the same.
export function ConfigSection({
  label,
  required,
  action,
  children,
  className = "",
}: {
  label: string;
  required?: boolean;
  action?: ReactNode;
  children: ReactNode;
  className?: string;
}) {
  return (
    <div className={`mb-3.5 ${className}`}>
      <div className="mb-1.5 flex items-center justify-between">
        <span className="text-[10.5px] font-semibold uppercase tracking-[.5px] text-[var(--color-text-3)]">
          {label}
          {required && <span className="ml-0.5 text-[var(--color-danger)]">*</span>}
        </span>
        {action && <div className="text-[10.5px] text-[var(--color-text-3)]">{action}</div>}
      </div>
      {children}
    </div>
  );
}

// Inset — the soft-gray rounded control surface that sits inside a ConfigSection
// (dify's input/field fill).
export function Inset({ children, className = "" }: { children: ReactNode; className?: string }) {
  return (
    <div className={`rounded-md bg-[var(--color-bg-inset)] px-3 py-2 text-[12px] text-[var(--color-text-1)] ${className}`}>
      {children}
    </div>
  );
}
