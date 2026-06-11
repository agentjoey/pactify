import { useEffect, useId, useRef, type ReactNode } from "react";

// Modal — overlay (55% black + blur) with a panel on the bg-overlay token.
// 180ms scale+fade entry via the shared motion vars; Esc closes; overlay click
// closes (panel clicks don't bubble out); focus is trapped inside the panel
// (Tab loops). variant="danger" tints the header. Children render in the body.
const FOCUSABLE =
  'a[href],button:not([disabled]),textarea:not([disabled]),input:not([disabled]),select:not([disabled]),[tabindex]:not([tabindex="-1"])';

export function Modal({
  title,
  onClose,
  variant = "default",
  children,
  footer,
  testId = "modal",
  width = "520px",
}: {
  title: ReactNode;
  onClose: () => void;
  variant?: "default" | "danger";
  children: ReactNode;
  footer?: ReactNode;
  testId?: string;
  width?: string;
}) {
  const panelRef = useRef<HTMLDivElement>(null);
  const titleId = useId();

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") {
        e.stopPropagation();
        onClose();
        return;
      }
      if (e.key === "Tab") {
        const panel = panelRef.current;
        if (!panel) return;
        const items = Array.from(
          panel.querySelectorAll<HTMLElement>(FOCUSABLE),
        ).filter(
          (el) =>
            el.tabIndex !== -1 &&
            !el.hasAttribute("hidden") &&
            el.getAttribute("aria-hidden") !== "true",
        );
        if (items.length === 0) {
          e.preventDefault();
          return;
        }
        const first = items[0];
        const last = items[items.length - 1];
        const active = document.activeElement as HTMLElement | null;
        if (e.shiftKey) {
          if (active === first || !panel.contains(active)) {
            e.preventDefault();
            last.focus();
          }
        } else {
          if (active === last || !panel.contains(active)) {
            e.preventDefault();
            first.focus();
          }
        }
      }
    };
    document.addEventListener("keydown", onKey, true);
    return () => document.removeEventListener("keydown", onKey, true);
  }, [onClose]);

  // Move focus into the panel on open so the trap has a starting point; on
  // close, restore focus to whatever had it before (WAI-ARIA dialog pattern —
  // a keyboard user who Esc's out must not be dropped on <body>).
  useEffect(() => {
    const prev = document.activeElement as HTMLElement | null;
    const panel = panelRef.current;
    if (panel) {
      const first = Array.from(
        panel.querySelectorAll<HTMLElement>(FOCUSABLE),
      ).find((el) => el.tabIndex !== -1);
      first?.focus();
    }
    return () => prev?.focus();
  }, []);

  return (
    <div
      data-testid={`${testId}-overlay`}
      className="ui-modal-overlay fixed inset-0 z-50 flex items-center justify-center bg-black/55 backdrop-blur-sm"
      onClick={onClose}
    >
      <div
        ref={panelRef}
        data-testid={testId}
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
        className="ui-modal-panel max-w-[92vw] rounded-lg border border-[var(--color-border-subtle)] bg-[var(--color-bg-overlay)] p-4 shadow-[var(--shadow-overlay)]"
        style={{ width }}
        onClick={(e) => e.stopPropagation()}
      >
        <div
          data-testid={`${testId}-header`}
          className={[
            "mb-3 flex items-center justify-between",
            variant === "danger"
              ? "text-[var(--color-danger)]"
              : "text-[var(--color-text-1)]",
          ].join(" ")}
        >
          <h2 id={titleId} className="text-sm font-semibold">{title}</h2>
          <button
            type="button"
            tabIndex={-1}
            className="text-xs text-[var(--color-text-3)] transition-colors duration-[var(--motion-micro)] hover:text-[var(--color-text-1)]"
            onClick={onClose}
            aria-label="close"
          >
            ✕
          </button>
        </div>
        {children}
        {footer && <div className="mt-4 flex items-center gap-2">{footer}</div>}
      </div>
    </div>
  );
}
