import type { State, BoardTask, PactEvent, Task } from "./types";
import type { MetricItem } from "../components/ui/MetricStrip";

// Board columns shown left→right (shipped is feature-level, not a task column).
export const COLUMNS = ["assigned", "in_progress", "awaiting_review", "accepted", "changes_requested"] as const;
export type Column = (typeof COLUMNS)[number];

export function allTasks(state: State): BoardTask[] {
  const out: BoardTask[] = [];
  for (const f of state.features) for (const t of f.tasks) out.push({ task: t, feature: f.id });
  return out;
}

export function boardColumns(state: State): Record<string, BoardTask[]> {
  const cols: Record<string, BoardTask[]> = {};
  for (const c of COLUMNS) cols[c] = [];
  for (const bt of allTasks(state)) (cols[bt.task.status] ??= []).push(bt);
  return cols;
}

// designBoard buckets tasks into the five dark-handoff columns (distinct from the
// raw-status COLUMNS): Working = in_progress; Review = awaiting_review ∪
// changes_requested (a changes card still reads as amber via its StatusPill);
// Shipped = every task in a feature whose status is "shipped"; Accepted = accepted
// tasks NOT in a shipped feature. A shipped feature's tasks move wholesale to
// Shipped so the column reflects delivered work.
export type DesignColumn = "assigned" | "working" | "review" | "accepted" | "shipped";

export function designBoard(state: State): Record<DesignColumn, BoardTask[]> {
  const cols: Record<DesignColumn, BoardTask[]> = {
    assigned: [],
    working: [],
    review: [],
    accepted: [],
    shipped: [],
  };
  for (const f of state.features) {
    const shippedFeature = f.status === "shipped";
    for (const t of f.tasks) {
      const bt: BoardTask = { task: t, feature: f.id };
      if (shippedFeature) {
        cols.shipped.push(bt);
        continue;
      }
      switch (t.status) {
        case "in_progress":
          cols.working.push(bt);
          break;
        case "awaiting_review":
        case "changes_requested":
          cols.review.push(bt);
          break;
        case "accepted":
          cols.accepted.push(bt);
          break;
        case "shipped":
          cols.shipped.push(bt);
          break;
        default:
          cols.assigned.push(bt);
      }
    }
  }
  return cols;
}

// agentActivity: events authored by agentId, newest first.
export function agentActivity(agentId: string, events: PactEvent[]): PactEvent[] {
  return events.filter((e) => e.agent_id === agentId).slice().reverse();
}

export function lastAction(agentId: string, events: PactEvent[]): PactEvent | undefined {
  return agentActivity(agentId, events)[0];
}

export function findTask(state: State, id: string): BoardTask | undefined {
  return allTasks(state).find((b) => b.task.id === id);
}

// canMergeFeature is the client mirror of the server's merge precondition: a
// feature is mergeable only when it has at least one task and every task is
// accepted. The server is authoritative (422 verbatim on disagreement); this
// just disables the button to avoid an obviously-doomed request.
export function canMergeFeature(state: State, featureId: string): boolean {
  const f = state.features.find((x) => x.id === featureId);
  if (!f || f.tasks.length === 0) return false;
  return f.tasks.every((t) => t.status === "accepted");
}

// --- Stat helpers (T3) ------------------------------------------------------

interface TaskSession {
  tokens?: number;
  cost_usd?: number;
  duration_ms?: number;
  iter?: number;
}

function readSession(taskId: string, events: PactEvent[]): TaskSession {
  let session: TaskSession = {};
  for (const e of events) {
    if (e.task_id !== taskId) continue;
    const p = e.payload;
    if (!p || typeof p !== "object") continue;
    if ("session" in p && p.session && typeof p.session === "object") {
      session = { ...session, ...(p.session as TaskSession) };
    }
    if ("tokens" in p && typeof p.tokens === "number") session.tokens = p.tokens;
    if ("cost_usd" in p && typeof p.cost_usd === "number") session.cost_usd = p.cost_usd;
    if ("duration_ms" in p && typeof p.duration_ms === "number") session.duration_ms = p.duration_ms;
    if ("iter" in p && typeof p.iter === "number") session.iter = p.iter;
  }
  return session;
}

function parseTs(ts: string): number {
  const n = Date.parse(ts);
  return Number.isNaN(n) ? 0 : n;
}

// taskTokens returns a task's token count from its session metadata (0 when
// none recorded). Exposed so the board's feature chips and seated rollup can sum
// token volume without re-deriving the whole MetricStrip.
export function taskTokens(taskId: string, events: PactEvent[]): number {
  return readSession(taskId, events).tokens ?? 0;
}

// fmtDuration renders milliseconds as a compact "3m02s" / "12s" / "1h04m".
export function fmtDuration(ms: number): string {
  const sec = Math.floor(ms / 1000);
  const h = Math.floor(sec / 3600);
  const m = Math.floor((sec % 3600) / 60);
  const s = sec % 60;
  if (h > 0) return `${h}h${m.toString().padStart(2, "0")}m`;
  if (m > 0) return `${m}m${s.toString().padStart(2, "0")}s`;
  return `${s}s`;
}

// fmtTokens renders a token count as "12.4k" / "38k" / "940".
export function fmtTokens(n: number): string {
  if (n < 1000) return String(n);
  return `${(n / 1000).toFixed(1).replace(".0", "")}k`;
}

// fmtCost renders a USD cost as "~$0.21".
export function fmtCost(usd: number): string {
  return `~$${usd.toFixed(2)}`;
}

// taskRuntimeMs measures the span from the task's first in_progress event to
// its accept event (accepted) or now (everything else). Returns 0 when no
// in_progress event has been recorded yet.
export function taskRuntimeMs(task: Task, events: PactEvent[], nowMs: number = Date.now()): number {
  const startEv = events.find((e) => e.task_id === task.id && e.event_type === "in_progress");
  if (!startEv) return 0;
  const start = parseTs(startEv.ts);
  if (start === 0) return 0;
  let end = nowMs;
  if (task.status === "accepted") {
    const acceptEv = events.find((e) => e.task_id === task.id && e.event_type === "accept");
    if (acceptEv) {
      const t = parseTs(acceptEv.ts);
      if (t > 0) end = t;
    }
  }
  return Math.max(0, end - start);
}

// taskMetrics builds the compact RUN / TOK / ×iter strip for a task. Values are
// live (blue) while in_progress, italic estimates while assigned/draft, and
// neutral once accepted/shipped. Tokens and iteration come from session metadata
// when present; otherwise they fall back to "—" or the checkpoint count.
export function taskMetrics(task: Task, events: PactEvent[], nowMs: number = Date.now()): MetricItem[] {
  const session = readSession(task.id, events);
  const runtime = taskRuntimeMs(task, events, nowMs);
  const live = task.status === "in_progress";
  const est = task.status === "assigned" || task.status === "draft";

  const runMs = est ? (session.duration_ms ?? runtime) : runtime;
  const runValue = est ? `~${fmtDuration(runMs)}` : fmtDuration(runMs);

  let tokValue: string;
  if (session.tokens != null) {
    tokValue = est ? `~${fmtTokens(session.tokens)}` : fmtTokens(session.tokens);
  } else {
    tokValue = est ? "~—" : "—";
  }

  const checkpoints = events.filter((e) => e.task_id === task.id && e.event_type === "checkpoint").length;
  const iter = session.iter ?? Math.max(1, checkpoints);

  return [
    { label: "RUN", value: runValue, live, est },
    { label: "TOK", value: tokValue, live, est },
    { label: "", value: `×${iter}`, live, est },
  ];
}
