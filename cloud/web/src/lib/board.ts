// Read-only board projection for U2 Mission Control. The columns/tasks are folded
// from the CLEARTEXT pact-event headers (eventType/task/feature/seq/ts), so the
// board renders without decryption; the encrypted body is only needed for
// drill-down detail. Mirrors the pactify board columns.

export type Column =
  | 'assigned'
  | 'in_progress'
  | 'awaiting_review'
  | 'accepted'
  | 'changes_requested'

export const COLUMNS: Column[] = [
  'assigned',
  'in_progress',
  'awaiting_review',
  'accepted',
  'changes_requested',
]

/** The cleartext header of one pact event as served by /v1/pact/projects/:id/events. */
export interface PactEventHeader {
  seq: number
  eventType: string
  task?: string | null
  feature?: string | null
  ts: number
}

export interface BoardTask {
  id: string
  feature?: string
  column: Column
  lastEventType: string
  lastSeq: number
  ts: number
}

// Which column an event_type moves a task into. Types not listed (merge, etc.)
// leave the column unchanged (merge is feature-level; the task stays accepted).
const EVENT_COLUMN: Record<string, Column> = {
  assign: 'assigned',
  join: 'in_progress',
  start: 'in_progress',
  checkpoint: 'awaiting_review',
  accept: 'accepted',
  changes_requested: 'changes_requested',
}

/**
 * Fold a project's cleartext event stream (seq order) into its current board:
 * one BoardTask per task_id, in the column implied by its latest state-affecting
 * event. Events are applied in seq order so the last one wins.
 */
export function projectBoard(events: PactEventHeader[]): BoardTask[] {
  const byTask = new Map<string, BoardTask>()
  const ordered = [...events].sort((a, b) => a.seq - b.seq)
  for (const e of ordered) {
    if (!e.task) continue
    const cur: BoardTask = byTask.get(e.task) ?? {
      id: e.task,
      column: 'assigned',
      lastEventType: '',
      lastSeq: -1,
      ts: 0,
    }
    cur.lastEventType = e.eventType
    cur.lastSeq = e.seq
    cur.ts = e.ts
    if (e.feature) cur.feature = e.feature
    const col = EVENT_COLUMN[e.eventType]
    if (col) cur.column = col
    byTask.set(e.task, cur)
  }
  return [...byTask.values()]
}

/** Group tasks by column for rendering. */
export function boardColumns(events: PactEventHeader[]): Record<Column, BoardTask[]> {
  const out = Object.fromEntries(COLUMNS.map((c) => [c, [] as BoardTask[]])) as Record<
    Column,
    BoardTask[]
  >
  for (const t of projectBoard(events)) out[t.column].push(t)
  return out
}
