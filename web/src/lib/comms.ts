import type { State, Task } from "./types";
import type { Node, Edge } from "@xyflow/react";
import { roleColorVar } from "./canvas";

// WaitEdge is one "who-waits-on-whom" arrow derived from the snapshot. For
// review/rework edges from/to are SEAT ids; for dep edges they are TASK ids
// (the dependent task → its unmet prerequisite). reason is the rendered chip.
export interface WaitEdge {
  from: string;
  to: string;
  kind: "review" | "rework" | "dep";
  reason: string;
  taskId: string;
}

// CommsResult is the full comms lens over a single snapshot. All four arrays
// are empty in a clean state (no pending review/rework, no unmet deps, every
// seat active, every owner/reviewer joined).
export interface CommsResult {
  edges: WaitEdge[];
  idleSeats: string[];
  notJoined: string[];
  blockedTasks: string[];
}

function flatTasks(state: State): Task[] {
  const out: Task[] = [];
  for (const f of state.features) for (const t of f.tasks) out.push(t);
  return out;
}

// deriveComms folds a snapshot into wait edges + seat/task markers. Pure: reads
// only its argument, never mutates. Semantics are the spec §1 table verbatim.
export function deriveComms(state: State): CommsResult {
  const tasks = flatTasks(state);
  const byId = new Map(tasks.map((t) => [t.id, t]));
  const joined = new Set(state.agents.map((a) => a.id));

  const edges: WaitEdge[] = [];

  // unmet(t): dep task ids of t that are not accepted (an absent dep id can't be
  // accepted, so it counts as unmet too — deps are same-feature by protocol).
  const unmet = (t: Task): string[] =>
    (t.deps ?? []).filter((d) => byId.get(d)?.status !== "accepted");

  for (const t of tasks) {
    if (t.status === "awaiting_review") {
      edges.push({ from: t.owner, to: t.reviewer, kind: "review", reason: `awaiting review: ${t.id}`, taskId: t.id });
    } else if (t.status === "changes_requested") {
      edges.push({ from: t.reviewer, to: t.owner, kind: "rework", reason: `changes requested: ${t.id}`, taskId: t.id });
    }
    // Dep edges are independent of the task's own status — a task can be both
    // in_progress and waiting on an unmet dependency.
    for (const d of unmet(t)) {
      edges.push({ from: t.id, to: d, kind: "dep", reason: `blocked by ${d}`, taskId: t.id });
    }
  }

  // notJoined: every owner/reviewer id not in the roster, deduped in first-seen
  // order (owner before reviewer within a task, tasks in snapshot order).
  const notJoined: string[] = [];
  const seenMissing = new Set<string>();
  for (const t of tasks) {
    for (const id of [t.owner, t.reviewer]) {
      if (!joined.has(id) && !seenMissing.has(id)) {
        seenMissing.add(id);
        notJoined.push(id);
      }
    }
  }

  // idleSeats: a joined seat that owns no assigned/in_progress task AND reviews
  // no awaiting_review task. Accepted/merged work doesn't keep a seat active.
  const active = new Set<string>();
  for (const t of tasks) {
    // Ownership counts only while the work is in flight (assigned/in_progress);
    // an awaiting_review task's owner has handed off and is not kept active by
    // it. Its reviewer, however, now has the ball.
    if (t.status === "assigned" || t.status === "in_progress") active.add(t.owner);
    if (t.status === "awaiting_review") active.add(t.reviewer);
  }
  const idleSeats = state.agents.map((a) => a.id).filter((id) => !active.has(id));

  // blockedTasks: a task is blocked iff it is not accepted and has an unmet
  // dep. This subsumes transitive blocking: a blocked dep is by definition not
  // accepted, so it already counts as unmet for its dependents — no graph walk.
  const blockedTasks = tasks
    .filter((t) => t.status !== "accepted" && unmet(t).length > 0)
    .map((t) => t.id);

  return { edges, idleSeats, notJoined, blockedTasks };
}

// pulseTargets diffs two snapshots and returns the task ids whose status changed
// or that newly appear in next. prev null → empty (a first snapshot must not
// pulse every node). Pure; reads only its arguments.
export function pulseTargets(prev: State | null, next: State): { taskIds: string[] } {
  if (prev === null) return { taskIds: [] };
  const before = new Map(flatTasks(prev).map((t) => [t.id, t.status]));
  const taskIds: string[] = [];
  for (const t of flatTasks(next)) {
    const old = before.get(t.id);
    if (old === undefined || old !== t.status) taskIds.push(t.id);
  }
  return { taskIds };
}

// --- Overlay lens (M3.3b C4) ----------------------------------------------
//
// mergeComms folds a CommsResult into the React Flow node/edge arrays that the
// canvas already renders, producing a NEW pair (the inputs are never mutated and
// the result is NEVER persisted to layout.json — this is a display-only lens).
//
// Wait edges become DASHED React Flow edges with ids `wait:${from}→${to}:${taskId}`
// so they cannot collide with the `dep:` edge ids. Markers (idle/blocked/not
// joined) are surfaced as boolean data flags + a `commsClass` className on the
// affected nodes, which the node components / className merge already honor.
//
// Anchoring for review/rework edges (from/to are SEAT ids): we draw from the
// TASK's node to the WAITED-ON seat's node, because the task is the subject of
// the wait and the seat carrying the ball is the destination. Review waits on
// the reviewer (the `to` seat); rework waits on the owner (also the `to` seat),
// so in BOTH cases the arrow targets `seat:${edge.to}` and sources the task.
// Seat nodes render only for JOINED seats, so a wait on a never-joined seat has
// no anchor — that edge is intentionally dropped (the not-joined warning badge
// on the task carries the signal instead). Dep edges (from/to are TASK ids)
// draw task→task, neutral amber.

// Role color var for a seat id from the snapshot. Delegates to roleColorVar so
// wait-edge colors can never drift from the node border colors.
function seatColorVar(state: State, seatId: string): string {
  return roleColorVar(state.agents.find((a) => a.id === seatId)?.roles ?? []);
}

const AMBER = "#d29922"; // neutral/blocked accent (matches stale-dot + blocked outline)

export function mergeComms(
  nodes: Node[],
  edges: Edge[],
  comms: CommsResult,
  state: State,
): { nodes: Node[]; edges: Edge[] } {
  const nodeIds = new Set(nodes.map((n) => n.id));
  const idle = new Set(comms.idleSeats.map((s) => `seat:${s}`));
  const blocked = new Set(comms.blockedTasks.map((t) => `task:${t}`));
  // notJoined holds SEAT ids of seats that never joined — they have NO seat
  // node on the canvas, so the warning surfaces on the affected TASK nodes.
  const notJoinedSeats = new Set(comms.notJoined);

  // task id → true if any of its owner/reviewer never joined (built from edges'
  // backing snapshot). We resolve through the State so a task node can light up
  // even if no wait edge references it.
  const tasksWithMissing = new Set<string>();
  for (const f of state.features) {
    for (const t of f.tasks) {
      if (notJoinedSeats.has(t.owner) || notJoinedSeats.has(t.reviewer)) {
        tasksWithMissing.add(`task:${t.id}`);
      }
    }
  }

  const outNodes: Node[] = nodes.map((n) => {
    const isIdle = idle.has(n.id);
    const isBlocked = blocked.has(n.id);
    const isNotJoined = tasksWithMissing.has(n.id);
    if (!isIdle && !isBlocked && !isNotJoined) return n;
    const classes = [
      isIdle ? "comms-idle" : "",
      isBlocked ? "comms-blocked" : "",
      isNotJoined ? "comms-notjoined" : "",
    ].filter(Boolean).join(" ");
    return {
      ...n,
      className: [n.className, classes].filter(Boolean).join(" "),
      data: { ...n.data, commsIdle: isIdle, commsBlocked: isBlocked, commsNotJoined: isNotJoined },
    };
  });

  const waitEdges: Edge[] = comms.edges.flatMap((e) => {
    const id = `wait:${e.from}→${e.to}:${e.taskId}`;
    let source: string;
    let target: string;
    let color: string;
    if (e.kind === "dep") {
      // from/to are TASK ids; draw dependent → unmet prerequisite, amber.
      source = `task:${e.from}`;
      target = `task:${e.to}`;
      color = AMBER;
    } else {
      // review/rework: from/to are SEAT ids. Anchor TASK → waited-on seat.
      const taskNode = `task:${e.taskId}`;
      source = nodeIds.has(taskNode) ? taskNode : `seat:${e.from}`;
      target = `seat:${e.to}`;
      color = `var(${seatColorVar(state, e.to)})`;
    }
    // An edge whose endpoint has no node (e.g. the waited-on seat never joined)
    // would be silently skipped by React Flow — drop it explicitly; the
    // not-joined badge on the task carries the signal.
    if (!nodeIds.has(source) || !nodeIds.has(target)) return [];
    return [{
      id,
      source,
      target,
      label: e.reason,
      labelStyle: { fontSize: 9, fill: "#e6edf3" },
      labelBgStyle: { fill: "#161b22" },
      labelBgPadding: [3, 1] as [number, number],
      style: { stroke: color, strokeDasharray: "5 4", strokeWidth: 1.5 },
      animated: false,
      data: { comms: true, kind: e.kind },
    }];
  });

  return { nodes: outNodes, edges: [...edges, ...waitEdges] };
}
