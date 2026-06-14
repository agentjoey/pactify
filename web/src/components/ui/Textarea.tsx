import { useState, type TextareaHTMLAttributes } from "react";

// Textarea — a prompt/spec text area with dify-style affordances in a footer:
// a live character count and an expand toggle (grows the field). The variable
// hint ({x}) is shown when `variables` is set. Controlled or uncontrolled; the
// char count tracks whichever value is current.
export function Textarea({
  value,
  defaultValue,
  variables,
  className = "",
  onChange,
  ...rest
}: TextareaHTMLAttributes<HTMLTextAreaElement> & { variables?: boolean }) {
  const [internal, setInternal] = useState(String(defaultValue ?? ""));
  const [expanded, setExpanded] = useState(false);
  const current = value !== undefined ? String(value) : internal;

  return (
    <div className={`rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] ${className}`}>
      <textarea
        value={value}
        defaultValue={value === undefined ? defaultValue : undefined}
        onChange={(e) => {
          if (value === undefined) setInternal(e.target.value);
          onChange?.(e);
        }}
        className={[
          "w-full resize-none bg-transparent px-3 pt-2 text-[12px] text-[var(--color-text-1)] outline-none",
          "placeholder:text-[var(--color-text-3)]",
          expanded ? "h-32" : "h-16",
        ].join(" ")}
        {...rest}
      />
      <div className="flex items-center justify-between px-2.5 pb-1.5 pt-0.5 text-[10px] text-[var(--color-text-3)]">
        <div className="flex items-center gap-2">
          <span>{current.length}</span>
          {variables && <span className="mono rounded bg-[var(--color-bg-surface)] px-1">{"{x}"}</span>}
        </div>
        <button
          type="button"
          onClick={() => setExpanded((x) => !x)}
          className="rounded px-1.5 py-0.5 transition-colors hover:bg-[var(--color-bg-surface)] hover:text-[var(--color-text-2)]"
        >
          {expanded ? "Collapse" : "Expand"}
        </button>
      </div>
    </div>
  );
}
