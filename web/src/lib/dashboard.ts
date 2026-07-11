import type { State, Feature, Task, PactEvent, Seat } from "./types";
import type { ProjectStats } from "./api";
import type { OrchestrateStatusResponse } from "./types";
import { eventsByTask, taskRuntimeMs, fmtTokens, fmtCost, fmtRelTime } from "./derive";
import { deriveFlow, liveStates } from "./flowderive";
import type { FlowModel } from "./flowderive";

const MS_PER_DAY = 24 * 60 * 60 * 1000;
const SHIPPED_WINDOW_MS = 7 * MS_PER_DAY;
const COST_PER_TOKEN = 4e-6; // rough $4 / 1M tokens → design 52.1k ≈ ~$0.21

export interface DashboardKPIs {
  activeRun: { count: number; label: string; live: boolean };
  awaitingReview: { count: number; label: string };
  tokensToday: { tokens: string; cost: string; rawTokens: number };
  shipped7d: { count: number; label: string };
}

export function deriveDashboardKPIs(
  state: State,
  stats: ProjectStats | null,
  orchestrateStatus: OrchestrateStatusResponse | null | undefined,
  events: PactEvent[],
  nowMs: number,
): DashboardKPIs {
  const running =
    !!orchestrateStatus?.present &&
    !!orchestrateStatus.status &&
    !orchestrateStatus.status.done &&
    !orchestrateStatus.status.escalated;

  const awaitingReview =
    state.awaiting_count ??
    state.features.reduce(
      (sum, f) => sum + f.tasks.filter((t) => t.status === "awaiting_review").length,
      0,
    );

  const totalTokens = stats?.tasks?.reduce((sum, t) => sum + (t.tokens ?? 0), 0) ?? 0;

  const shipped7d = events.filter((e) => {
    if (e.event_type !== "merge") return false;
    const ts = Date.parse(e.ts);
    return !Number.isNaN(ts) && nowMs - ts < SHIPPED_WINDOW_MS;
  }).length;

  return {
    activeRun: {
      count: running ? 1 : 0,
      label: running ? "orchestrating" : "idle",
      live: running,
    },
    awaitingReview: { count: awaitingReview, label: "human decision" },
    tokensToday: {
      tokens: fmtTokens(totalTokens),
      cost: fmtCost(totalTokens * COST_PER_TOKEN),
      rawTokens: totalTokens,
    },
    shipped7d: { count: shipped7d, label: "to local main" },
  };
}

export interface FeatureLane {
  feature: Feature;
  progress: { done: number; total: number };
  tokens: number;
  elapsedMs: number;
  reviewTask?: Task;
}

export function deriveFeatureLanes(
  state: State,
  stats: ProjectStats | null,
  events: PactEvent[],
  nowMs: number,
): FeatureLane[] {
  const statMap = new Map(stats?.tasks?.map((t) => [t.task_id, t]));
  const byTask = eventsByTask(events);
  const lanes: FeatureLane[] = [];

  for (const f of state.features) {
    if (f.status === "shipped") continue;
    if (f.tasks.length === 0) continue;

    let tokens = 0;
    let elapsedMs = 0;
    let reviewTask: Task | undefined;
    const done = f.tasks.filter((t) => t.status === "accepted").length;

    for (const t of f.tasks) {
      tokens += statMap.get(t.id)?.tokens ?? 0;
      elapsedMs += taskRuntimeMs(t, byTask.get(t.id) ?? [], nowMs);
      if (t.status === "awaiting_review" && !reviewTask) reviewTask = t;
    }

    lanes.push({ feature: f, progress: { done, total: f.tasks.length }, tokens, elapsedMs, reviewTask });
  }

  return lanes;
}

export interface SeatRosterEntry {
  seat: Seat;
  status: "active" | "working" | "idle";
  currentTask?: string;
  shipped: number;
  tokens: number;
}

export function deriveSeatRoster(
  state: State,
  events: PactEvent[],
  stats: ProjectStats | null,
): SeatRosterEntry[] {
  const model: FlowModel = deriveFlow(events);
  const live = liveStates(model);
  const agentMap = new Map(stats?.agents?.map((a) => [a.seat, a]));

  return state.agents.map((seat) => {
    const liveState = live[seat.id];
    let status: SeatRosterEntry["status"] = "idle";
    if (liveState?.kind === "work" || liveState?.kind === "rework") status = "working";
    else if (liveState?.kind === "review") status = "active";

    const stat = agentMap.get(seat.id);
    return {
      seat,
      status,
      currentTask: liveState?.task,
      shipped: stat?.accepted ?? 0,
      tokens: stat?.tokens ?? 0,
    };
  });
}

export type ActivityKind = "awaiting" | "started" | "accepted" | "changes" | "shipped";

export interface ActivityItem {
  kind: ActivityKind;
  taskId?: string;
  agentId?: string;
  feature?: string;
  text: string;
  ts: number;
}

export function deriveActivityFeed(
  events: PactEvent[],
  state: State,
  nowMs: number,
  lastSeenMs: number,
): { items: ActivityItem[]; newCount: number } {
  const byTask = eventsByTask(events);
  const items: ActivityItem[] = [];

  for (const f of state.features) {
    for (const t of f.tasks) {
      const evs = byTask.get(t.id) ?? [];
      const recent = [...evs].reverse();

      if (t.status === "awaiting_review") {
        const cp = recent.find((e) => e.event_type === "checkpoint");
        if (cp) {
          const ts = Date.parse(cp.ts);
          if (!Number.isNaN(ts)) {
            items.push({
              kind: "awaiting",
              taskId: t.id,
              agentId: cp.agent_id,
              feature: f.id,
              text: `${t.id} awaiting your review`,
              ts,
            });
          }
        }
      } else if (t.status === "changes_requested") {
        const ch = recent.find((e) => e.event_type === "changes_requested" || e.event_type === "changes");
        if (ch) {
          const ts = Date.parse(ch.ts);
          if (!Number.isNaN(ts)) {
            items.push({
              kind: "changes",
              taskId: t.id,
              agentId:ch.agent_id,
              feature: f.id,
              text: `${t.id} changes requested`,
              ts,
            });
          }
        }
      } else if (t.status === "in_progress") {
        const st = recent.find((e) => e.event_type === "start" || e.event_type === "assign");
        if (st) {
          const ts = Date.parse(st.ts);
          if (!Number.isNaN(ts)) {
            items.push({
              kind: "started",
              taskId: t.id,
              agentId: st.agent_id,
              feature: f.id,
              text: `${st.agent_id} started ${t.id}`,
              ts,
            });
          }
        }
      } else if (t.status === "accepted") {
        const ac = recent.find((e) => e.event_type === "accept");
        if (ac) {
          const ts = Date.parse(ac.ts);
          if (!Number.isNaN(ts)) {
            items.push({
              kind: "accepted",
              taskId: t.id,
              agentId: ac.agent_id,
              feature: f.id,
              text: `${t.id} accepted by ${ac.agent_id}`,
              ts,
            });
          }
        }
      }
    }
  }

  for (const e of events) {
    if (e.event_type === "merge") {
      const ts = Date.parse(e.ts);
      if (!Number.isNaN(ts)) {
        items.push({
          kind: "shipped",
          agentId: e.agent_id,
          feature: e.feature,
          text: `${e.feature || "feature"} shipped to local main`,
          ts,
        });
      }
    }
  }

  const sorted = items
    .filter((i) => i.ts > 0 && nowMs - i.ts < SHIPPED_WINDOW_MS)
    .sort((a, b) => b.ts - a.ts)
    .slice(0, 20);

  const newCount = sorted.filter((i) => i.ts > lastSeenMs).length;
  return { items: sorted, newCount };
}

export function deriveRunProgress(
  state: State,
  orchestrateStatus?: OrchestrateStatusResponse | null,
): number {
  const total = orchestrateStatus?.status?.total;
  const accepted = orchestrateStatus?.status?.accepted;
  if (typeof total === "number" && total > 0 && typeof accepted === "number") {
    return Math.min(1, Math.max(0, accepted / total));
  }

  const all = state.features.flatMap((f) => f.tasks);
  if (all.length === 0) return 0;
  const done = all.filter((t) => t.status === "accepted" || t.status === "shipped").length;
  return done / all.length;
}

export interface RunStats {
  features: number;
  concurrency: number;
  iter: number;
  tokens: number;
  elapsedMs: number;
  cost: string;
}

export function deriveRunStats(
  state: State,
  stats: ProjectStats | null,
  orchestrateStatus: OrchestrateStatusResponse | null | undefined,
  events: PactEvent[],
  nowMs: number,
): RunStats {
  const features = state.features.length;
  const concurrency = Math.max(
    1,
    state.features.filter((f) => f.tasks.some((t) => t.status === "in_progress")).length,
  );
  const iter = orchestrateStatus?.status?.iter ?? 0;
  const tokens = stats?.tasks?.reduce((sum, t) => sum + (t.tokens ?? 0), 0) ?? 0;

  let firstAssign = Number.POSITIVE_INFINITY;
  for (const e of events) {
    if (e.event_type === "assign") {
      const ts = Date.parse(e.ts);
      if (!Number.isNaN(ts)) firstAssign = Math.min(firstAssign, ts);
    }
  }
  const elapsedMs = Number.isFinite(firstAssign) ? nowMs - firstAssign : 0;

  return {
    features,
    concurrency,
    iter,
    tokens,
    elapsedMs,
    cost: fmtCost(tokens * COST_PER_TOKEN),
  };
}

export { fmtRelTime };
