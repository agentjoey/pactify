import type { State, Task } from "./types";

// Exported: office.ts shares this flattening (one definition, no drift).
export function flatTasks(state: State): Task[] {
  const out: Task[] = [];
  for (const f of state.features) for (const t of f.tasks) out.push(t);
  return out;
}

// unmet(t, byId): dep task ids of t that are not accepted (an absent dep id can't
// be accepted, so it counts as unmet too — deps are same-feature by protocol).
// Shared with office.ts (T9) so the two lenses can never drift on what "blocked"
// means. byId is the caller's flatTasks index; pure (reads only its args).
export function unmetDeps(t: Task, byId: Map<string, Task>): string[] {
  return (t.deps ?? []).filter((d) => byId.get(d)?.status !== "accepted");
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
