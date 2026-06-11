import type { State, Task } from "./types";

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

function allTasks(state: State): Task[] {
  const out: Task[] = [];
  for (const f of state.features) for (const t of f.tasks) out.push(t);
  return out;
}

// deriveComms folds a snapshot into wait edges + seat/task markers. Pure: reads
// only its argument, never mutates. Semantics are the spec §1 table verbatim.
export function deriveComms(state: State): CommsResult {
  const tasks = allTasks(state);
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

  // blockedTasks: a task is blocked if it has an unmet dep (direct) or any of
  // its deps is itself blocked (transitive). Accepted tasks are never blocked.
  // The dep graph is acyclic (assign-time DFS guard), so memoized reachability
  // terminates without a visited-cycle guard.
  const memo = new Map<string, boolean>();
  const isBlocked = (t: Task): boolean => {
    const cached = memo.get(t.id);
    if (cached !== undefined) return cached;
    if (t.status === "accepted") {
      memo.set(t.id, false);
      return false;
    }
    let blocked = false;
    for (const d of t.deps ?? []) {
      const dep = byId.get(d);
      if (!dep || dep.status !== "accepted") {
        // Unmet dep (absent or not accepted) blocks directly. If the dep exists
        // and is itself blocked, that's the transitive case — same outcome.
        blocked = true;
        break;
      }
    }
    memo.set(t.id, blocked);
    return blocked;
  };
  const blockedTasks = tasks.filter((t) => isBlocked(t)).map((t) => t.id);

  return { edges, idleSeats, notJoined, blockedTasks };
}

// pulseTargets diffs two snapshots and returns the task ids whose status changed
// or that newly appear in next. prev null → empty (a first snapshot must not
// pulse every node). Pure; reads only its arguments.
export function pulseTargets(prev: State | null, next: State): { taskIds: string[] } {
  if (prev === null) return { taskIds: [] };
  const before = new Map(allTasks(prev).map((t) => [t.id, t.status]));
  const taskIds: string[] = [];
  for (const t of allTasks(next)) {
    const old = before.get(t.id);
    if (old === undefined || old !== t.status) taskIds.push(t.id);
  }
  return { taskIds };
}
