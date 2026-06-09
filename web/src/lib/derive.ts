import type { State, BoardTask, PactEvent } from "./types";

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
