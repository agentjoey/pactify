import { useEffect, useRef, useState } from "react";
import type { OrchestrateStatus, Seat, PactEvent } from "../lib/types";
import {
  getOrchestrateStatus,
  getParallelOrchestrate,
  runOrchestrate,
  resumeOrchestrate,
  shipFeature,
  getDiff,
} from "../lib/api";
import { Badge } from "./ui/Badge";
import { Button } from "./ui/Button";
import { Modal } from "./ui/Modal";
import { Input } from "./ui/Input";
import { Textarea } from "./ui/Textarea";

export function LiveOrchestrate({
  project,
  refreshTick,
  author,
  agents,
  events = [],
  onNotify,
}: {
  project: string;
  refreshTick: number;
  author: boolean;
  agents: Seat[];
  events?: PactEvent[];
  onNotify?: (message: string, kind?: "error") => void;
}) {
  const [status, setStatus] = useState<OrchestrateStatus | null>(null);
  const [present, setPresent] = useState<boolean | null>(null);
  // Parallel run: one status per concurrent feature. When present, it takes over
  // the view (a parallel run is the richer signal); otherwise we fall back to the
  // single serial status.json.
  const [parallel, setParallel] = useState<OrchestrateStatus[] | null>(null);
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);
  const [resuming, setResuming] = useState(false);
  const [shipping, setShipping] = useState(false);
  const [diffText, setDiffText] = useState("");
  const [diffOpen, setDiffOpen] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);
  const [shipOpen, setShipOpen] = useState(false);
  const [shipTitle, setShipTitle] = useState("");
  const [shipBody, setShipBody] = useState("");
  const [shipHead, setShipHead] = useState("");
  const [shipResult, setShipResult] = useState<{ prUrl?: string; error?: string } | null>(null);
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

  const rosterComplete = agents.length >= 2;
  const canRun = author && rosterComplete && !running && !resuming;

  async function handleRun() {
    if (!canRun) return;
    setRunning(true);
    try {
      await runOrchestrate(project);
      onNotify?.("Orchestrate run started");
      load();
    } catch (e) {
      onNotify?.(e instanceof Error ? e.message : "Run failed", "error");
    } finally {
      setRunning(false);
    }
  }

  async function handleResume() {
    if (!author || resuming || running) return;
    setResuming(true);
    try {
      await resumeOrchestrate(project);
      onNotify?.("Orchestrate resumed");
      setDiffOpen(false);
      load();
    } catch (e) {
      onNotify?.(e instanceof Error ? e.message : "Resume failed", "error");
    } finally {
      setResuming(false);
    }
  }

  async function handleViewDiff() {
    setLoadingDiff(true);
    setDiffOpen(true);
    try {
      const r = await getDiff(project);
      setDiffText(r.diff);
    } catch (e) {
      setDiffText(e instanceof Error ? e.message : "Failed to load diff");
    } finally {
      setLoadingDiff(false);
    }
  }

  async function handleShip() {
    if (!author || shipping) return;
    if (!shipTitle || !shipHead) {
      setShipResult({ error: "Head branch and title are required" });
      return;
    }
    setShipping(true);
    try {
      const r = await shipFeature(project, { pr: true, head: shipHead, title: shipTitle, body: shipBody });
      setShipResult({ prUrl: r.pr_url });
      onNotify?.(r.pr_url ? `PR opened: ${r.pr_url}` : "Pushed");
    } catch (e) {
      const msg = e instanceof Error ? e.message : "Ship failed";
      setShipResult({ error: msg });
      onNotify?.(msg, "error");
    } finally {
      setShipping(false);
    }
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
      className="flex flex-1 overflow-hidden view-enter"
    >
      <div className="flex-1 overflow-y-auto p-5">
      {error && (
        <div className="text-xs text-[var(--color-danger)] mb-3">{error}</div>
      )}

      {/* Parallel run: a grid of per-feature cards. */}
      {parallelSorted && (
        <div data-testid="parallel-panel">
          <div className="mb-3 flex items-center gap-2">
            <span className="text-[13px] font-[650] text-[var(--color-text-1)]">Parallel orchestration</span>
            <Badge color="role-design">{parallelSorted.length} features</Badge>
          </div>
          <div className="grid grid-cols-2 gap-3">
            {parallelSorted.map((f, i) => (
              <FeatureCard key={f.feature} s={f} delay={i * 40} />
            ))}
          </div>
        </div>
      )}

      {/* Serial fallback (no parallel run). */}
      {!parallelSorted && present === false && (
        <div className="text-center">
          <div className="text-sm text-[var(--color-text-3)] mt-8 mb-3">
            orchestrate hasn't run yet
          </div>
          {author && !rosterComplete && (
            <div className="text-xs text-[var(--color-warn)] mb-3">
              Wire at least two seats before running
            </div>
          )}
          <Button size="sm" loading={running} disabled={!canRun} onClick={handleRun}>
            Run
          </Button>
        </div>
      )}

      {!parallelSorted && present === true && status && status.escalated && (
        <>
          <div
            data-testid="escalated-banner"
            className="rounded-lg border border-[color-mix(in_srgb,var(--color-danger)_30%,transparent)] bg-[color-mix(in_srgb,var(--color-danger)_10%,transparent)] p-4 mb-4"
          >
            <div className="text-sm font-medium text-[var(--color-danger)] mb-1">
              Orchestration escalated — needs manual intervention
            </div>
            {status.reason && (
              <div className="text-xs text-[var(--color-danger)] opacity-80">
                {status.reason}
              </div>
            )}
          </div>
          {author && (
            <div className="mb-4 flex items-center gap-2">
              <Button size="sm" loading={resuming} onClick={handleResume}>
                Resume
              </Button>
              <Button size="sm" variant="ghost" loading={loadingDiff} onClick={handleViewDiff}>
                View diff
              </Button>
            </div>
          )}
        </>
      )}

      {!parallelSorted && present === true && status && status.done && (
        <div className="text-center">
          <div className="text-sm text-[var(--color-success)] mt-8 mb-3">
            Wrapped up / all delivered
          </div>
          {author && (
            <>
              <Button size="sm" loading={shipping} onClick={() => { setShipOpen(true); setShipResult(null); }}>
                Ship
              </Button>
              {shipResult?.prUrl && (
                <div className="mt-3 text-xs text-[var(--color-text-2)]">
                  PR: <a href={shipResult.prUrl} target="_blank" rel="noreferrer" className="text-[var(--color-role-design)] underline">{shipResult.prUrl}</a>
                </div>
              )}
            </>
          )}
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
            <span className="text-xs text-[var(--color-text-3)]">Progress</span>
            <Badge color="success">
              {status.accepted}/{status.total}
            </Badge>
          </div>
          <div className="text-[10px] text-[var(--color-text-3)]">
            updated: {status.updated_at}
          </div>
        </div>
      )}

      {diffOpen && (
        <Modal title="Working diff" onClose={() => setDiffOpen(false)} width="720px">
          {loadingDiff ? (
            <div className="text-xs text-[var(--color-text-3)]">Loading diff…</div>
          ) : (
            <pre className="max-h-[60vh] overflow-auto rounded-md bg-[var(--color-bg-inset)] p-3 text-[11px] text-[var(--color-text-1)]">
              {diffText || "(no diff)"}
            </pre>
          )}
        </Modal>
      )}

      {shipOpen && (
        <Modal
          title="Ship feature"
          onClose={() => setShipOpen(false)}
          footer={
            <>
              <Button size="sm" loading={shipping} onClick={handleShip}>Open PR</Button>
              <Button size="sm" variant="ghost" onClick={() => setShipOpen(false)}>Cancel</Button>
            </>
          }
        >
          <div className="flex flex-col gap-3">
            {shipResult?.error && (
              <div className="text-xs text-[var(--color-danger)]">{shipResult.error}</div>
            )}
            <div>
              <label className="mb-1 block text-[11px] text-[var(--color-text-3)]">Head branch</label>
              <Input value={shipHead} onChange={(e) => setShipHead(e.target.value)} placeholder="feat-xyz" />
            </div>
            <div>
              <label className="mb-1 block text-[11px] text-[var(--color-text-3)]">PR title</label>
              <Input value={shipTitle} onChange={(e) => setShipTitle(e.target.value)} placeholder="feat: …" />
            </div>
            <div>
              <label className="mb-1 block text-[11px] text-[var(--color-text-3)]">PR body</label>
              <Textarea value={shipBody} onChange={(e) => setShipBody(e.target.value)} placeholder="" />
            </div>
          </div>
        </Modal>
      )}
      </div>

      <EventStream events={events} agents={agents} />
    </div>
  );
}

// EventStream — the dark-handoff right pane: a colorized terminal of the pact
// log.jsonl events + a per-seat presence footer. Read-only; sources whatever
// events the dashboard already holds (newest at the bottom, like a tail).
function EventStream({ events, agents }: { events: PactEvent[]; agents: Seat[] }) {
  const recent = events.slice(-200);
  const glyph: Record<string, { ch: string; color: string }> = {
    checkpoint: { ch: "$", color: "var(--color-role-dev)" },
    accept: { ch: "✓", color: "var(--color-role-design)" },
    assign: { ch: "·", color: "var(--color-text-3)" },
    join: { ch: "→", color: "var(--color-text-3)" },
    merge: { ch: "✓", color: "var(--color-role-dev)" },
    changes: { ch: "!", color: "var(--color-warn)" },
    escalate: { ch: "⊘", color: "var(--color-danger)" },
  };
  const counts = agents.map((a) => ({
    id: a.id,
    n: events.filter((e) => e.agent_id === a.id).length,
  }));
  return (
    <div
      data-testid="event-stream"
      className="flex w-[392px] flex-none flex-col border-l border-[var(--color-border-subtle)]"
      style={{ background: "#07090d" }}
    >
      <div className="flex items-center gap-2 border-b border-[var(--color-border-subtle)] px-4 py-3">
        <span className="mono text-[10px] uppercase tracking-[1px] text-[var(--color-text-3)]">Event stream</span>
        <span className="topbar-live-dot h-[5px] w-[5px] rounded-full" style={{ background: "var(--color-success)" }} />
        <span className="mono ml-auto text-[10px] text-[var(--color-text-3)]">log.jsonl</span>
      </div>
      <div className="mono flex-1 overflow-y-auto px-4 py-3 text-[11px] leading-[1.85]">
        {recent.length === 0 ? (
          <div className="text-[var(--color-text-3)]">no events yet…</div>
        ) : (
          recent.map((e) => {
            const g = glyph[e.event_type] ?? { ch: "·", color: "var(--color-text-3)" };
            return (
              <div key={e.event_id} className="whitespace-nowrap">
                <span style={{ color: g.color }}>{g.ch}</span>{" "}
                <span style={{ color: "var(--color-role-product)" }}>{e.agent_id}</span>{" "}
                <span style={{ color: "var(--color-text-2)" }}>{e.event_type}</span>{" "}
                {e.task_id && <span style={{ color: "var(--color-text-3)" }}>{e.task_id}</span>}
              </div>
            );
          })
        )}
        <span className="inline-block h-[12px] w-[7px] align-middle" style={{ background: "var(--color-role-dev)", animation: "none" }} />
      </div>
      <div className="flex flex-wrap gap-x-4 gap-y-1 border-t border-[var(--color-border-subtle)] px-4 py-2.5">
        {counts.map((c) => (
          <span key={c.id} className="inline-flex items-center gap-1.5 text-[9.5px] font-medium text-[var(--color-text-2)]">
            <span className="h-[6px] w-[6px] rounded-full" style={{ background: c.n > 0 ? "var(--color-success)" : "var(--color-text-3)" }} />
            <span className="mono">{c.id}</span>
            <span className="mono text-[var(--color-text-3)]">{c.n}</span>
          </span>
        ))}
      </div>
    </div>
  );
}

// FeatureCard is one feature's status in the parallel grid: id + phase + progress,
// tinted by state (escalated=danger, done=success, else running).
function FeatureCard({ s, delay = 0 }: { s: OrchestrateStatus; delay?: number }) {
  const tone = s.escalated ? "danger" : s.done ? "success" : "role-design";
  const phase = s.escalated ? "escalated" : s.done ? "done" : s.phase;
  return (
    <div
      data-testid="parallel-feature"
      style={{ animationDelay: `${delay}ms` }}
      className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2.5 hover-lift fade-rise"
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
    <div className="rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-inset)] px-3 py-2">
      <div className="text-[10px] text-[var(--color-text-3)]">{label}</div>
      <div className="text-sm text-[var(--color-text-1)] mono">{value || "—"}</div>
    </div>
  );
}
