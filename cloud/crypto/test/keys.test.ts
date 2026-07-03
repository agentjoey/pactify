import { describe, it, expect } from 'vitest'
import { generateMasterSecret, deriveRunKey, deriveAccountKeypair } from '../src/keys'
import { encryptEvent, decryptEvent } from '../src/crypto'
import { ed25519 } from '@noble/curves/ed25519.js'
import { utf8ToBytes, hexToBytes } from '@noble/hashes/utils.js'
import type { AgentEvent } from '@pactify-apps/wire'

describe('derived run keys', () => {
  it('generateMasterSecret is 32 random bytes', () => {
    const a = generateMasterSecret()
    const b = generateMasterSecret()
    expect(a.length).toBe(32)
    expect(Buffer.from(a).equals(Buffer.from(b))).toBe(false)
  })

  it('deriveRunKey is deterministic per (master, runId) and 32 bytes', () => {
    const master = generateMasterSecret()
    const k1 = deriveRunKey(master, 'r1')
    const k2 = deriveRunKey(master, 'r1')
    const k3 = deriveRunKey(master, 'r2')
    expect(k1.length).toBe(32)
    expect(Buffer.from(k1).equals(Buffer.from(k2))).toBe(true) // same inputs → same key
    expect(Buffer.from(k1).equals(Buffer.from(k3))).toBe(false) // different runId → different key
    // a different master yields a different key for the same runId
    expect(Buffer.from(k1).equals(Buffer.from(deriveRunKey(generateMasterSecret(), 'r1')))).toBe(
      false,
    )
  })

  it('a body encrypted with a derived key round-trips when the key is re-derived', () => {
    const master = generateMasterSecret()
    const ev: AgentEvent = { kind: 'message', role: 'assistant', text: 'secret' }
    const blob = encryptEvent(deriveRunKey(master, 'r1'), ev)
    expect(decryptEvent(deriveRunKey(master, 'r1'), blob)).toEqual(ev) // re-derived key decrypts
    expect(() => decryptEvent(deriveRunKey(master, 'r2'), blob)).toThrow() // wrong run's key fails
  })
})

describe('deriveAccountKeypair', () => {
  it('is deterministic and produces a verifiable signer', () => {
    const master = generateMasterSecret()
    const kp1 = deriveAccountKeypair(master)
    const kp2 = deriveAccountKeypair(master)
    expect(kp1.publicKeyHex).toBe(kp2.publicKeyHex) // deterministic
    expect(kp1.publicKeyHex).not.toBe(deriveAccountKeypair(generateMasterSecret()).publicKeyHex)
    const sig = kp1.sign('challenge-1')
    expect(
      ed25519.verify(hexToBytes(sig), utf8ToBytes('challenge-1'), hexToBytes(kp1.publicKeyHex)),
    ).toBe(true)
    expect(
      ed25519.verify(
        hexToBytes(kp1.sign('challenge-1')),
        utf8ToBytes('other'),
        hexToBytes(kp1.publicKeyHex),
      ),
    ).toBe(false)
  })
})
