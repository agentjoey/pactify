import { useEffect, useState } from "react";
import type { PlanReviewResponse } from "../lib/types";
import { getPlanReview } from "../lib/api";
import { Badge } from "./ui/Badge";
import { Alert } from "./ui/Alert";
import { EmptyState } from "./ui/EmptyState";
import { Spinner } from "./ui/Spinner";

// PlanReview (#7) — the human review surface for a planner-generated task graph
// before `pactify plan apply` assigns it. Pick a feature, see its proposed tasks
// (owner → reviewer, deps, verify command) and a validity verdict. Read-only for
// now: the apply gate stays in the CLP (`plan apply`); this turns the black-box
// manifest into a reviewable artifact and tells the user the exact apply command.
export function PlanReview({
  project,
  features,
}: {
  project: string;
  features: string[];
}) {
  const [feature, setFeature] = useState<string>(features[0] ?? "");
  const [data, setData] = useState<PlanReviewResponse | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState("");

  // Keep a selected feature valid as the project/feature list changes.
  useEffect(() => {
    if (!features.includes(feature)) setFeature(features[0] ?? "");
  }, [features, feature]);

  useEffect(() => {
    if (!project || !feature) {
      setData(null);
      return;
    }
    setLoading(true);
    getPlanReview(project, feature)
      .then((r) => {
        setData(r);
        setError("");
      })
      .catch(() => setError("Failed to load plan"))
      .finally(() => setLoading(false));
  }, [project, feature]);

  return (
    <div className="flex-1 overflow-y-auto px-6 py-5 view-enter">
      <div className="mb-4 flex items-center gap-3">
        <h2 className="text-[15px] font-[650] text-[var(--color-text-1)]">Plan review</h2>
        {features.length > 0 && (
          <select
            aria-label="feature"
            value={feature}
            onChange={(e) => setFeature(e.target.value)}
            className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-2 py-1 text-[12px] text-[var(--color-text-1)] outline-none focus-visible:ring-2"
          >
            {features.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
        )}
        {loading && <Spinner size="sm" />}
      </div>

      {error && (
        <Alert tone="danger" title="Load failed">
          {error}
        </Alert>
      )}

      {!error && features.length === 0 && (
        <EmptyState title="No features yet" hint="Run `pactify plan <feature>` to generate a task graph, then review it here." />
      )}

      {!error && features.length > 0 && data && !data.present && (
        <EmptyState
          title={`No plan for "${feature}"`}
          hint={`Generate one with \`pactify plan ${feature}\` — it writes .pact/plan-${feature}.json for review.`}
        />
      )}

      {!error && data && data.present && (
        <div className="fade-rise">
          <div className="mb-3 flex items-center gap-2 text-[12px] text-[var(--color-text-2)]">
            <span className="mono">{data.feature}</span>
            {data.branch && <span className="text-[var(--color-text-3)]">→ {data.branch}</span>}
            {data.valid ? (
              <Badge color="role-dev">valid</Badge>
            ) : (
              <Badge color="danger">invalid</Badge>
            )}
            <span className="text-[var(--color-text-3)]">{data.tasks?.length ?? 0} tasks</span>
          </div>

          {!data.valid && data.error && (
            <Alert tone="warn" title="Validation" className="mb-3">
              {data.error}
            </Alert>
          )}

          <ol className="flex flex-col gap-2">
            {(data.tasks ?? []).map((t, i) => (
              <li
                key={t.id}
                data-testid="plan-task"
                style={{ animationDelay: `${i * 40}ms` }}
                className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5 hover-lift fade-rise"
              >
                <div className="flex items-center gap-2">
                  <span className="mono text-[12px] font-medium text-[var(--color-text-1)]">{t.id}</span>
                  <span className="text-[11px] text-[var(--color-text-2)]">
                    {t.owner} <span className="text-[var(--color-text-3)]">→</span> {t.reviewer}
                  </span>
                  {t.deps && t.deps.length > 0 && (
                    <span className="text-[10.5px] text-[var(--color-text-3)]">deps: {t.deps.join(", ")}</span>
                  )}
                </div>
                {t.spec && (
                  <div className="mt-1 mono text-[10.5px] text-[var(--color-text-3)]">{t.spec}</div>
                )}
                {t.verify && (
                  <div className="mt-1 text-[10.5px] text-[var(--color-text-2)]">
                    <span className="text-[var(--color-text-3)]">verify:</span> <span className="mono">{t.verify}</span>
                  </div>
                )}
              </li>
            ))}
          </ol>

          {data.valid && (
            <div className="mt-4 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-raised)] px-3 py-2 text-[11px] text-[var(--color-text-2)]">
              Looks good? Apply with{" "}
              <span className="mono text-[var(--color-text-1)]">pactify plan apply {data.feature}</span>
            </div>
          )}
        </div>
      )}
    </div>
  );
}
