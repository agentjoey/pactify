import { describe, it, expect } from 'vitest'
import { RpcRequest, RpcResponse, AgentKind } from '../src/rpc'

describe('RpcRequest', () => {
  it('parses a spawn request', () => {
    const r = RpcRequest.parse({
      type: 'spawn',
      machineId: 'm1',
      agentKind: 'claude',
      workdir: '/repo',
      task: 'fix the test',
    })
    expect(r.type).toBe('spawn')
  })

  it('parses a spawn request carrying an optional title', () => {
    const r = RpcRequest.parse({
      type: 'spawn',
      machineId: 'm1',
      agentKind: 'claude',
      workdir: '/repo',
      task: 'fix the test',
      title: 'Session name',
    })
    if (r.type === 'spawn') expect(r.title).toBe('Session name')
    else throw new Error('expected spawn')
  })

  it('still parses a spawn without a title (title omitted)', () => {
    const r = RpcRequest.parse({
      type: 'spawn',
      machineId: 'm1',
      agentKind: 'claude',
      workdir: '/repo',
    })
    if (r.type === 'spawn') expect(r.title).toBeUndefined()
    else throw new Error('expected spawn')
  })

  it('rejects a spawn with a title over 200 chars', () => {
    expect(() =>
      RpcRequest.parse({
        type: 'spawn',
        machineId: 'm1',
        agentKind: 'claude',
        workdir: '/repo',
        title: 'x'.repeat(201),
      }),
    ).toThrow()
  })

  it('parses a resolve-approval request', () => {
    const r = RpcRequest.parse({
      type: 'resolve-approval',
      runId: 'r1',
      approvalId: 'a1',
      decision: 'approve',
    })
    if (r.type === 'resolve-approval') expect(r.decision).toBe('approve')
    else throw new Error('expected resolve-approval')
  })

  it('rejects an unknown type', () => {
    expect(() => RpcRequest.parse({ type: 'nope' })).toThrow()
  })

  it('rejects a spawn with an unknown agentKind', () => {
    expect(() =>
      RpcRequest.parse({ type: 'spawn', machineId: 'm1', agentKind: 'cursor', workdir: '/r' }),
    ).toThrow()
  })

  it('enumerates the agent kinds', () => {
    expect(AgentKind.options).toContain('opencode')
  })

  // Kept in lockstep with pactify's pactToWireKind (internal/serve/remotechannel.go):
  // a machine announcing a kind outside this enum makes the relay's throwing
  // MachineInfo.parse blow up the whole account's machine list.
  it('enumerates every drivable kind pactify advertises', () => {
    expect([...AgentKind.options].sort()).toEqual([
      'antigravity',
      'claude',
      'codex',
      'gemini',
      'kimi',
      'opencode',
    ])
  })

  it('accepts a spawn with the antigravity (agy) kind', () => {
    const r = RpcRequest.parse({
      type: 'spawn',
      machineId: 'm1',
      agentKind: 'antigravity',
      workdir: '/repo',
    })
    if (r.type === 'spawn') expect(r.agentKind).toBe('antigravity')
    else throw new Error('expected spawn')
  })

  it('parses a list-files request', () => {
    const r = RpcRequest.parse({
      type: 'list-files',
      runId: 'r1',
      requestId: 'req1',
      query: 'src/',
    })
    if (r.type === 'list-files') expect(r.requestId).toBe('req1')
    else throw new Error('expected list-files')
  })

  it('rejects a list-files missing requestId', () => {
    expect(() => RpcRequest.parse({ type: 'list-files', runId: 'r1', query: 'src/' })).toThrow()
  })

  it('parses a set-model request', () => {
    const r = RpcRequest.parse({ type: 'set-model', runId: 'r1', modelId: 'opus' })
    if (r.type === 'set-model') expect(r.modelId).toBe('opus')
    else throw new Error('expected set-model')
  })

  it('parses a set-mode request', () => {
    const r = RpcRequest.parse({ type: 'set-mode', runId: 'r1', modeId: 'plan' })
    if (r.type === 'set-mode') expect(r.modeId).toBe('plan')
    else throw new Error('expected set-mode')
  })

  it('accepts the always decision on resolve-approval', () => {
    const r = RpcRequest.parse({
      type: 'resolve-approval',
      runId: 'r1',
      approvalId: 'a1',
      decision: 'always',
    })
    if (r.type === 'resolve-approval') expect(r.decision).toBe('always')
    else throw new Error('expected resolve-approval')
  })

  it('parses a discover request', () => {
    const r = RpcRequest.parse({ type: 'discover', machineId: 'm1', requestId: 'req1' })
    if (r.type === 'discover') expect(r.requestId).toBe('req1')
    else throw new Error('expected discover')
  })

  it('rejects a discover missing requestId', () => {
    expect(() => RpcRequest.parse({ type: 'discover', machineId: 'm1' })).toThrow()
  })

  it('round-trips a discover request', () => {
    const input = { type: 'discover', machineId: 'm1', requestId: 'req1' }
    expect(RpcRequest.parse(JSON.parse(JSON.stringify(input)))).toEqual(input)
  })

  it('parses a list-dirs request with an explicit path', () => {
    const r = RpcRequest.parse({
      type: 'list-dirs',
      machineId: 'm1',
      requestId: 'req1',
      path: '/repo/src',
    })
    if (r.type === 'list-dirs') expect(r.path).toBe('/repo/src')
    else throw new Error('expected list-dirs')
  })

  it('parses a list-dirs request without a path (defaults at the daemon)', () => {
    const r = RpcRequest.parse({ type: 'list-dirs', machineId: 'm1', requestId: 'req1' })
    if (r.type === 'list-dirs') expect(r.path).toBeUndefined()
    else throw new Error('expected list-dirs')
  })

  it('rejects a list-dirs missing requestId', () => {
    expect(() => RpcRequest.parse({ type: 'list-dirs', machineId: 'm1' })).toThrow()
  })

  it('rejects a list-dirs missing machineId', () => {
    expect(() => RpcRequest.parse({ type: 'list-dirs', requestId: 'req1' })).toThrow()
  })

  it('round-trips a list-dirs request', () => {
    const input = { type: 'list-dirs', machineId: 'm1', requestId: 'req1', path: '/repo' }
    expect(RpcRequest.parse(JSON.parse(JSON.stringify(input)))).toEqual(input)
  })

  it('parses an attach request', () => {
    const r = RpcRequest.parse({
      type: 'attach',
      machineId: 'm1',
      agentKind: 'claude',
      sessionId: 's1',
    })
    if (r.type === 'attach') expect(r.sessionId).toBe('s1')
    else throw new Error('expected attach')
  })

  it('rejects an attach with an unknown agentKind', () => {
    expect(() =>
      RpcRequest.parse({ type: 'attach', machineId: 'm1', agentKind: 'cursor', sessionId: 's1' }),
    ).toThrow()
  })

  it('round-trips an attach request', () => {
    const input = { type: 'attach', machineId: 'm1', agentKind: 'claude', sessionId: 's1' }
    expect(RpcRequest.parse(JSON.parse(JSON.stringify(input)))).toEqual(input)
  })

  it('carries optional images on send-message', () => {
    const r = RpcRequest.parse({
      type: 'send-message',
      runId: 'r1',
      text: 'look at this',
      images: [{ data: 'aGk=', mimeType: 'image/png' }],
    })
    if (r.type === 'send-message') expect(r.images?.[0].mimeType).toBe('image/png')
    else throw new Error('expected send-message')
  })

  it('still parses a send-message without images', () => {
    expect(RpcRequest.parse({ type: 'send-message', runId: 'r1', text: 'hi' }).type).toBe(
      'send-message',
    )
  })

  it('parses a list-commands request', () => {
    const r = RpcRequest.parse({ type: 'list-commands', runId: 'r1', requestId: 'req1' })
    if (r.type === 'list-commands') expect(r.requestId).toBe('req1')
    else throw new Error('expected list-commands')
  })

  it('rejects a list-commands missing requestId', () => {
    expect(() => RpcRequest.parse({ type: 'list-commands', runId: 'r1' })).toThrow()
  })

  it('parses a run-command request', () => {
    const r = RpcRequest.parse({ type: 'run-command', runId: 'r1', name: 'commit' })
    if (r.type === 'run-command') expect(r.name).toBe('commit')
    else throw new Error('expected run-command')
  })

  it('rejects a run-command with an empty name', () => {
    expect(() => RpcRequest.parse({ type: 'run-command', runId: 'r1', name: '' })).toThrow()
  })

  it('parses a rename request', () => {
    const r = RpcRequest.parse({ type: 'rename', runId: 'r1', title: 'new title' })
    if (r.type === 'rename') expect(r.title).toBe('new title')
    else throw new Error('expected rename')
  })

  it('rejects a rename with an empty title', () => {
    expect(() => RpcRequest.parse({ type: 'rename', runId: 'r1', title: '' })).toThrow()
  })

  it('rejects a rename with a title over 200 chars', () => {
    expect(() =>
      RpcRequest.parse({ type: 'rename', runId: 'r1', title: 'x'.repeat(201) }),
    ).toThrow()
  })
})

describe('RpcResponse', () => {
  it('parses an ok response', () => {
    expect(RpcResponse.parse({ ok: true, runId: 'r1' }).ok).toBe(true)
  })
  it('parses an error response', () => {
    expect(RpcResponse.parse({ ok: false, error: 'no such machine' }).error).toBe('no such machine')
  })
})
