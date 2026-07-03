import { describe, it, expect } from 'vitest'
import { OperationalHeader, RunState, EventKind } from '../src/operational'

const valid = {
  v: 1,
  machineId: 'm1',
  runId: 'r1',
  seq: 0,
  ts: 1719100000000,
  state: 'thinking',
  eventKind: 'delta',
  pendingApprovals: 0,
  tokensIn: 10,
  tokensOut: 5,
}

describe('OperationalHeader', () => {
  it('parses a valid header', () => {
    const parsed = OperationalHeader.parse(valid)
    expect(parsed.runId).toBe('r1')
    expect(parsed.state).toBe('thinking')
  })

  it('accepts optional costMicros', () => {
    expect(OperationalHeader.parse({ ...valid, costMicros: 1200 }).costMicros).toBe(1200)
  })

  it('accepts an optional branch and round-trips without it', () => {
    expect(OperationalHeader.parse({ ...valid, branch: 'main' }).branch).toBe('main')
    expect(OperationalHeader.parse(valid).branch).toBeUndefined()
  })

  it('accepts an optional startedAt (epoch ms) and round-trips without it', () => {
    expect(OperationalHeader.parse({ ...valid, startedAt: 1719100000000 }).startedAt).toBe(
      1719100000000,
    )
    expect(OperationalHeader.parse(valid).startedAt).toBeUndefined()
  })

  it('accepts an optional workdir and round-trips without it', () => {
    expect(OperationalHeader.parse({ ...valid, workdir: '/Users/me/repo' }).workdir).toBe(
      '/Users/me/repo',
    )
    expect(OperationalHeader.parse(valid).workdir).toBeUndefined()
  })

  it('accepts an optional repoRoot and round-trips without it', () => {
    expect(OperationalHeader.parse({ ...valid, repoRoot: '/Users/me/repo' }).repoRoot).toBe(
      '/Users/me/repo',
    )
    expect(OperationalHeader.parse(valid).repoRoot).toBeUndefined()
  })

  it('accepts an optional title and round-trips without it', () => {
    expect(OperationalHeader.parse({ ...valid, title: 'Session name' }).title).toBe('Session name')
    expect(OperationalHeader.parse(valid).title).toBeUndefined()
  })

  it('rejects a non-integer startedAt', () => {
    expect(() => OperationalHeader.parse({ ...valid, startedAt: 1.5 })).toThrow()
  })

  it('rejects an unknown state', () => {
    expect(() => OperationalHeader.parse({ ...valid, state: 'sleeping' })).toThrow()
  })

  it('rejects a negative seq', () => {
    expect(() => OperationalHeader.parse({ ...valid, seq: -1 })).toThrow()
  })

  it('rejects v != 1', () => {
    expect(() => OperationalHeader.parse({ ...valid, v: 2 })).toThrow()
  })

  it('enumerates the run states and event kinds', () => {
    expect(RunState.options).toContain('awaiting-approval')
    expect(EventKind.options).toContain('snapshot')
  })

  it('includes thinking in the event kinds', () => {
    expect(EventKind.options).toContain('thinking')
    expect(OperationalHeader.parse({ ...valid, eventKind: 'thinking' }).eventKind).toBe('thinking')
  })
})
