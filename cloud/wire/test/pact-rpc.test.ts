import { describe, it, expect } from 'vitest'
import { RpcRequest } from '../src/rpc'

// The pactify pact-verb rpc types are additive members of the shared RpcRequest
// union (P-b of U3). These assert they parse through the union and that the
// discriminator + required fields hold — the wire contract the relay validates
// and pactify serve (internal/remoteexec) dispatches.
describe('RpcRequest: pactify pact.* control messages', () => {
  it('parses pact.assign with deps', () => {
    const r = RpcRequest.parse({
      type: 'pact.assign',
      machineId: 'm1',
      project: 'demo',
      task: 't1',
      feature: 'f1',
      branch: 'feat-f1',
      owner: 'kimi',
      reviewer: 'claude',
      spec: '.pact/tasks/t1.md',
      deps: ['t0'],
    })
    expect(r.type).toBe('pact.assign')
    if (r.type === 'pact.assign') expect(r.deps).toEqual(['t0'])
  })

  it('parses pact.accept / changes / merge / checkpoint', () => {
    expect(RpcRequest.parse({ type: 'pact.accept', machineId: 'm1', project: 'p', task: 't1' }).type).toBe('pact.accept')
    expect(RpcRequest.parse({ type: 'pact.changes', machineId: 'm1', project: 'p', task: 't1', reason: 'fix' }).type).toBe('pact.changes')
    expect(RpcRequest.parse({ type: 'pact.merge', machineId: 'm1', project: 'p', feature: 'f1' }).type).toBe('pact.merge')
    expect(RpcRequest.parse({ type: 'pact.checkpoint', machineId: 'm1', project: 'p', task: 't1', evidence: 'green' }).type).toBe('pact.checkpoint')
  })

  it('rejects a pact.assign missing required fields', () => {
    expect(() => RpcRequest.parse({ type: 'pact.assign', machineId: 'm1', project: 'p' })).toThrow()
  })

  it('requires machineId (machine-targeted routing)', () => {
    expect(() => RpcRequest.parse({ type: 'pact.accept', project: 'p', task: 't1' })).toThrow()
  })
})
