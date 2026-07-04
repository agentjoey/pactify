import { describe, it, expect } from 'vitest'
import { project, type PactEvent, type State } from '../src/index.js'

function ev(
  overrides: Partial<PactEvent> & { event_type: string },
): PactEvent {
  let n = nextId++
  return {
    event_id: `ev${String(n).padStart(3, '0')}`,
    ts: '2026-01-01T00:00:00Z',
    agent_id: 'claude',
    role: 'orchestrator',
    task_id: '',
    feature: '',
    payload: {},
    ...overrides,
  }
}

let nextId = 1

describe('project', () => {
  it('empty events yield default State', () => {
    const s = project([])
    expect(s.project).toBe('unknown')
    expect(s.agents).toEqual([])
    expect(s.features).toEqual([])
    expect(s.awaiting_count).toBe(0)
  })

  it('init sets project and initial seats', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: {
          project: 'myproject',
          protocol_version: 1,
          seats: [
            { id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'ALICE.md' },
            { id: 'bob', roles: ['worker'], kind: 'opencode', entry: 'BOB.md' },
          ],
        },
      }),
    ])
    expect(s.project).toBe('myproject')
    expect(s.agents).toEqual([
      { id: 'alice', roles: ['orchestrator'], kind: 'claude-code' },
      { id: 'bob', roles: ['worker'], kind: 'opencode' },
    ])
  })

  it('add-seat adds a new agent', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: {
          project: 'p',
          seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'ALICE.md' }],
        },
      }),
      ev({ event_type: 'add-seat', payload: { id: 'bob', roles: ['worker'], kind: 'opencode', entry: 'BOB.md' } }),
    ])
    expect(s.agents).toHaveLength(2)
    expect(s.agents[1].id).toBe('bob')
  })

  it('add-seat is idempotent on duplicate id', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: {
          project: 'p',
          seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'ALICE.md' }],
        },
      }),
      ev({ event_type: 'add-seat', payload: { id: 'alice', roles: ['worker'], kind: 'claude-code', entry: 'ALICE.md' } }),
    ])
    expect(s.agents).toHaveLength(1)
  })

  it('assign creates a feature and task', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
    ])
    expect(s.features).toHaveLength(1)
    expect(s.features[0].id).toBe('feat')
    expect(s.features[0].tasks).toHaveLength(1)
    expect(s.features[0].tasks[0].id).toBe('T1')
    expect(s.features[0].tasks[0].status).toBe('assigned')
    expect(s.features[0].tasks[0].deps).toBeUndefined()
  })

  it('assign with deps includes deps array', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T2',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T2.md', deps: ['T1'] },
      }),
    ])
    expect(s.features[0].tasks).toHaveLength(2)
    expect(s.features[0].tasks[1].deps).toEqual(['T1'])
  })

  it('start lifts assigned → in_progress', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'start',
        agent_id: 'claude',
        role: 'orchestrator',
        task_id: 'T1',
        feature: 'feat',
        payload: { owner: 'bob' },
      }),
    ])
    expect(s.features[0].tasks[0].status).toBe('in_progress')
  })

  it('checkpoint sets awaiting_review and evidence', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'checkpoint',
        agent_id: 'bob',
        role: 'worker',
        task_id: 'T1',
        feature: 'feat',
        payload: { evidence: 'all tests pass' },
      }),
    ])
    expect(s.features[0].tasks[0].status).toBe('awaiting_review')
    expect(s.features[0].tasks[0].evidence).toBe('all tests pass')
    expect(s.awaiting_count).toBe(1)
  })

  it('accept sets accepted', () => {
    const events: PactEvent[] = [
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'checkpoint',
        agent_id: 'bob',
        role: 'worker',
        task_id: 'T1',
        feature: 'feat',
        payload: { evidence: 'done' },
      }),
      ev({
        event_type: 'accept',
        task_id: 'T1',
        feature: 'feat',
        payload: {},
      }),
    ]
    const s = project(events)
    expect(s.features[0].tasks[0].status).toBe('accepted')
    expect(s.awaiting_count).toBe(0)
  })

  it('changes_requested sets changes_requested', () => {
    const events: PactEvent[] = [
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'checkpoint',
        agent_id: 'bob',
        role: 'worker',
        task_id: 'T1',
        feature: 'feat',
        payload: { evidence: 'done' },
      }),
      ev({
        event_type: 'changes_requested',
        task_id: 'T1',
        feature: 'feat',
        payload: {},
      }),
    ]
    const s = project(events)
    expect(s.features[0].tasks[0].status).toBe('changes_requested')
    expect(s.awaiting_count).toBe(0)
  })

  it('merge sets feature status to shipped', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'accept',
        task_id: 'T1',
        feature: 'feat',
        payload: {},
      }),
      ev({
        event_type: 'merge',
        feature: 'feat',
        payload: {},
      }),
    ])
    expect(s.features[0].status).toBe('shipped')
  })

  it('cancel removes a task from the projection', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'cancel',
        task_id: 'T1',
        feature: 'feat',
        payload: {},
      }),
    ])
    expect(s.features[0].tasks).toHaveLength(0)
  })

  it('withdraw removes a feature from the projection', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'withdraw',
        feature: 'feat',
        payload: {},
      }),
    ])
    expect(s.features).toHaveLength(0)
  })

  it('multiple features preserve first-seen order', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'A1',
        feature: 'alpha',
        payload: { branch: 'feat/a', owner: 'bob', reviewer: 'alice', spec: 'spec/A1.md' },
      }),
      ev({
        event_type: 'assign',
        task_id: 'B1',
        feature: 'beta',
        payload: { branch: 'feat/b', owner: 'bob', reviewer: 'alice', spec: 'spec/B1.md' },
      }),
      ev({
        event_type: 'assign',
        task_id: 'A2',
        feature: 'alpha',
        payload: { branch: 'feat/a', owner: 'bob', reviewer: 'alice', spec: 'spec/A2.md' },
      }),
    ])
    expect(s.features.map((f) => f.id)).toEqual(['alpha', 'beta'])
    expect(s.features[0].tasks.map((t) => t.id)).toEqual(['A1', 'A2'])
    expect(s.features[1].tasks.map((t) => t.id)).toEqual(['B1'])
  })

  it('unknown event types are ignored', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({ event_type: 'assign' as never, task_id: 'T1', feature: 'feat', payload: { branch: 'b', owner: 'bob', reviewer: 'alice', spec: 's' } } as PactEvent),
      ev({ event_type: 'unknown_event' as never, task_id: 'T1', feature: 'feat', payload: {} } as PactEvent),
    ])
    expect(s.features[0].tasks).toHaveLength(1)
    expect(s.features[0].tasks[0].status).toBe('assigned')
  })

  it('join lifts first ready assigned task to in_progress', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }, { id: 'bob', roles: ['worker'], kind: 'opencode', entry: 'B.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'join',
        agent_id: 'bob',
        role: 'worker',
        payload: { roles: ['worker'] },
      }),
    ])
    expect(s.features[0].tasks[0].status).toBe('in_progress')
  })

  it('join does not lift task with unaccepted dep', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }, { id: 'bob', roles: ['worker'], kind: 'opencode', entry: 'B.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'alice', reviewer: 'bob', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T2',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T2.md', deps: ['T1'] },
      }),
      ev({
        event_type: 'join',
        agent_id: 'bob',
        role: 'worker',
        payload: { roles: ['worker'] },
      }),
    ])
    expect(s.features[0].tasks[1].status).toBe('assigned')
  })

  it('join lifts task when dep is accepted', () => {
    const s = project([
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }, { id: 'bob', roles: ['worker'], kind: 'opencode', entry: 'B.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'alice', reviewer: 'bob', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'accept',
        task_id: 'T1',
        feature: 'feat',
        payload: {},
      }),
      ev({
        event_type: 'assign',
        task_id: 'T2',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T2.md', deps: ['T1'] },
      }),
      ev({
        event_type: 'join',
        agent_id: 'bob',
        role: 'worker',
        payload: { roles: ['worker'] },
      }),
    ])
    expect(s.features[0].tasks[1].status).toBe('in_progress')
  })

  it('changes_requested followed by checkpoint restores awaiting_review', () => {
    const events: PactEvent[] = [
      ev({
        event_type: 'init',
        payload: { project: 'p', seats: [{ id: 'alice', roles: ['orchestrator'], kind: 'claude-code', entry: 'A.md' }] },
      }),
      ev({
        event_type: 'assign',
        task_id: 'T1',
        feature: 'feat',
        payload: { branch: 'feat/feat', owner: 'bob', reviewer: 'alice', spec: 'spec/T1.md' },
      }),
      ev({
        event_type: 'checkpoint',
        agent_id: 'bob',
        role: 'worker',
        task_id: 'T1',
        feature: 'feat',
        payload: { evidence: 'v1' },
      }),
      ev({
        event_type: 'changes_requested',
        task_id: 'T1',
        feature: 'feat',
        payload: {},
      }),
      ev({
        event_type: 'checkpoint',
        agent_id: 'bob',
        role: 'worker',
        task_id: 'T1',
        feature: 'feat',
        payload: { evidence: 'v2 fixed' },
      }),
    ]
    const s = project(events)
    expect(s.features[0].tasks[0].status).toBe('awaiting_review')
    expect(s.features[0].tasks[0].evidence).toBe('v2 fixed')
    expect(s.awaiting_count).toBe(1)
  })
})
