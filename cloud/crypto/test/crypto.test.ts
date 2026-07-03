import { describe, it, expect } from 'vitest'
import type { AgentEvent } from '@pactify/wire'
import { generateRunKey, encryptEvent, decryptEvent } from '../src/crypto'

const samples: AgentEvent[] = [
  { kind: 'message', role: 'assistant', text: 'hello 世界' },
  { kind: 'approval-request', approvalId: 'a1', tool: 'Bash', detail: 'rm -rf /' },
  { kind: 'tool-call', toolCallId: 't1', name: 'Edit', args: { path: 'x.ts', n: 3 } },
  { kind: 'usage', tokensIn: 10, tokensOut: 4 },
]

describe('core crypto', () => {
  it('round-trips every AgentEvent kind', () => {
    const key = generateRunKey()
    for (const ev of samples) {
      const blob = encryptEvent(key, ev)
      expect(decryptEvent(key, blob)).toEqual(ev)
    }
  })

  it('produces a well-formed EncryptedBlob', () => {
    const blob = encryptEvent(generateRunKey(), samples[0]!)
    expect(blob.alg).toBe('xchacha20poly1305')
    expect(blob.nonce.length).toBeGreaterThan(0)
    expect(blob.ct.length).toBeGreaterThan(0)
  })

  it('uses a fresh nonce each call (ciphertexts differ)', () => {
    const key = generateRunKey()
    const a = encryptEvent(key, samples[0]!)
    const b = encryptEvent(key, samples[0]!)
    expect(a.nonce).not.toBe(b.nonce)
    expect(a.ct).not.toBe(b.ct)
  })

  it('fails to decrypt with the wrong key', () => {
    const blob = encryptEvent(generateRunKey(), samples[0]!)
    expect(() => decryptEvent(generateRunKey(), blob)).toThrow()
  })

  it('fails to decrypt tampered ciphertext', () => {
    const key = generateRunKey()
    const blob = encryptEvent(key, samples[0]!)
    const tampered = {
      ...blob,
      ct: blob.ct.slice(0, -2) + (blob.ct.endsWith('A') ? 'B' : 'A') + '=',
    }
    expect(() => decryptEvent(key, tampered)).toThrow()
  })

  it('generates 32-byte keys', () => {
    expect(generateRunKey().length).toBe(32)
  })
})
