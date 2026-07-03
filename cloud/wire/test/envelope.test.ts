import { describe, it, expect } from 'vitest'
import { EncryptedBlob, WireMessage } from '../src/envelope'

const header = {
  v: 1,
  machineId: 'm1',
  runId: 'r1',
  seq: 3,
  ts: 1719100000000,
  state: 'awaiting-approval',
  eventKind: 'approval-request',
  pendingApprovals: 1,
  tokensIn: 0,
  tokensOut: 0,
}
const body = { alg: 'xchacha20poly1305', nonce: 'bm9uY2U=', ct: 'Y2lwaGVy' }

describe('EncryptedBlob', () => {
  it('parses a valid blob', () => {
    expect(EncryptedBlob.parse(body).ct).toBe('Y2lwaGVy')
  })
  it('rejects a wrong alg', () => {
    expect(() => EncryptedBlob.parse({ ...body, alg: 'aes' })).toThrow()
  })
  it('rejects empty ciphertext', () => {
    expect(() => EncryptedBlob.parse({ ...body, ct: '' })).toThrow()
  })
})

describe('WireMessage', () => {
  it('parses header + body together', () => {
    const msg = WireMessage.parse({ header, body })
    expect(msg.header.runId).toBe('r1')
    expect(msg.body.alg).toBe('xchacha20poly1305')
  })
  it('rejects a message missing the body', () => {
    expect(() => WireMessage.parse({ header })).toThrow()
  })
})
