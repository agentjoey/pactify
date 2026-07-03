import { describe, it, expect } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import { createPgliteDb } from '../src/db'
import {
  verifyChallenge,
  issueToken,
  verifyToken,
  authenticate,
  timingSafeEqualStr,
} from '../src/auth'

describe('timingSafeEqualStr', () => {
  it('returns true for equal strings', () => {
    expect(timingSafeEqualStr('hunter2', 'hunter2')).toBe(true)
    expect(timingSafeEqualStr('', '')).toBe(true)
  })

  it('returns false for same-length but different strings', () => {
    expect(timingSafeEqualStr('hunter2', 'hunter3')).toBe(false)
    expect(timingSafeEqualStr('abc', 'xyz')).toBe(false)
  })

  it('returns false (no throw) for different-length strings', () => {
    expect(() => timingSafeEqualStr('short', 'longer-value')).not.toThrow()
    expect(timingSafeEqualStr('short', 'longer-value')).toBe(false)
    expect(timingSafeEqualStr('a', '')).toBe(false)
    expect(timingSafeEqualStr('', 'a')).toBe(false)
  })
})

function keypair() {
  const priv = ed25519.utils.randomSecretKey()
  const pub = ed25519.getPublicKey(priv)
  return { priv, pubHex: bytesToHex(pub) }
}
function sign(priv: Uint8Array, challenge: string) {
  return bytesToHex(ed25519.sign(utf8ToBytes(challenge), priv))
}

describe('verifyChallenge', () => {
  it('accepts a valid signature and rejects tampering', () => {
    const { priv, pubHex } = keypair()
    const sig = sign(priv, 'chal-123')
    expect(verifyChallenge(pubHex, 'chal-123', sig)).toBe(true)
    expect(verifyChallenge(pubHex, 'chal-XXX', sig)).toBe(false)
    expect(verifyChallenge(keypair().pubHex, 'chal-123', sig)).toBe(false)
    expect(verifyChallenge(pubHex, 'chal-123', 'deadbeef')).toBe(false)
  })
})

describe('issueToken / verifyToken', () => {
  it('round-trips and rejects expiry/tampering/wrong-secret', () => {
    const t = issueToken('s3cret', 'acc1', 60_000, 1000)
    expect(verifyToken('s3cret', t, 2000)).toEqual({ accountId: 'acc1' })
    expect(verifyToken('s3cret', t, 1_000_000)).toBeNull() // expired
    expect(verifyToken('other', t, 2000)).toBeNull() // wrong secret
    expect(verifyToken('s3cret', t + 'x', 2000)).toBeNull() // tampered sig
    expect(verifyToken('s3cret', 'garbage', 2000)).toBeNull()
  })

  it('rejects a length-mismatched signature without throwing', () => {
    const t = issueToken('s3cret', 'acc1', 60_000, 1000)
    const body = t.slice(0, t.indexOf('.'))
    // a signature shorter than the real mac must be rejected (no timingSafeEqual throw)
    expect(verifyToken('s3cret', `${body}.short`, 2000)).toBeNull()
    expect(verifyToken('s3cret', `${body}.`, 2000)).toBeNull()
  })
})

describe('authenticate', () => {
  it('verifies, upserts the account, returns a token; rejects bad signatures', async () => {
    const db = await createPgliteDb()
    const { priv, pubHex } = keypair()
    const ok = await authenticate(
      db,
      's3cret',
      { publicKey: pubHex, challenge: 'c1', signature: sign(priv, 'c1') },
      60_000,
      1000,
    )
    expect(ok).not.toBeNull()
    expect(verifyToken('s3cret', ok!.token, 2000)).toEqual({ accountId: ok!.accountId })
    // upsert: same key → same account
    const again = await authenticate(
      db,
      's3cret',
      { publicKey: pubHex, challenge: 'c2', signature: sign(priv, 'c2') },
      60_000,
      1000,
    )
    expect(again!.accountId).toBe(ok!.accountId)
    // bad signature → null, no new account
    const bad = await authenticate(
      db,
      's3cret',
      { publicKey: pubHex, challenge: 'c3', signature: 'deadbeef' },
      60_000,
      1000,
    )
    expect(bad).toBeNull()
    expect(await db.account.count()).toBe(1)
  })
})
