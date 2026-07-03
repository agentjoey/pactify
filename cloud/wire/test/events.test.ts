import { describe, it, expect } from 'vitest'
import { AgentEvent } from '../src/events'

describe('AgentEvent', () => {
  it('parses a message event', () => {
    const e = AgentEvent.parse({ kind: 'message', role: 'assistant', text: 'hi' })
    expect(e.kind).toBe('message')
  })

  it('parses an approval-request event and narrows it', () => {
    const e = AgentEvent.parse({
      kind: 'approval-request',
      approvalId: 'a1',
      tool: 'Bash',
      detail: 'rm -rf /',
    })
    if (e.kind === 'approval-request') expect(e.approvalId).toBe('a1')
    else throw new Error('expected approval-request')
  })

  it('parses an approval-resolved event and narrows the decision', () => {
    const e = AgentEvent.parse({ kind: 'approval-resolved', approvalId: 'a1', decision: 'always' })
    if (e.kind === 'approval-resolved') expect(e.decision).toBe('always')
    else throw new Error('expected approval-resolved')
  })

  it('rejects an approval-resolved with an unknown decision', () => {
    expect(() =>
      AgentEvent.parse({ kind: 'approval-resolved', approvalId: 'a1', decision: 'maybe' }),
    ).toThrow()
  })

  it('parses a tool-call with arbitrary args', () => {
    const e = AgentEvent.parse({
      kind: 'tool-call',
      toolCallId: 't1',
      name: 'Edit',
      args: { a: 1 },
    })
    expect(e.kind).toBe('tool-call')
  })

  it('rejects an unknown kind', () => {
    expect(() => AgentEvent.parse({ kind: 'nope' })).toThrow()
  })

  it('rejects a message missing text', () => {
    expect(() => AgentEvent.parse({ kind: 'message', role: 'user' })).toThrow()
  })

  it('parses a thinking event', () => {
    const e = AgentEvent.parse({ kind: 'thinking', text: 'let me reason', partial: true })
    if (e.kind === 'thinking') expect(e.partial).toBe(true)
    else throw new Error('expected thinking')
  })

  it('parses a thinking event without the optional partial flag', () => {
    const e = AgentEvent.parse({ kind: 'thinking', text: 'done reasoning' })
    if (e.kind === 'thinking') expect(e.partial).toBeUndefined()
    else throw new Error('expected thinking')
  })

  it('accumulates delta by an optional stable partId', () => {
    const e = AgentEvent.parse({ kind: 'delta', text: 'hel', partId: 'p1' })
    if (e.kind === 'delta') expect(e.partId).toBe('p1')
    else throw new Error('expected delta')
  })

  it('still parses a delta without a partId', () => {
    expect(AgentEvent.parse({ kind: 'delta', text: 'lo' }).kind).toBe('delta')
  })

  it('carries optional rich meta on usage', () => {
    const e = AgentEvent.parse({
      kind: 'usage',
      tokensIn: 10,
      tokensOut: 20,
      costMicros: 1500,
      model: 'claude-opus-4-8',
      durationMs: 4200,
      cacheReadTokens: 8,
    })
    if (e.kind === 'usage') {
      expect(e.model).toBe('claude-opus-4-8')
      expect(e.cacheReadTokens).toBe(8)
    } else throw new Error('expected usage')
  })

  it('still parses a usage event without the rich meta', () => {
    expect(AgentEvent.parse({ kind: 'usage', tokensIn: 1, tokensOut: 2 }).kind).toBe('usage')
  })

  it('parses a todo event with status items', () => {
    const e = AgentEvent.parse({
      kind: 'todo',
      items: [
        { content: 'write the test', status: 'completed' },
        { content: 'implement', status: 'in_progress' },
        { content: 'ship', status: 'pending' },
      ],
    })
    if (e.kind === 'todo') expect(e.items).toHaveLength(3)
    else throw new Error('expected todo')
  })

  it('rejects a todo item with an unknown status', () => {
    expect(() =>
      AgentEvent.parse({ kind: 'todo', items: [{ content: 'x', status: 'blocked' }] }),
    ).toThrow()
  })

  it('rejects a todo item with empty content', () => {
    expect(() =>
      AgentEvent.parse({ kind: 'todo', items: [{ content: '', status: 'pending' }] }),
    ).toThrow()
  })

  it('parses a command-list event through the union', () => {
    const e = AgentEvent.parse({
      kind: 'command-list',
      requestId: 'req1',
      commands: [{ name: 'commit', description: 'make a commit' }, { name: 'test' }],
    })
    if (e.kind === 'command-list') expect(e.commands).toHaveLength(2)
    else throw new Error('expected command-list')
  })
})
