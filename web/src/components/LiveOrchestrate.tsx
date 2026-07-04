import { useEffect, useMemo, useRef, useState } from "react";
import type { OrchestrateStatus, Seat, PactEvent, State, Feature, Task } from "../lib/types";
import { useDataSource } from "../lib/datasource";
import { AgentTerminal } from "./live/AgentTerminal";
import { taskTokens, fmtTokens, canMergeFeature } from "../lib/derive";
import { Button } from "./ui/Button";
import { Modal } from "./ui/Modal";
import { Input } from "./ui/Input";
import { Textarea } from "./ui/Textarea";

// LiveOrchestrate — the dark-handoff Live view (designs/Pactify Live.dc.html):
// a left column of per-feature *lanes* (run-control strip + task pipeline +
// review gate when a hard gate fails) and a right *event stream* terminal with a
// seat-presence footer. Everything is sourced from real state/events — per-task
// tokens via derive.taskTokens, pipeline from state.features[].tasks[]. Controls
// with no backend yet (Pause/Stop, cost) are intentionally omitted, not faked.
export function LiveOrchestrate({
  project,
  state,
  refreshTick,
  author,
  agents,
  events = [],
  onNotify,
}: {
  project: string;
  state: State;
  refreshTick: number;
  author: boolean;
  agents: Seat[];
  events?: PactEvent[];
  onNotify?: (message: string, kind?: "error") => void;
}) {
  const [status, setStatus] = useState<OrchestrateStatus | null>(null);
  const [present, setPresent] = useState<boolean | null>(null);
  const [parallel, setParallel] = useState<OrchestrateStatus[] | null>(null);
  const [error, setError] = useState("");
  const [running, setRunning] = useState(false);
  const [resuming, setResuming] = useState(false);
  const [shipping, setShipping] = useState(false);
  const [expanded, setExpanded] = useState<string | null>(null);
  const [diffText, setDiffText] = useState("");
  const [diffOpen, setDiffOpen] = useState(false);
  const [loadingDiff, setLoadingDiff] = useState(false);
  const [shipOpen, setShipOpen] = useState(false);
  const [shipTitle, setShipTitle] = useState("");
  const [shipBody, setShipBody] = useState("");
  const [shipHead, setShipHead] = useState("");
  const [shipResult, setShipResult] = useState<{ prUrl?: string; error?: string } | null>(null);
  const timer = useRef<ReturnType<typeof setInterval> | null>(null);
  const src = useDataSource();
  // Orchestrate (Run/Resume/Ship) needs the drive capability, not just write:
  // the relay source can drive pact verbs (accept/changes) but not orchestrate.
  const canWrite = src.capabilities.canOrchestrate;

  // Hosted (RelaySource) omits the orchestrate READ/ACTION methods — run status,
  // parallel status, working diff, ship — because they read/act on the machine's
  // live driver + git tree, which the zero-knowledge relay doesn't hold yet.
  // Guard every call on method PRESENCE so hosted degrades gracefully (event
  // stream + lanes still render) instead of throwing on a non-null assertion.
  // Local serve implements them all, so local behavior is byte-identical.
  const canQueryStatus =
    typeof src.getOrchestrateStatus === "function" && typeof src.getParallelOrchestrate === "function";
  const canShip = typeof src.shipFeature === "function";
  const canDiff = typeof src.getDiff === "function";

  function load() {
    if (!project) return;
    // Hosted has no run-status source — skip the poll (the note renders instead).
    if (!src.getOrchestrateStatus || !src.getParallelOrchestrate) return;
    Promise.all([src.getOrchestrateStatus(project), src.getParallelOrchestrate(project)])
      .then(([single, par]) => {
        setPresent(single.present);
        setStatus(single.status ?? null);
        setParallel(par.present && par.features && par.features.length > 0 ? par.features : null);
        setError("");
      })
      .catch(() => setError("Failed to load orchestrate status"));
  }

  const rosterComplete = agents.length >= 2;
  const canRun = author && rosterComplete && !running && !resuming && canWrite;

  async function handleRun() {
    if (!canRun || !src.runOrchestrate) return;
    setRunning(true);
    try {
      await src.runOrchestrate(project);
      onNotify?.("Orchestrate run started");
      load();
    } catch (e) {
      onNotify?.(e instanceof Error ? e.message : "Run failed", "error");
    } finally {
      setRunning(false);
    }
  }

  async function handleResume() {
    if (!author || !canWrite || resuming || running || !src.resumeOrchestrate) return;
    setResuming(true);
    try {
      await src.resumeOrchestrate(project);
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
    if (!src.getDiff) return;
    setLoadingDiff(true);
    setDiffOpen(true);
    try {
      const r = await src.getDiff(project);
      setDiffText(r.diff);
    } catch (e) {
      setDiffText(e instanceof Error ? e.message : "Failed to load diff");
    } finally {
      setLoadingDiff(false);
    }
  }

  async function handleShip() {
    if (!author || !canWrite || shipping || !src.shipFeature) return;
    if (!shipTitle || !shipHead) {
      setShipResult({ error: "Head branch and title are required" });
      return;
    }
    setShipping(true);
    try {
      const r = await src.shipFeature(project, { pr: true, head: shipHead, title: shipTitle, body: shipBody });
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

  // Per-feature orchestrate status: prefer the parallel entry, fall back to the
  // single serial status when it names this feature.
  const osFor = (fid: string): OrchestrateStatus | null =>
    parallel?.find((p) => p.feature === fid) ?? (status?.feature === fid ? status : null);

  // Lanes = features with live work (not yet shipped), escalated first.
  const lanes = useMemo(() => {
    const active = state.features.filter((f) => f.status !== "shipped");
    return [...active].sort((a, b) => {
      const ae = osFor(a.id)?.escalated ? 0 : 1;
      const be = osFor(b.id)?.escalated ? 0 : 1;
      return ae - be || a.id.localeCompare(b.id);
    });
  }, [state.features, parallel, status]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => {
    if (expanded === null) {
      const firstWorking = lanes.find((f) => { const o = osFor(f.id); return o && !o.done && !o.escalated; });
      if (firstWorking) setExpanded(firstWorking.id);
    }
  }, [lanes]); // eslint-disable-line react-hooks/exhaustive-deps

  const totals = useMemo(() => {
    let tok = 0, accepted = 0, total = 0;
    for (const f of lanes) {
      for (const t of f.tasks) {
        tok += taskTokens(t.id, events);
        total += 1;
        if (t.status === "accepted" || t.status === "shipped") accepted += 1;
      }
    }
    return { tok, accepted, total };
  }, [lanes, events]);

  const anyEscalated = lanes.some((f) => osFor(f.id)?.escalated);
  const allDone = present === true && status?.done === true;
  const hasActivity = lanes.length > 0 || present === true;

  return (
    <div
      data-testid="live-orchestrate"
      aria-label="orchestrate live view"
      className="flex flex-1 overflow-hidden view-enter"
    >
      <div
        className="flex-1 min-w-0 overflow-y-auto"
        style={{
          padding: "18px 22px",
          background:
            "radial-gradient(900px 420px at 30% -10%, color-mix(in srgb, var(--color-role-design) 6%, transparent), transparent 60%), var(--color-bg-page)",
        }}
      >
        {error && <div className="mb-3 text-xs text-[var(--color-danger)]">{error}</div>}

        {!canQueryStatus && (
          <div
            data-testid="hosted-status-note"
            className="mb-3 rounded-md border border-[var(--color-border-subtle)] bg-[var(--color-bg-surface)] px-3 py-2 text-xs text-[var(--color-text-3)]"
          >
            Run status isn't available in hosted mode yet — the board and event stream stay live.
          </div>
        )}

        {!hasActivity && present === false && (
          <div className="mt-10 text-center">
            <div className="mb-3 text-sm text-[var(--color-text-3)]">orchestrate hasn't run yet</div>
            {author && !rosterComplete && (
              <div className="mb-3 text-xs text-[var(--color-warn)]">Wire at least two seats before running</div>
            )}
            <Button size="sm" loading={running} disabled={!canRun} title={canWrite ? undefined : "Remote control needs U3"} onClick={handleRun}>Run</Button>
          </div>
        )}

        {hasActivity && (
          <RunControl
            featureCount={lanes.length}
            concurrency={parallel?.length ?? null}
            iter={status?.iter ?? null}
            tok={totals.tok}
            accepted={totals.accepted}
            total={totals.total}
            escalated={anyEscalated}
            done={allDone}
            author={author}
            resuming={resuming}
            onResume={handleResume}
            onShip={() => { setShipOpen(true); setShipResult(null); }}
            canWrite={canWrite}
            canShip={canShip}
          />
        )}

        {shipResult?.prUrl && (
          <div className="mb-4 text-xs text-[var(--color-text-2)]">
            PR: <a href={shipResult.prUrl} target="_blank" rel="noreferrer" className="text-[var(--color-role-design)] underline">{shipResult.prUrl}</a>
          </div>
        )}

        {lanes.map((f) => (
          <FeatureLane
            key={f.id}
            feature={f}
            os={osFor(f.id)}
            events={events}
            state={state}
            author={author}
            resuming={resuming}
            loadingDiff={loadingDiff}
            onResume={handleResume}
            onDiff={handleViewDiff}
            expanded={expanded === f.id}
            onToggle={() => setExpanded(expanded === f.id ? null : f.id)}
            project={project}
            canWrite={canWrite}
            canDiff={canDiff}
          />
        ))}
      </div>

      <EventStream events={events} agents={agents} state={state} />

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
            {shipResult?.error && <div className="text-xs text-[var(--color-danger)]">{shipResult.error}</div>}
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
  );
}

// RunControl — the top strip: live orchestrating pulse + run-wide meta (features,
// concurrency, iter, tokens) + accepted progress bar + Resume (when a gate is
// open) / Ship (when all delivered). Pause/Stop are omitted — no backend yet.
function RunControl({
  featureCount, concurrency, iter, tok, accepted, total, escalated, done, author, resuming, onResume, onShip, canWrite, canShip,
}: {
  featureCount: number; concurrency: number | null; iter: number | null; tok: number;
  accepted: number; total: number; escalated: boolean; done: boolean;
  author: boolean; resuming: boolean; onResume: () => void; onShip: () => void; canWrite: boolean; canShip: boolean;
}) {
  const pct = total > 0 ? Math.round((accepted / total) * 100) : 0;
  const label = done ? "Delivered" : escalated ? "Paused" : "Orchestrating";
  const dot = done ? "var(--color-success)" : escalated ? "var(--color-danger)" : "var(--color-role-dev)";
  const meta = [
    `${featureCount} ${featureCount === 1 ? "feature" : "features"}`,
    concurrency && concurrency > 1 ? `concurrency ${concurrency}` : null,
    iter != null ? `iter ${iter}` : null,
    tok > 0 ? `${fmtTokens(tok)} tok` : null,
  ].filter(Boolean).join(" · ");
  return (
    <div
      className="mb-[18px] flex items-center gap-[14px] rounded-[13px] border border-[var(--color-border-subtle)] px-4 py-[13px]"
      style={{ background: "var(--color-bg-surface)", boxShadow: "0 1px 2px rgba(0,0,0,.4)" }}
    >
      <span className="inline-flex items-center gap-[7px] text-[13px] font-semibold text-[var(--color-text-1)]">
        <span className="status-pill-dot-live h-[7px] w-[7px] rounded-full" style={{ background: dot, boxShadow: `0 0 8px ${dot}` }} />
        {label}
      </span>
      <span className="text-[11.5px] text-[var(--color-text-3)]">{meta}</span>
      <div className="ml-auto flex items-center gap-[10px]">
        <div className="flex items-center gap-2 text-[10.5px] font-medium text-[var(--color-text-2)]">
          <span>{accepted} / {total} accepted</span>
          <span className="block h-[5px] w-[120px] overflow-hidden rounded-[3px]" style={{ background: "color-mix(in srgb, var(--color-text-1) 10%, transparent)" }}>
            <i className="block h-full rounded-[3px]" style={{ width: `${pct}%`, background: "linear-gradient(90deg,var(--color-role-dev),#9fe8be)" }} />
          </span>
        </div>
        {author && escalated && (
          <Button size="sm" loading={resuming} disabled={!canWrite} title={canWrite ? undefined : "Remote control needs U3"} onClick={onResume}>Resume</Button>
        )}
        {author && done && canShip && (
          <Button size="sm" disabled={!canWrite} title={canWrite ? undefined : "Remote control needs U3"} onClick={onShip}>Ship</Button>
        )}
      </div>
    </div>
  );
}

type ChipKind = "done" | "working" | "review" | "changes" | "gate" | "pending";

function chipKindFor(t: Task, os: OrchestrateStatus | null): ChipKind {
  if (t.status === "accepted" || t.status === "shipped") return "done";
  if (os?.escalated && os.task === t.id) return "gate";
  if (t.status === "awaiting_review") return "review";
  if (t.status === "changes_requested") return "changes";
  if (t.status === "in_progress") return "working";
  return "pending";
}

// FeatureLane — one feature's card: header (name/branch/status pill/meta) +
// horizontal task pipeline + (when its hard gate failed) the review gate +
// (when working and expanded) live agent terminal.
function FeatureLane({
  feature, os, events, state, author, resuming, loadingDiff, onResume, onDiff, expanded, onToggle, project, canWrite, canDiff,
}: {
  feature: Feature; os: OrchestrateStatus | null; events: PactEvent[]; state: State;
  author: boolean; resuming: boolean; loadingDiff: boolean; onResume: () => void; onDiff: () => void;
  expanded: boolean; onToggle: () => void; project: string; canWrite: boolean; canDiff: boolean;
}) {
  const gate = !!os?.escalated;
  const accepted = feature.tasks.filter((t) => t.status === "accepted" || t.status === "shipped").length;
  const tok = feature.tasks.reduce((n, t) => n + taskTokens(t.id, events), 0);
  const mergeable = canMergeFeature(state, feature.id);
  const working = !gate && os && !os.done;

  const pill = gate
    ? { txt: "paused · gate", c: "var(--color-danger)" }
    : working
      ? { txt: "working", c: "var(--color-role-design)" }
      : accepted === feature.tasks.length && feature.tasks.length > 0
        ? { txt: "ready", c: "var(--color-role-dev)" }
        : { txt: "queued", c: "var(--color-text-3)" };

  return (
    <div
      data-testid="feature-lane"
      className="mb-4 rounded-[14px] px-4 py-[15px]"
      style={
        gate
          ? {
              border: "1px solid color-mix(in srgb, var(--color-danger) 30%, transparent)",
              background: "linear-gradient(180deg, color-mix(in srgb, var(--color-danger) 5%, transparent), var(--color-bg-surface) 40%)",
              boxShadow: "0 0 0 1px color-mix(in srgb, var(--color-danger) 8%, transparent), 0 8px 22px rgba(0,0,0,.4)",
            }
          : { border: "1px solid var(--color-border-subtle)", background: "var(--color-bg-surface)", boxShadow: "0 1px 2px rgba(0,0,0,.4)" }
      }
    >
      <div className="mb-[14px] flex items-center gap-[11px]">
        <span className="text-[14px] font-[650] leading-[1.1] text-[var(--color-text-1)]">{feature.id}</span>
        <span className="mono text-[10.5px] text-[var(--color-text-3)]">{feature.branch}</span>
        <span
          className="inline-flex items-center gap-[5px] rounded-full px-[9px] py-[3px] text-[10px] font-medium"
          style={{ color: pill.c, background: `color-mix(in srgb, ${pill.c} 12%, transparent)`, border: `1px solid color-mix(in srgb, ${pill.c} 30%, transparent)` }}
        >
          {(gate || working) && <span className="status-pill-dot-live h-[5px] w-[5px] rounded-full" style={{ background: pill.c }} />}
          {pill.txt}
        </span>
        {working && (
          <button onClick={onToggle} className="ml-2 rounded-[6px] border border-[color-mix(in_srgb,var(--color-role-design)_30%,transparent)] px-2 py-[3px] text-[9.5px] text-[var(--color-role-design)]">
            {expanded ? "▴ 收起" : "▾ 看执行"}
          </button>
        )}
        <span className="ml-auto flex items-center gap-2 text-[10px] font-medium text-[var(--color-text-3)]">
          <span className="mono">{tok > 0 ? `${fmtTokens(tok)} tok` : "—"}{os?.iter != null ? ` · ×${os.iter}` : ""}</span>
          <span className="opacity-40">·</span>
          <span>{accepted} / {feature.tasks.length} accepted</span>
        </span>
      </div>

      <div className={`flex items-center ${gate ? "mb-[15px]" : ""}`}>
        {feature.tasks.map((t, i) => (
          <PipeChipWithConnector
            key={t.id}
            first={i === 0}
            prevKind={i > 0 ? chipKindFor(feature.tasks[i - 1], os) : "pending"}
            kind={chipKindFor(t, os)}
            id={t.id}
            tok={taskTokens(t.id, events)}
            seat={os && os.task === t.id ? os.seat : undefined}
          />
        ))}
        <Connector kind={accepted === feature.tasks.length ? "done" : "pending"} />
        <MergeNode active={mergeable} />
      </div>

      {gate && (
        <ReviewGate
          reason={os?.reason}
          author={author}
          resuming={resuming}
          loadingDiff={loadingDiff}
          onResume={onResume}
          onDiff={onDiff}
          canWrite={canWrite}
          canDiff={canDiff}
        />
      )}

      {expanded && working && os?.task && (
        <div className="mt-3">
          <AgentTerminal project={project} task={os.task} seat={os.seat} />
        </div>
      )}
    </div>
  );
}

const CHIP: Record<ChipKind, { c: string; glyph: string; dim?: boolean }> = {
  done: { c: "var(--color-role-dev)", glyph: "✓" },
  working: { c: "var(--color-role-design)", glyph: "" }, // equalizer rendered separately
  review: { c: "var(--color-role-product)", glyph: "◴" },
  changes: { c: "var(--color-warn)", glyph: "!" },
  gate: { c: "var(--color-danger)", glyph: "⊘" },
  pending: { c: "var(--color-text-3)", glyph: "·", dim: true },
};

function PipeChipWithConnector({ first, prevKind, kind, id, tok, seat }: {
  first: boolean; prevKind: ChipKind; kind: ChipKind; id: string; tok: number; seat?: string;
}) {
  return (
    <>
      {!first && <Connector kind={prevKind} />}
      <PipeChip kind={kind} id={id} tok={tok} seat={seat} />
    </>
  );
}

function PipeChip({ kind, id, tok, seat }: { kind: ChipKind; id: string; tok: number; seat?: string }) {
  const m = CHIP[kind];
  const lit = kind === "working" || kind === "gate";
  return (
    <div
      className="flex flex-none items-center gap-[7px] rounded-[9px] px-[10px] py-[7px]"
      style={{
        background: lit ? `color-mix(in srgb, ${m.c} 9%, transparent)` : "var(--color-bg-inset)",
        border: `1px solid color-mix(in srgb, ${m.c} ${lit ? 45 : kind === "done" ? 30 : 12}%, transparent)`,
        boxShadow: lit ? `0 0 14px color-mix(in srgb, ${m.c} 20%, transparent)` : "none",
        opacity: m.dim ? 0.6 : 1,
      }}
    >
      <span
        className="grid h-4 w-4 place-items-center rounded-[5px] mono text-[9px] font-semibold"
        style={{ background: `color-mix(in srgb, ${m.c} 20%, transparent)`, color: m.c }}
      >
        {kind === "working" ? <Equalizer color={m.c} /> : m.glyph}
      </span>
      <span className="mono text-[10px] font-medium" style={{ color: lit ? "var(--color-text-1)" : "var(--color-text-2)" }}>{id}</span>
      {kind === "gate" && <span className="text-[9px]" style={{ color: m.c }}>gate failed</span>}
      {seat && kind === "working" && <span className="text-[9px]" style={{ color: m.c }}>{seat}</span>}
      {tok > 0 && <span className="mono text-[9px]" style={{ color: lit ? m.c : "var(--color-text-3)" }}>{fmtTokens(tok)}</span>}
    </div>
  );
}

function Equalizer({ color }: { color: string }) {
  return (
    <span className="inline-flex h-[8px] items-end gap-[1.5px]" style={{ color }}>
      <i className="live-eq-bar inline-block w-[2px] rounded-[1px]" style={{ height: 3, background: "currentColor" }} />
      <i className="live-eq-bar inline-block w-[2px] rounded-[1px]" style={{ height: 7, background: "currentColor", animationDelay: ".15s" }} />
      <i className="live-eq-bar inline-block w-[2px] rounded-[1px]" style={{ height: 4, background: "currentColor", animationDelay: ".3s" }} />
    </span>
  );
}

function Connector({ kind }: { kind: ChipKind }) {
  const c = kind === "done" ? "color-mix(in srgb, var(--color-role-dev) 40%, transparent)"
    : kind === "working" ? "color-mix(in srgb, var(--color-role-design) 40%, transparent)"
    : "color-mix(in srgb, var(--color-text-1) 10%, transparent)";
  return <span className="h-[2px] w-[26px] flex-none" style={{ background: c }} />;
}

function MergeNode({ active }: { active: boolean }) {
  return (
    <div
      className="flex flex-none items-center rounded-[9px] px-[10px] py-[7px]"
      style={{
        background: "var(--color-bg-inset)",
        border: active ? "1px solid color-mix(in srgb, var(--color-role-dev) 35%, transparent)" : "1px dashed color-mix(in srgb, var(--color-text-1) 14%, transparent)",
        opacity: active ? 1 : 0.5,
      }}
    >
      <span className="mono text-[10px] font-medium" style={{ color: active ? "var(--color-role-dev)" : "var(--color-text-3)" }}>▸ merge</span>
    </div>
  );
}

// ReviewGate — the human-decision panel shown when a feature's hard test gate
// fails. Surfaces the escalation reason and the actions that have a backend
// today (See diff, Resume run). Approve/Reject/Take-over await dedicated APIs.
function ReviewGate({ reason, author, resuming, loadingDiff, onResume, onDiff, canWrite, canDiff }: {
  reason?: string; author: boolean; resuming: boolean; loadingDiff: boolean; onResume: () => void; onDiff: () => void; canWrite: boolean; canDiff: boolean;
}) {
  return (
    <div
      data-testid="review-gate"
      className="rounded-[11px] px-[14px] py-[13px]"
      style={{ background: "var(--color-bg-terminal,#07090d)", border: "1px solid color-mix(in srgb, var(--color-danger) 25%, transparent)" }}
    >
      <div className="mb-[9px] flex items-center gap-2">
        <span className="mono text-[13px] font-semibold text-[var(--color-danger)]">⊘</span>
        <span className="text-[12.5px] font-semibold leading-[1.3] text-[var(--color-text-1)]">Hard test gate failed — human decision required</span>
      </div>
      <pre className="m-0 mb-3 whitespace-pre-wrap mono text-[11px] leading-[1.7] text-[var(--color-text-2)]">
        {reason || "the orchestrator paused this feature for review"}
      </pre>
      {author && (
        <div className="flex flex-wrap items-center gap-[9px]">
          {canDiff && <Button size="sm" variant="ghost" loading={loadingDiff} onClick={onDiff}>See diff</Button>}
          <Button size="sm" loading={resuming} disabled={!canWrite} title={canWrite ? undefined : "Remote control needs U3"} onClick={onResume}>Resume run ▸</Button>
        </div>
      )}
    </div>
  );
}

// EventStream — the right pane: a colorized terminal tail of pact log.jsonl +
// a per-seat presence footer (runs · tokens, attributed from owned tasks).
function EventStream({ events, agents, state }: { events: PactEvent[]; agents: Seat[]; state: State }) {
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
  const seats = agents.map((a) => {
    const runs = events.filter((e) => e.agent_id === a.id && (e.event_type === "checkpoint" || e.event_type === "accept")).length;
    const tok = state.features
      .flatMap((f) => f.tasks)
      .filter((t) => t.owner === a.id)
      .reduce((n, t) => n + taskTokens(t.id, events), 0);
    return { id: a.id, runs, tok };
  });
  return (
    <div
      data-testid="event-stream"
      className="flex w-[392px] flex-none flex-col border-l border-[var(--color-border-subtle)]"
      style={{ background: "var(--color-bg-terminal,#07090d)" }}
    >
      <div className="flex items-center gap-2 border-b border-[var(--color-border-subtle)] px-4 py-3">
        <span className="mono text-[10px] uppercase tracking-[1px] text-[var(--color-text-3)]">Event stream</span>
        <span className="inline-flex items-center gap-[5px] text-[9.5px] font-medium" style={{ color: "var(--color-success)" }}>
          <span className="status-pill-dot-live h-[5px] w-[5px] rounded-full" style={{ background: "var(--color-success)" }} />live
        </span>
        <span className="mono ml-auto text-[10px] text-[var(--color-text-3)]">log.jsonl</span>
      </div>
      <div className="mono flex-1 overflow-y-auto px-4 py-3 text-[11px] leading-[1.85] text-[var(--color-text-2)]">
        {recent.length === 0 ? (
          <div className="text-[var(--color-text-3)]">no events yet…</div>
        ) : (
          recent.map((e) => {
            const g = glyph[e.event_type] ?? { ch: "·", color: "var(--color-text-3)" };
            return (
              <div key={e.event_id} className="whitespace-nowrap">
                <span className="text-[var(--color-text-3)]">{(e.ts || "").slice(11, 19)}</span>{" "}
                <span style={{ color: g.color }}>{g.ch}</span>{" "}
                <span style={{ color: "var(--color-role-product)" }}>{e.agent_id}</span>{" "}
                <span style={{ color: "var(--color-text-2)" }}>{e.event_type}</span>
                {e.task_id && <> <span className="text-[var(--color-text-3)]">{e.task_id}</span></>}
              </div>
            );
          })
        )}
        <span className="mt-1 flex items-center gap-[6px]">
          <span style={{ color: "var(--color-role-dev)" }}>$</span>
          <span className="live-cursor inline-block h-[12px] w-[7px]" style={{ background: "var(--color-role-dev)" }} />
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-x-3 gap-y-1 border-t border-[var(--color-border-subtle)] px-4 py-[11px]">
        <span className="mono text-[9px] text-[var(--color-text-3)]">SEATS</span>
        {seats.map((s) => (
          <span key={s.id} className="inline-flex items-center gap-[5px] text-[9.5px] font-medium text-[var(--color-text-2)]">
            <span className="h-[7px] w-[7px] rounded-full" style={{ background: s.runs > 0 ? "var(--color-success)" : "var(--color-text-3)" }} />
            {s.id} <span className="mono text-[var(--color-text-3)]">{s.runs}·{fmtTokens(s.tok)}</span>
          </span>
        ))}
      </div>
    </div>
  );
}
