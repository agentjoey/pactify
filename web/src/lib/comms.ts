import type { State, Task } from "./types";

function flatTasks(state: State): Task[] {
  const out: Task[] = [];
  for (const f of state.features) for (const t of f.tasks) out.push(t);
  return out;
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
