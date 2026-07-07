import { useEffect, useMemo, useRef, useState } from "react";
import type { OrchestrateStatus, PactEvent, State, Feature, Task } from "../../lib/types";
import { useDataSource } from "../../lib/datasource";
import { AgentTerminal } from "./AgentTerminal";
import { taskTokens, fmtTokens, canMergeFeature } from "../../lib/derive";
import { Button } from "../ui/Button";
import { Modal } from "../ui/Modal";
import { Input } from "../ui/Input";
import { Textarea } from "../ui/Textarea";

// RunRail — the Board's run banner (formerly the Live view's lane column,
// PR2 of the views consolidation): an aggregate run-control strip + one lane
// per driver-touched feature (task pipeline, the five-action ReviewGate when a
// hard gate fails, an expandable per-task agent terminal). Renders NOTHING when
// orchestrate has no activity — the Board stays clean. Everything is sourced
// from real state/events; controls with no backend yet (Pause/Stop) are
// intentionally omitted, not faked.
export function RunRail({
  project,
  state,
  refreshTick,
  author,
  events = [],
  onNotify,
}: {
  project: string;
  state: State;
  refreshTick: number;
  author: boolean;
  events?: PactEvent[];
  onNotify?: (message: string, kind?: "error") => void;
}) {
  const [status, setStatus] = useState<OrchestrateStatus | null>(null);
  const [present, setPresent] = useState<boolean | null>(null);
  const [parallel, setParallel] = useState<OrchestrateStatus[] | null>(null);
  const [error, setError] = useState("");
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
  const [gateBusy, setGateBusy] = useState(false);
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


  async function handleResume() {
    if (!author || !canWrite || resuming || !src.resumeOrchestrate) return;
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

  // canAct gates the Review Gate's pact-verb decisions (Reject→rework, Approve
  // merge). These are writes (accept/changes/merge), not orchestrate drives, so
  // they gate on canWrite + verb presence — the relay source supports them too.
  const canAct = author && src.capabilities.canWrite && typeof src.verb === "function";

  // handleReject — the human requests changes on every awaiting-review task of a
  // paused feature, threading their feedback into the reason so it reaches the
  // owner's next briefing (structured round-trip, still self-driving). Reopens
  // the tasks for rework and lets orchestrate pick them up on resume.
  async function handleReject(feature: Feature, feedback: string) {
    if (!canAct || gateBusy) return;
    const awaiting = feature.tasks.filter((t) => t.status === "awaiting_review");
    if (awaiting.length === 0) {
      onNotify?.("No awaiting-review task to reject on this feature", "error");
      return;
    }
    setGateBusy(true);
    try {
      for (const t of awaiting) {
        await src.verb!(project, "changes", { task: t.id, reason: feedback });
      }
      onNotify?.(`Requested changes on ${awaiting.length} task(s) — rework queued`);
      load();
    } catch (e) {
      onNotify?.(e instanceof Error ? e.message : "Reject failed", "error");
    } finally {
      setGateBusy(false);
    }
  }

  // handleApproveMerge — the deliberate gate OVERRIDE: the human has reviewed the
  // failed hard gate and decides to ship anyway. Accepts every awaiting-review
  // task (the human acting as reviewer — within the two invariants) then merges
  // the feature. The caller confirms first because this bypasses the failed gate.
  async function handleApproveMerge(feature: Feature) {
    if (!canAct || gateBusy) return;
    setGateBusy(true);
    try {
      for (const t of feature.tasks.filter((t) => t.status === "awaiting_review")) {
        await src.verb!(project, "accept", { task: t.id });
      }
      await src.verb!(project, "merge", { feature: feature.id });
      onNotify?.(`Merged ${feature.id} (gate overridden by human approval)`);
      load();
    } catch (e) {
      onNotify?.(e instanceof Error ? e.message : "Approve/merge failed", "error");
    } finally {
      setGateBusy(false);
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

  // Lanes = driver-touched features (a live or escalated run status names
  // them), escalated first. Everything else already lives on the Board columns.
  const lanes = useMemo(() => {
    const active = state.features.filter((f) => f.status !== "shipped" && osFor(f.id) != null);
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

  // Nothing to show: hosted sources can't query run status, and an idle local
  // driver means the Board columns already tell the whole story.
  if (!canQueryStatus) return null;
  if (!hasActivity) return null;

  return (
    <div
      data-testid="run-rail"
      aria-label="orchestrate run rail"
      className="shrink-0 overflow-y-auto border-b border-[var(--color-border-subtle)]"
      style={{ maxHeight: "42vh", padding: "14px 22px 4px", background: "var(--color-bg-page)" }}
    >
        {error && <div className="mb-3 text-xs text-[var(--color-danger)]">{error}</div>}

        {hasActivity && (
          <RunControl
            featureCount={lanes.length}
            concurrency={parallel?.length ?? null}
            iter={status?.iter ?? null}
            phase={status?.phase ?? null}
            fixRound={status?.fix_round ?? null}
            fixMax={status?.fix_max ?? null}
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
            onReject={handleReject}
            onApproveMerge={handleApproveMerge}
            gateBusy={gateBusy}
            canAct={canAct}
            expanded={expanded === f.id}
            onToggle={() => setExpanded(expanded === f.id ? null : f.id)}
            project={project}
            canWrite={canWrite}
            canDiff={canDiff}
          />
        ))}


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
  featureCount, concurrency, iter, phase, fixRound, fixMax, tok, accepted, total, escalated, done, author, resuming, onResume, onShip, canWrite, canShip,
}: {
  featureCount: number; concurrency: number | null; iter: number | null;
  phase: string | null; fixRound: number | null; fixMax: number | null; tok: number;
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
      {phase === "fixing" && (
        <span data-testid="fixing-indicator" className="text-[11.5px] font-medium text-[var(--color-warn)]">
          修复中 {fixRound ?? 0}/{fixMax ?? 0}
        </span>
      )}
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
  feature, os, events, state, author, resuming, loadingDiff, onResume, onDiff, onReject, onApproveMerge, gateBusy, canAct, expanded, onToggle, project, canWrite, canDiff,
}: {
  feature: Feature; os: OrchestrateStatus | null; events: PactEvent[]; state: State;
  author: boolean; resuming: boolean; loadingDiff: boolean; onResume: () => void; onDiff: () => void;
  onReject: (f: Feature, feedback: string) => void; onApproveMerge: (f: Feature) => void; gateBusy: boolean; canAct: boolean;
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
          onReject={(fb) => onReject(feature, fb)}
          onApproveMerge={() => onApproveMerge(feature)}
          gateBusy={gateBusy}
          canAct={canAct}
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
// fails (design: Pactify Live.dc.html, five actions). All five are backed:
// See diff + Resume run drive the local orchestrator; Reject→rework requests
// changes with the human's feedback (→ owner's next briefing); Approve merge is
// the deliberate gate OVERRIDE (accept awaiting tasks + merge, confirmed); Take
// over reveals the manual-drive commands (no persistent-attach backend yet —
// honest, not faked).
function ReviewGate({ reason, author, resuming, loadingDiff, onResume, onDiff, onReject, onApproveMerge, gateBusy, canAct, canWrite, canDiff }: {
  reason?: string; author: boolean; resuming: boolean; loadingDiff: boolean;
  onResume: () => void; onDiff: () => void; onReject: (feedback: string) => void; onApproveMerge: () => void;
  gateBusy: boolean; canAct: boolean; canWrite: boolean; canDiff: boolean;
}) {
  const [rejectOpen, setRejectOpen] = useState(false);
  const [feedback, setFeedback] = useState("");
  const [takeoverOpen, setTakeoverOpen] = useState(false);
  const [confirmMerge, setConfirmMerge] = useState(false);
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
        <>
          <div className="flex flex-wrap items-center gap-[9px]">
            {canDiff && <Button size="sm" variant="ghost" loading={loadingDiff} onClick={onDiff}>See diff</Button>}
            <Button size="sm" variant="ghost" disabled={!canAct || gateBusy} title={canAct ? undefined : "Needs write access"} onClick={() => { setRejectOpen((v) => !v); setConfirmMerge(false); setTakeoverOpen(false); }}>Reject → rework</Button>
            {!confirmMerge ? (
              <Button size="sm" variant="ghost" disabled={!canAct || gateBusy} title={canAct ? undefined : "Needs write access"} onClick={() => { setConfirmMerge(true); setRejectOpen(false); setTakeoverOpen(false); }}>Approve merge</Button>
            ) : (
              <Button size="sm" loading={gateBusy} data-testid="approve-merge-confirm" onClick={onApproveMerge} style={{ background: "var(--color-danger)" }}>Override gate & merge ▸</Button>
            )}
            <Button size="sm" variant="ghost" onClick={() => { setTakeoverOpen((v) => !v); setRejectOpen(false); setConfirmMerge(false); }}>Take over</Button>
            <Button size="sm" loading={resuming} disabled={!canWrite} title={canWrite ? undefined : "Remote control needs U3"} onClick={onResume}>Resume run ▸</Button>
          </div>

          {confirmMerge && (
            <p className="mt-2 text-[10.5px] leading-[1.5] text-[var(--color-warn)]">
              This overrides the failed hard gate: it accepts the awaiting task(s) as you (the reviewer) and merges. Click again to confirm.
            </p>
          )}

          {rejectOpen && (
            <div className="mt-3">
              <Textarea
                data-testid="reject-feedback"
                value={feedback}
                onChange={(e) => setFeedback(e.target.value)}
                placeholder="What needs to change? This feedback goes into the owner's next briefing."
              />
              <div className="mt-2 flex items-center gap-2">
                <Button size="sm" loading={gateBusy} disabled={!feedback.trim()} onClick={() => { onReject(feedback.trim()); setRejectOpen(false); setFeedback(""); }}>Send back for rework</Button>
                <Button size="sm" variant="ghost" onClick={() => setRejectOpen(false)}>Cancel</Button>
              </div>
            </div>
          )}

          {takeoverOpen && (
            <div className="mt-3 rounded-[8px] bg-[var(--color-bg-inset)] p-3 text-[10.5px] leading-[1.7] text-[var(--color-text-2)]">
              <p className="mb-1 font-medium text-[var(--color-text-1)]">Drive this feature by hand:</p>
              <pre className="m-0 whitespace-pre-wrap mono text-[10.5px] text-[var(--color-text-1)]">{`# in the repo:
pactify status
pactify checkpoint <task>      # you finish the work
pactify accept <task>          # or: pactify changes <task> "…"
pactify merge <feature>`}</pre>
              <p className="mt-1 text-[var(--color-text-3)]">Then Resume run to hand control back to the orchestrator.</p>
            </div>
          )}
        </>
      )}
    </div>
  );
}
