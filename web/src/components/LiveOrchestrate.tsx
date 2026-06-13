import { useEffect, useRef, useState } from "react";
import type { OrchestrateStatus } from "../lib/types";
import { getOrchestrateStatus, getParallelOrchestrate } from "../lib/api";
import { Badge } from "./ui/Badge";

export function LiveOrchestrate({
  project,
  refreshTick,
}: {
  project: string;
  refreshTick: number;
}) {
  const [status, setStatus] = useState<OrchestrateStatus | null>(null);
  const [present, setPresent] = useState<boolean | null>(null);
  // Parallel run: one status per concurrent feature. When present, it takes over
  // the view (a parallel run is the richer signal); otherwise we fall back to the
  // single serial status.json.
  const [parallel, setParallel] = useState<OrchestrateStatus[] | null>(null);
  const [error, setError] = useState("");
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);

  function load() {
    if (!project) return;
    Promise.all([getOrchestrateStatus(project), getParallelOrchestrate(project)])
      .then(([single, par]) => {
        setPresent(single.present);
        setStatus(single.status ?? null);
        setParallel(par.present && par.features && par.features.length > 0 ? par.features : null);
        setError("");
      })
      .catch(() => setError("Failed to load orchestrate status"));
  }

  useEffect(() => {
    load();
    timer.current = setInterval(() => load(), 3000);
    return () => {
      if (timer.current) clearInterval(timer.current);
    };
  }, [project]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    load();
  }, [refreshTick, project]); // eslint-disable-line react-hooks/exhaustive-deps

  // A parallel run aggregates several features; sort by id for a stable order.
  const parallelSorted = parallel
    ? [...parallel].sort((a, b) => a.feature.localeCompare(b.feature))
    : null;

  return (
    <div
      data-testid="live-orchestrate"
      aria-label="orchestrate live view"
      className="flex-1 overflow-y-auto p-5"
    >
      {error && (
        <div className="text-xs text-[var(--color-danger)] mb-3">{error}</div>
      )}

      {/* Parallel run: a grid of per-feature cards. */}
      {parallelSorted && (
        <div data-testid="parallel-panel">
          <div className="mb-3 flex items-center gap-2">
            <span className="text-[13px] font-[650] text-[var(--color-text-1)]">并行编排</span>
            <Badge color="role-design">{parallelSorted.length} features</Badge>
          </div>
          <div className="grid grid-cols-2 gap-3">
            {parallelSorted.map((f) => (
              <FeatureCard key={f.feature} s={f} />
            ))}
          </div>
        </div>
      )}

      {/* Serial fallback (no parallel run). */}
      {!parallelSorted && present === false && (
        <div className="text-sm text-[var(--color-text-3)] mt-8 text-center">
          orchestrate 尚未运行
        </div>
      )}

      {!parallelSorted && present === true && status && status.escalated && (
        <div
          data-testid="escalated-banner"
          className="rounded-lg border border-[color-mix(in_srgb,var(--color-danger)_30%,transparent)] bg-[color-mix(in_srgb,var(--color-danger)_10%,transparent)] p-4 mb-4"
        >
          <div className="text-sm font-medium text-[var(--color-danger)] mb-1">
            编排已升级 — 需人工介入
          </div>
          {status.reason && (
            <div className="text-xs text-[var(--color-danger)] opacity-80">
              {status.reason}
            </div>
          )}
        </div>
      )}

      {!parallelSorted && present === true && status && status.done && (
        <div className="text-sm text-[var(--color-success)] mt-8 text-center">
          已收工 / 全部交付
        </div>
      )}

      {!parallelSorted && present === true && status && !status.done && !status.escalated && (
        <div data-testid="running-panel" className="space-y-3">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Feature" value={status.feature} />
            <Field label="Task" value={status.task} />
            <Field label="Seat" value={status.seat} />
            <Field label="Action" value={status.action} />
            <Field label="Phase" value={status.phase} />
            <Field label="Iter" value={String(status.iter)} />
          </div>
          <div className="flex items-center gap-2">
            <span className="text-xs text-[var(--color-text-3)]">进度</span>
            <Badge color="success">
              {status.accepted}/{status.total}
            </Badge>
          </div>
          <div className="text-[10px] text-[var(--color-text-3)]">
            updated: {status.updated_at}
          </div>
        </div>
      )}
    </div>
  );
}

// FeatureCard is one feature's status in the parallel grid: id + phase + progress,
// tinted by state (escalated=danger, done=success, else running).
function FeatureCard({ s }: { s: OrchestrateStatus }) {
  const tone = s.escalated ? "danger" : s.done ? "success" : "role-design";
  const phase = s.escalated ? "升级" : s.done ? "done" : s.phase;
  return (
    <div
      data-testid="parallel-feature"
      className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5 hover-lift"
    >
      <div className="flex items-center justify-between gap-2">
        <span className="mono text-[12px] font-medium text-[var(--color-text-1)]">{s.feature}</span>
        <Badge color={tone}>{phase}</Badge>
      </div>
      <div className="mt-1.5 flex items-center gap-2 text-[11px] text-[var(--color-text-2)]">
        {s.task && <span className="mono">{s.task}</span>}
        {s.seat && <span className="text-[var(--color-text-3)]">{s.seat}</span>}
      </div>
      {!s.done && !s.escalated && (
        <div className="mt-1.5">
          <Badge color="success">
            {s.accepted}/{s.total}
          </Badge>
        </div>
      )}
      {s.escalated && s.reason && (
        <div className="mt-1.5 text-[10.5px] text-[var(--color-danger)] [overflow-wrap:anywhere]">{s.reason}</div>
      )}
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-[var(--color-border-subtle)] bg-[rgba(255,255,255,.02)] px-3 py-2">
      <div className="text-[10px] text-[var(--color-text-3)]">{label}</div>
      <div className="text-sm text-[var(--color-text-1)] mono">{value || "—"}</div>
    </div>
  );
}
