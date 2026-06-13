import { useEffect, useRef, useState } from "react";
import type { OrchestrateStatus } from "../lib/types";
import { getOrchestrateStatus } from "../lib/api";
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
  const [error, setError] = useState("");
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);

  function load() {
    if (!project) return;
    getOrchestrateStatus(project)
      .then((r) => {
        setPresent(r.present);
        setStatus(r.status ?? null);
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

  return (
    <div
      data-testid="live-orchestrate"
      aria-label="orchestrate live view"
      className="flex-1 overflow-y-auto p-5"
    >
      {error && (
        <div className="text-xs text-[var(--color-danger)] mb-3">{error}</div>
      )}

      {present === false && (
        <div className="text-sm text-[var(--color-text-3)] mt-8 text-center">
          orchestrate 尚未运行
        </div>
      )}

      {present === true && status && status.escalated && (
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

      {present === true && status && status.done && (
        <div className="text-sm text-[var(--color-success)] mt-8 text-center">
          已收工 / 全部交付
        </div>
      )}

      {present === true && status && !status.done && !status.escalated && (
        <div
          data-testid="running-panel"
          className="space-y-3"
        >
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

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-md border border-[var(--color-border-subtle)] bg-[rgba(255,255,255,.02)] px-3 py-2">
      <div className="text-[10px] text-[var(--color-text-3)]">{label}</div>
      <div className="text-sm text-[var(--color-text-1)] mono">{value || "—"}</div>
    </div>
  );
}
