import { useEffect, useState } from "react";
import { getPlanReview } from "../../lib/api";
import type { PlanReviewResponse } from "../../lib/types";

// PlanDock — floating read-only plan widget showing the current feature's task
// breakdown + status. Mounted alongside RosterDock so it's visible across all 3
// lenses. No Apply / no editing — planning happens via CLI or a conversation window.
export function PlanDock({ project, features }: { project: string; features: string[] }) {
  const [feature, setFeature] = useState(features[0] ?? "");
  const [data, setData] = useState<PlanReviewResponse | null>(null);
  const [collapsed, setCollapsed] = useState(false);

  useEffect(() => { setFeature(features[0] ?? ""); }, [features]);

  useEffect(() => {
    if (!feature) { setData(null); return; }
    let alive = true;
    getPlanReview(project, feature)
      .then((d) => { if (alive) setData(d); })
      .catch(() => { if (alive) setData(null); });
    return () => { alive = false; };
  }, [project, feature]);

  if (features.length === 0) {
    return (
      <div
        data-testid="plan-dock-empty"
        className="pointer-events-auto w-full rounded-2xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-overlay)]/70 p-3 text-[11px] text-[var(--color-text-3)] shadow-[var(--shadow-overlay)] backdrop-blur-md"
      >
        No features yet — plan via CLI or the conversation window.
      </div>
    );
  }

  return (
    <div
      data-testid="plan-dock"
      className="pointer-events-auto w-full rounded-2xl border border-[var(--color-border-subtle)] bg-[var(--color-bg-overlay)]/70 p-2.5 shadow-[var(--shadow-overlay)] backdrop-blur-md"
    >
      <div className="flex items-center justify-between px-1 text-[11px] font-semibold">
        <span>📋 Plan</span>
        <button
          type="button"
          data-testid="plan-dock-toggle"
          className="text-[var(--color-text-3)]"
          onClick={() => setCollapsed((c) => !c)}
        >
          {collapsed ? "▸" : "▾"}
        </button>
      </div>
      {!collapsed && (
        <div data-testid="plan-dock-body" className="mt-2">
          {features.length > 1 && (
            <select
              className="mb-2 w-full bg-transparent text-[11px]"
              value={feature}
              onChange={(e) => setFeature(e.target.value)}
            >
              {features.map((f) => (
                <option key={f} value={f}>{f}</option>
              ))}
            </select>
          )}
          <ul className="flex flex-col gap-1 text-[10px] text-[var(--color-text-2)]">
            {(data?.tasks ?? []).map((t) => (
              <li key={t.id} data-testid="plan-dock-task" className="truncate">
                {t.id}{t.deps?.length ? ` · deps: ${t.deps.join(",")}` : ""}
              </li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}
