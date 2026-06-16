import { useEffect, useState } from "react";
import { getAudit, type AuditRecord } from "../lib/api";

// AuditLens — the read-only permission-audit surface (sibling of the Cost lens):
// the tool calls a seat made during a task, with a count and per-risk coloring.
// v1 is log-only (every decision is "allow"); governance is a later addition.

const RISK_COLOR: Record<string, string> = {
  exec: "var(--color-role-product)",
  write: "var(--color-warn)",
  read: "var(--color-role-dev)",
  mcp: "var(--color-role-design)",
};

function riskColor(risk: string): string {
  return RISK_COLOR[risk] ?? "var(--color-text-3)";
}

export function AuditLens({ project, task, seat }: { project: string; task?: string; seat?: string }) {
  const [recs, setRecs] = useState<AuditRecord[] | null>(null);
  const [error, setError] = useState(false);

  useEffect(() => {
    const params: Record<string, string> = {};
    if (task) params.task = task;
    if (seat) params.seat = seat;
    getAudit(project, params)
      .then((r) => {
        setRecs(r);
        setError(false);
      })
      .catch(() => setError(true));
  }, [project, task, seat]);

  if (error) return null;
  if (!recs) return null;

  return (
    <div data-testid="audit-lens" className="flex flex-col gap-1.5">
      <div className="flex items-center gap-2">
        <span className="text-[10px] font-semibold uppercase tracking-wide text-[var(--color-text-3)]">
          Permission audit
        </span>
        <span
          data-testid="audit-count"
          className="rounded-full bg-[var(--color-bg-inset)] px-1.5 py-0.5 text-[10px] font-medium text-[var(--color-text-2)]"
        >
          {recs.length}
        </span>
      </div>
      {recs.length === 0 ? (
        <span className="text-[11px] text-[var(--color-text-3)]">No tool calls recorded.</span>
      ) : (
        <ul className="flex flex-col gap-1">
          {recs.map((r, i) => (
            <li key={`${r.ts}-${i}`} className="flex items-center gap-2 text-[11px] leading-tight">
              <span
                aria-hidden
                style={{ width: 6, height: 6, borderRadius: 999, background: riskColor(r.risk), flexShrink: 0 }}
              />
              <span className="mono text-[var(--color-text-2)]">{r.tool}</span>
              <span className="truncate text-[var(--color-text-3)]">{r.summary}</span>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
