import type { PactEvent, State } from "./types";

export interface FlowLane {
  id: string;
  firstT: number;
}

export interface FlowStint {
  agent: string;
  task: string;
  kind: "work" | "rework" | "review";
  t0: number;
  t1: number | null;
}

export interface FlowArrow {
  verb: "assign" | "checkpoint" | "changes" | "accept";
  from: string;
  to: string;
  task: string;
  t: number;
  note?: string;
}

export interface FlowMark {
  agent: string;
  verb: "merge" | "join";
  task?: string;
  t: number;
}

export interface FlowGap {
  t0: number;
  t1: number;
}

export interface FlowModel {
  lanes: FlowLane[];
  stints: FlowStint[];
  arrows: FlowArrow[];
  marks: FlowMark[];
  gaps: FlowGap[];
  tMin: number;
  tMax: number;
  x(t: number): number;
}

export type LiveKind = "idle" | "work" | "rework" | "review";

/** Live state for each seat inferred from open stints in the flow model.
 *  The first open stint per agent wins; agents with no open stint are idle. */
export function liveStates(
  model: FlowModel,
): Record<string, { kind: LiveKind; task?: string }> {
  const out: Record<string, { kind: LiveKind; task?: string }> = {};
  for (const lane of model.lanes) {
    out[lane.id] = { kind: "idle" };
  }
  for (const s of model.stints) {
    if (s.t1 === null && out[s.agent]?.kind === "idle") {
      out[s.agent] = { kind: s.kind, task: s.task };
    }
  }
  return out;
}

/** Returns a map of task ids → ids of their deps that are not yet accepted or
 *  shipped. Empty map means nothing is blocked. Mirrors the blocked-derivation
 *  logic in Board.tsx. */
export function blockedTasks(state: State): Map<string, string[]> {
  const status = new Map<string, string>();
  for (const f of state.features) {
    for (const t of f.tasks) {
      status.set(t.id, t.status);
    }
  }
  const out = new Map<string, string[]>();
  for (const f of state.features) {
    for (const t of f.tasks) {
      if (!t.deps?.length) continue;
      const blocked = t.deps.filter((depId) => {
        const depStatus = status.get(depId);
        return depStatus !== "accepted" && depStatus !== "shipped";
      });
      if (blocked.length) out.set(t.id, blocked);
    }
  }
  return out;
}

const DEFAULT_GAP_MIN_MS = 30 * 60_000;
const GAP_W = 0.02;

function parseMs(ts: string): number | null {
  const n = Date.parse(ts);
  return Number.isNaN(n) ? null : n;
}

function asString(v: unknown): string | undefined {
  return typeof v === "string" ? v : undefined;
}

function asStringArray(v: unknown): string[] | undefined {
  return Array.isArray(v) && v.every((x) => typeof x === "string")
    ? (v as string[])
    : undefined;
}

function buildX(
  tMin: number,
  tMax: number,
  gaps: FlowGap[],
): (t: number) => number {
  if (tMax <= tMin) {
    return () => 0;
  }
  const totalReal = tMax - tMin;
  const gapSum = gaps.reduce((sum, g) => sum + (g.t1 - g.t0), 0);
  const working = totalReal - gapSum;
  const workingWidth = Math.max(0, 1 - gaps.length * GAP_W);
  const scale = working > 0 ? workingWidth / working : 0;

  return (t: number): number => {
    if (t <= tMin) return 0;
    if (t >= tMax) return 1;

    let pos = 0;
    let prev = tMin;
    for (const g of gaps) {
      if (t < g.t0) {
        return pos + (t - prev) * scale;
      }
      if (t < g.t1) {
        const frac = (t - g.t0) / (g.t1 - g.t0);
        return pos + (g.t0 - prev) * scale + frac * GAP_W;
      }
      pos += (g.t0 - prev) * scale + GAP_W;
      prev = g.t1;
    }
    return Math.min(1, pos + (t - prev) * scale);
  };
}

/** Inverse of buildX: map normalized x∈[0,1] back to milliseconds.
 *  Returns tMin/tMax at the edges. If x falls inside a compressed gap
 *  segment, returns the gap's start time (t0). */
export function tAt(model: FlowModel, x: number): number {
  const { tMin, tMax, gaps } = model;
  if (tMax <= tMin) return tMin;
  if (x <= 0) return tMin;
  if (x >= 1) return tMax;

  const totalReal = tMax - tMin;
  const gapSum = gaps.reduce((sum, g) => sum + (g.t1 - g.t0), 0);
  const working = totalReal - gapSum;
  const workingWidth = Math.max(0, 1 - gaps.length * GAP_W);
  const scale = working > 0 ? workingWidth / working : 0;

  let pos = 0;
  let prev = tMin;
  for (const g of gaps) {
    const workSeg = (g.t0 - prev) * scale;
    if (x < pos + workSeg) {
      if (scale === 0) return prev;
      return prev + (x - pos) / scale;
    }
    if (x < pos + workSeg + GAP_W) {
      return g.t0;
    }
    pos += workSeg + GAP_W;
    prev = g.t1;
  }
  if (scale === 0) return prev;
  return Math.min(tMax, prev + (x - pos) / scale);
}

export function deriveFlow(
  events: PactEvent[],
  opts?: { gapMinMs?: number },
): FlowModel {
  const gapMinMs = opts?.gapMinMs ?? DEFAULT_GAP_MIN_MS;

  const sorted = events
    .map((e) => ({ e, ms: parseMs(e.ts) }))
    .filter((item): item is { e: PactEvent; ms: number } => item.ms !== null)
    .sort((a, b) => a.ms - b.ms)
    .map((item) => ({ ...item.e, _ms: item.ms }));

  if (sorted.length === 0) {
    return {
      lanes: [],
      stints: [],
      arrows: [],
      marks: [],
      gaps: [],
      tMin: 0,
      tMax: 0,
      x: () => 0,
    };
  }

  const firstT = new Map<string, number>();
  const orchestrators = new Set<string>();

  const stints: FlowStint[] = [];
  const arrows: FlowArrow[] = [];
  const marks: FlowMark[] = [];

  // Per-task bookkeeping.
  const assignCtx = new Map<
    string,
    { owner: string; reviewer?: string; reviewers?: string[] }
  >();
  const ownerStint = new Map<string, FlowStint>();
  const reviewStints = new Map<string, Map<string, FlowStint>>();
  const needsRework = new Map<string, boolean>();

  function closeOwnerStint(task: string, t: number) {
    const s = ownerStint.get(task);
    if (s && s.t1 === null) {
      s.t1 = t;
    }
    ownerStint.delete(task);
  }

  function closeReviewerStint(task: string, reviewer: string, t: number) {
    const map = reviewStints.get(task);
    if (!map) return;
    const s = map.get(reviewer);
    if (s && s.t1 === null) {
      s.t1 = t;
    }
  }

  for (const e of sorted) {
    const t = e._ms;
    const agent = e.agent_id;
    const task = e.task_id;

    if (!firstT.has(agent)) {
      firstT.set(agent, t);
    }
    if (e.role === "orchestrator") {
      orchestrators.add(agent);
    }

    switch (e.event_type) {
      case "join": {
        marks.push({ agent, verb: "join", t });
        break;
      }
      case "merge": {
        marks.push({
          agent,
          verb: "merge",
          task: task || undefined,
          t,
        });
        break;
      }
      case "assign": {
        const owner = asString(e.payload.owner);
        const reviewer = asString(e.payload.reviewer);
        const reviewers = asStringArray(e.payload.reviewers);
        if (owner) {
          assignCtx.set(task, { owner, reviewer, reviewers });
          if (owner !== agent) {
            arrows.push({
              verb: "assign",
              from: agent,
              to: owner,
              task,
              t,
              note: asString(e.payload.note) || undefined,
            });
          }
          closeOwnerStint(task, t);
          const kind = needsRework.get(task) ? "rework" : "work";
          needsRework.set(task, false);
          const s: FlowStint = {
            agent: owner,
            task,
            kind,
            t0: t,
            t1: null,
          };
          stints.push(s);
          ownerStint.set(task, s);
        }
        break;
      }
      case "checkpoint": {
        closeOwnerStint(task, t);
        needsRework.set(task, false);
        const ctx = assignCtx.get(task);
        if (ctx?.owner) {
          const reviewers =
            ctx.reviewers && ctx.reviewers.length > 0
              ? ctx.reviewers
              : ctx.reviewer
                ? [ctx.reviewer]
                : [];
          for (const to of reviewers) {
            arrows.push({
              verb: "checkpoint",
              from: ctx.owner,
              to,
              task,
              t,
            });
            const map = reviewStints.get(task) ?? new Map<string, FlowStint>();
            reviewStints.set(task, map);
            const s: FlowStint = {
              agent: to,
              task,
              kind: "review",
              t0: t,
              t1: null,
            };
            stints.push(s);
            map.set(to, s);
          }
        }
        break;
      }
      case "changes_requested":
      case "changes": {
        const ctx = assignCtx.get(task);
        if (ctx?.owner) {
          arrows.push({
            verb: "changes",
            from: agent,
            to: ctx.owner,
            task,
            t,
          });
          // A changes_requested event ends the whole review round for this task.
          const map = reviewStints.get(task);
          if (map) {
            for (const [, s] of map) {
              if (s.t1 === null) {
                s.t1 = t;
              }
            }
          }
          closeOwnerStint(task, t);
          needsRework.set(task, true);
          const s: FlowStint = {
            agent: ctx.owner,
            task,
            kind: "rework",
            t0: t,
            t1: null,
          };
          stints.push(s);
          ownerStint.set(task, s);
        }
        break;
      }
      case "accept": {
        const ctx = assignCtx.get(task);
        if (ctx?.owner) {
          arrows.push({
            verb: "accept",
            from: agent,
            to: ctx.owner,
            task,
            t,
          });
          closeReviewerStint(task, agent, t);
        }
        break;
      }
    }
  }

  const lanes = Array.from(firstT.entries())
    .sort(([aId, aT], [bId, bT]) => {
      const aOrc = orchestrators.has(aId);
      const bOrc = orchestrators.has(bId);
      if (aOrc && !bOrc) return -1;
      if (!aOrc && bOrc) return 1;
      return aT - bT;
    })
    .map(([id, first]) => ({ id, firstT: first }));

  const tMin = sorted[0]._ms;
  const tMax = sorted[sorted.length - 1]._ms;

  const gaps: FlowGap[] = [];
  for (let i = 1; i < sorted.length; i++) {
    const prev = sorted[i - 1]._ms;
    const cur = sorted[i]._ms;
    if (cur - prev > gapMinMs) {
      gaps.push({ t0: prev, t1: cur });
    }
  }

  const x = buildX(tMin, tMax, gaps);

  return {
    lanes,
    stints,
    arrows,
    marks,
    gaps,
    tMin,
    tMax,
    x,
  };
}
