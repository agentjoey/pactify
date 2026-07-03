import { describe, it, expect } from 'vitest'
import { BackendCapabilitiesLite, RunSummary } from '../src/summary'

describe('BackendCapabilitiesLite', () => {
  it('parses a full capabilities object and round-trips', () => {
    const caps = {
      diff: true,
      approvals: false,
      sessionResume: true,
      takeover: false,
      mcp: true,
      imageInput: false,
      models: ['claude-opus', 'claude-sonnet'],
    }
    const parsed = BackendCapabilitiesLite.parse(caps)
    expect(parsed).toEqual(caps)
  })

  it('parses an empty object (all booleans optional)', () => {
    expect(BackendCapabilitiesLite.parse({})).toEqual({})
  })

  it('rejects a non-boolean capability', () => {
    expect(BackendCapabilitiesLite.safeParse({ diff: 'yes' }).success).toBe(false)
  })

  it('rejects a non-string entry in models', () => {
    expect(BackendCapabilitiesLite.safeParse({ models: [1, 2] }).success).toBe(false)
  })
})

describe('RunSummary', () => {
  const valid = {
    id: 'run-1',
    agentKind: 'claude',
    model: 'claude-opus',
    state: 'thinking',
    pendingApprovals: 0,
    tokensIn: 100,
    tokensOut: 200,
    costMicros: 1500,
    machineId: 'm1',
    seq: 7,
    updatedAt: 1719000000000,
    capabilities: { diff: true, models: ['claude-opus'] },
  }

  it('parses a valid summary and round-trips', () => {
    const parsed = RunSummary.parse(valid)
    expect(parsed).toEqual(valid)
  })

  it('accepts an optional branch and round-trips without it', () => {
    expect(RunSummary.parse({ ...valid, branch: 'feat/x' }).branch).toBe('feat/x')
    expect(RunSummary.parse(valid).branch).toBeUndefined()
  })

  it('accepts an optional startedAt (epoch ms) and round-trips without it', () => {
    expect(RunSummary.parse({ ...valid, startedAt: 1719000000000 }).startedAt).toBe(1719000000000)
    expect(RunSummary.parse(valid).startedAt).toBeUndefined()
  })

  it('accepts an optional workdir and round-trips without it', () => {
    expect(RunSummary.parse({ ...valid, workdir: '/Users/me/repo' }).workdir).toBe('/Users/me/repo')
    expect(RunSummary.parse(valid).workdir).toBeUndefined()
  })

  it('accepts an optional repoRoot and round-trips without it', () => {
    expect(RunSummary.parse({ ...valid, repoRoot: '/Users/me/repo' }).repoRoot).toBe(
      '/Users/me/repo',
    )
    expect(RunSummary.parse(valid).repoRoot).toBeUndefined()
  })

  it('accepts an optional title and round-trips without it', () => {
    expect(RunSummary.parse({ ...valid, title: 'fixing the build' }).title).toBe('fixing the build')
    expect(RunSummary.parse(valid).title).toBeUndefined()
  })

  it('parses a minimal summary (optional fields omitted)', () => {
    const minimal = {
      id: 'run-2',
      agentKind: 'opencode',
      state: 'idle',
      pendingApprovals: 0,
      tokensIn: 0,
      tokensOut: 0,
      machineId: 'm2',
      seq: 0,
      updatedAt: 1719000000001,
    }
    expect(RunSummary.parse(minimal)).toEqual(minimal)
  })

  it('rejects a missing required field (machineId)', () => {
    const { machineId, ...rest } = valid
    void machineId
    expect(RunSummary.safeParse(rest).success).toBe(false)
  })

  it('rejects a non-integer seq', () => {
    expect(RunSummary.safeParse({ ...valid, seq: 1.5 }).success).toBe(false)
  })

  it('rejects an unknown agentKind', () => {
    expect(RunSummary.safeParse({ ...valid, agentKind: 'cursor' }).success).toBe(false)
  })

  it('rejects an unknown state', () => {
    expect(RunSummary.safeParse({ ...valid, state: 'paused' }).success).toBe(false)
  })
})
