import { describe, it, expect } from 'vitest'
import { projectBoard, boardColumns, type PactEventHeader } from './board'

function ev(seq: number, eventType: string, task?: string, feature = 'f1'): PactEventHeader {
  return { seq, eventType, task, feature, ts: 1000 + seq }
}

describe('projectBoard (U2 read-only board)', () => {
  it('places a task in the column of its latest state-affecting event', () => {
    const tasks = projectBoard([
      ev(0, 'assign', 't1'),
      ev(1, 'start', 't1'),
      ev(2, 'checkpoint', 't1'),
      ev(3, 'accept', 't1'),
    ])
    expect(tasks).toHaveLength(1)
    expect(tasks[0]).toMatchObject({ id: 't1', column: 'accepted', lastEventType: 'accept', feature: 'f1' })
  })

  it('applies events in seq order regardless of input order (last wins)', () => {
    const tasks = projectBoard([ev(2, 'checkpoint', 't1'), ev(0, 'assign', 't1'), ev(1, 'start', 't1')])
    expect(tasks[0].column).toBe('awaiting_review')
    expect(tasks[0].lastSeq).toBe(2)
  })

  it('changes_requested sends a task back from review', () => {
    const tasks = projectBoard([ev(0, 'assign', 't1'), ev(1, 'checkpoint', 't1'), ev(2, 'changes_requested', 't1')])
    expect(tasks[0].column).toBe('changes_requested')
  })

  it('leaves the column unchanged for non-state events (e.g. merge)', () => {
    const tasks = projectBoard([ev(0, 'assign', 't1'), ev(1, 'accept', 't1'), ev(2, 'merge', 't1')])
    expect(tasks[0].column).toBe('accepted')
    expect(tasks[0].lastEventType).toBe('merge')
  })

  it('ignores events without a task and tracks multiple tasks', () => {
    const cols = boardColumns([ev(0, 'assign', 't1'), ev(1, 'assign', 't2'), ev(2, 'checkpoint', 't2'), ev(3, 'init', undefined)])
    expect(cols.assigned.map((t) => t.id)).toEqual(['t1'])
    expect(cols.awaiting_review.map((t) => t.id)).toEqual(['t2'])
  })
})
