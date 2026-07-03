import { describe, it, expect } from 'vitest'
import { ClientWsMessage, ServerWsMessage } from '../src/ws'

describe('ClientWsMessage', () => {
  it('parses subscribe with afterSeqByRun and round-trips', () => {
    const msg = {
      type: 'subscribe',
      runIds: ['r1', 'r2'],
      afterSeqByRun: { r1: 5, r2: 0 },
    }
    expect(ClientWsMessage.parse(msg)).toEqual(msg)
  })

  it('parses a bare subscribe', () => {
    expect(ClientWsMessage.parse({ type: 'subscribe' })).toEqual({ type: 'subscribe' })
  })

  it('parses unsubscribe', () => {
    const msg = { type: 'unsubscribe', runIds: ['r1'] }
    expect(ClientWsMessage.parse(msg)).toEqual(msg)
  })

  it('parses ping', () => {
    expect(ClientWsMessage.parse({ type: 'ping' })).toEqual({ type: 'ping' })
  })

  it('rejects a bad discriminant', () => {
    expect(ClientWsMessage.safeParse({ type: 'nope' }).success).toBe(false)
  })

  it('rejects unsubscribe missing runIds', () => {
    expect(ClientWsMessage.safeParse({ type: 'unsubscribe' }).success).toBe(false)
  })

  it('rejects a non-number afterSeqByRun value', () => {
    expect(
      ClientWsMessage.safeParse({ type: 'subscribe', afterSeqByRun: { r1: 'x' } }).success,
    ).toBe(false)
  })
})

describe('ServerWsMessage', () => {
  const body = { alg: 'xchacha20poly1305', nonce: 'bm9uY2U=', ct: 'Y3Q=' }

  it('parses an event and round-trips', () => {
    const msg = { type: 'event', runId: 'r1', seq: 3, body }
    expect(ServerWsMessage.parse(msg)).toEqual(msg)
  })

  it('parses run-updated', () => {
    const summary = {
      id: 'r1',
      agentKind: 'claude',
      state: 'idle',
      pendingApprovals: 0,
      tokensIn: 0,
      tokensOut: 0,
      machineId: 'm1',
      seq: 1,
      updatedAt: 1719000000000,
    }
    const msg = { type: 'run-updated', summary }
    expect(ServerWsMessage.parse(msg)).toEqual(msg)
  })

  it('parses replay-end', () => {
    const msg = { type: 'replay-end', runId: 'r1', lastSeq: 9 }
    expect(ServerWsMessage.parse(msg)).toEqual(msg)
  })

  it('parses machines', () => {
    const msg = {
      type: 'machines',
      machines: [
        { machineId: 'm1', agentKinds: ['claude'], online: true, lastSeenAt: 1719000000000 },
      ],
    }
    expect(ServerWsMessage.parse(msg)).toEqual(msg)
  })

  it('parses pong', () => {
    expect(ServerWsMessage.parse({ type: 'pong' })).toEqual({ type: 'pong' })
  })

  it('rejects a bad discriminant', () => {
    expect(ServerWsMessage.safeParse({ type: 'nope' }).success).toBe(false)
  })

  it('rejects an event with a malformed body', () => {
    expect(
      ServerWsMessage.safeParse({ type: 'event', runId: 'r1', seq: 3, body: { alg: 'aes' } })
        .success,
    ).toBe(false)
  })

  it('rejects run-updated with an invalid summary', () => {
    expect(ServerWsMessage.safeParse({ type: 'run-updated', summary: { id: 'r1' } }).success).toBe(
      false,
    )
  })
})
