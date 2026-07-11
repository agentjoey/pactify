import type { State, BoardTask, PactEvent, Task } from "./types";
import type { MetricItem } from "../components/ui/MetricStrip";
import type { TaskStat, ProjectStats } from "./api";

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

// Notification kinds surfaced in the Board context-header message strip.
export type NotifKind = "awaiting" | "started" | "changes";

export interface BoardNotification {
  kind: NotifKind;
  taskId: string;
  agentId: string;
  ts: number;
}

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
        // No task-level "shipped" case: shipped is a FEATURE status (projection
        // only sets it on features); the shippedFeature branch above covers it.
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

function parseTs(ts: string): number {
  const n = Date.parse(ts);
  return Number.isNaN(n) ? 0 : n;
}

// eventsByTask groups the flat event log by task_id so per-card helpers scan
// only their own slice instead of the whole log. Build once per render (Board
// memoizes on the events array identity).
export function eventsByTask(events: PactEvent[]): Map<string, PactEvent[]> {
  const m = new Map<string, PactEvent[]>();
  for (const e of events) {
    if (!e.task_id) continue;
    const arr = m.get(e.task_id);
    if (arr) arr.push(e);
    else m.set(e.task_id, [e]);
  }
  return m;
}

// deriveNotifications builds the context-header message chips from recent
// noteworthy events: awaiting-review (last checkpoint), started, changes-requested.
// Result is newest-first and capped so the header strip stays compact.
export function deriveNotifications(
  events: PactEvent[],
  state: State,
  nowMs: number,
  cap = 8,
): BoardNotification[] {
  const byTask = eventsByTask(events);
  const out: BoardNotification[] = [];
  for (const f of state.features) {
    for (const t of f.tasks) {
      const evs = byTask.get(t.id) ?? [];
      if (t.status === "awaiting_review") {
        const cp = [...evs].reverse().find((e) => e.event_type === "checkpoint");
        if (cp) out.push({ kind: "awaiting", taskId: t.id, agentId: cp.agent_id, ts: parseTs(cp.ts) });
      } else if (t.status === "changes_requested") {
        const ch = [...evs].reverse().find((e) => e.event_type === "changes_requested");
        if (ch) out.push({ kind: "changes", taskId: t.id, agentId: ch.agent_id, ts: parseTs(ch.ts) });
      } else if (t.status === "in_progress") {
        const st = [...evs].reverse().find((e) => e.event_type === "start");
        if (st) out.push({ kind: "started", taskId: t.id, agentId: st.agent_id, ts: parseTs(st.ts) });
      }
    }
  }
  return out
    .filter((n) => n.ts > 0 && nowMs - n.ts < 24 * 60 * 60 * 1000)
    .sort((a, b) => b.ts - a.ts)
    .slice(0, cap);
}

// fmtRelTime renders a compact "2m" / "5m" / "1h" age for the notification strip.
export function fmtRelTime(msAgo: number): string {
  const sec = Math.max(0, Math.floor(msAgo / 1000));
  if (sec < 60) return "now";
  const min = Math.floor(sec / 60);
  if (min < 60) return `${min}m`;
  const h = Math.floor(min / 60);
  if (h < 24) return `${h}h`;
  return `${Math.floor(h / 24)}d`;
}

// statsByTask indexes a GET /api/projects/{id}/stats response by task id for
// O(1) per-card lookup. Null/undefined (stats not fetched yet) → empty map.
export function statsByTask(stats: ProjectStats | null | undefined): Map<string, TaskStat> {
  const m = new Map<string, TaskStat>();
  for (const t of stats?.tasks ?? []) m.set(t.task_id, t);
  return m;
}

// taskTokens returns a task's token count from the per-task /stats index (0
// when the entry is absent or the backend hasn't attributed tokens yet).
// The legacy PactEvent[] form always returns 0 — pact events never carried
// token counts, so callers still passing the log (LiveOrchestrate) get exactly
// the behavior they always had until they migrate to the stats index.
export function taskTokens(
  taskId: string,
  source: ReadonlyMap<string, TaskStat> | readonly PactEvent[],
): number {
  if (Array.isArray(source)) return 0;
  return (source as ReadonlyMap<string, TaskStat>).get(taskId)?.tokens ?? 0;
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

// taskRuntimeMs measures the span from the task's first `assign` to its terminal
// moment, mirroring the backend stats.durationSec contract exactly:
//   accepted           → the accept event's ts
//   awaiting_review    → the last checkpoint's ts
//   changes_requested  → now (rework is active working time — clock keeps ticking)
//   anything else      → now (ongoing)
// Returns 0 when the task has no assign event yet. `nowMs` is required so callers
// pass ONE per-render Date.now() (purity: same inputs → same output).
//
// NOTE: the pact protocol has NO "in_progress" event — in_progress is a projected
// STATUS, not a logged event; only assign/checkpoint/accept anchor the clock.
export function taskRuntimeMs(task: Task, events: PactEvent[], nowMs: number): number {
  const startEv = events.find((e) => e.task_id === task.id && e.event_type === "assign");
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
  } else if (task.status === "awaiting_review") {
    const checkpoints = events.filter((e) => e.task_id === task.id && e.event_type === "checkpoint");
    const lastCp = checkpoints.length ? parseTs(checkpoints[checkpoints.length - 1].ts) : 0;
    if (lastCp > 0) end = lastCp;
  }
  return Math.max(0, end - start);
}

// Rough estimate for not-yet-run cards (no assign event yet). Derived from the
// task spec/id size so identical inputs stay stable across renders.
function estimateMetrics(task: Task): { duration: string; tokens: string } {
  const chars = (task.spec?.length ?? 0) + (task.id?.length ?? 0);
  const minutes = Math.max(2, Math.min(15, Math.round(chars / 60) + 2));
  const tokens = Math.max(6, Math.min(30, Math.round(chars / 40) + 6));
  return { duration: `~${minutes}m`, tokens: `~${tokens}k tok` };
}

// taskMetrics builds the compact RUN / TOK / ×iter strip for a task. RUN comes
// from the event log (contract above); TOK from the task's /stats entry ("—"
// until the backend attributes tokens — 0 means unknown, not zero spend); iter
// is the checkpoint count (min 1). Values render live (blue) while in_progress.
// `events` should be the task's own slice (see eventsByTask); `nowMs` is the
// caller's per-render clock.
export function taskMetrics(task: Task, events: PactEvent[], nowMs: number, stat?: TaskStat): MetricItem[] {
  const runtime = taskRuntimeMs(task, events, nowMs);
  const live = task.status === "in_progress";

  // Not yet assigned: show an italic estimate instead of a zero runtime.
  if (runtime === 0 && task.status === "assigned") {
    const est = estimateMetrics(task);
    return [
      { label: "est", value: est.duration, est: true },
      { label: "", value: est.tokens, est: true },
    ];
  }

  const tokens = stat?.tokens ?? 0;

  const checkpoints = events.filter((e) => e.task_id === task.id && e.event_type === "checkpoint").length;
  const iter = Math.max(1, checkpoints);

  const items: MetricItem[] = [
    { label: "RUN", value: fmtDuration(runtime), live },
  ];
  if (tokens > 0) {
    items.push({ label: "TOK", value: fmtTokens(tokens), live });
  }
  items.push({ label: "", value: `×${iter}`, live });
  return items;
}

// roleColorVar — a seat's *defining duty* drives its color (moved here from the
// retired lib/canvas.ts; consumed by ops/Seats and any future seat chips).
// Orchestrator (owns the spec, assigns/accepts — Product) is the most senior
// duty, so it wins when present; reviewer (owns the blueprint — Design)
// outranks plain worker; worker (builds/ships — Dev) is the baseline and the
// safe default for an unrecognized or seatless role set.
export function roleColorVar(roles: string[]): string {
  if (roles.includes("orchestrator")) return "--role-product";
  if (roles.includes("reviewer")) return "--role-design";
  if (roles.includes("worker")) return "--role-dev";
  return "--role-dev";
}
