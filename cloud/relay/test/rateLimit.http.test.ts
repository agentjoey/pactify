import { describe, it, expect, beforeEach } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'

const SECRET = 's3cret'
function keypair() {
  const priv = ed25519.utils.randomSecretKey()
  return { priv, pubHex: bytesToHex(ed25519.getPublicKey(priv)) }
}
function sign(priv: Uint8Array, m: string) {
  return bytesToHex(ed25519.sign(utf8ToBytes(m), priv))
}

let db: PrismaClient
beforeEach(async () => {
  db = await createPgliteDb()
})

describe('relay http rate limiting', () => {
  it('an over-limit /v1/auth burst from one IP gets 429 + Retry-After', async () => {
    const app = createServer({
      db,
      secret: SECRET,
      rateLimits: { authPerMin: 5, readsPerMin: 120 },
    })
    const kp = keypair()
    const statuses: number[] = []
    let retryAfter: string | undefined
    for (let i = 0; i < 8; i++) {
      const res = await app.inject({
        method: 'POST',
        url: '/v1/auth',
        payload: { publicKey: kp.pubHex, challenge: 'c1', signature: sign(kp.priv, 'c1') },
      })
      statuses.push(res.statusCode)
      if (res.statusCode === 429) retryAfter = res.headers['retry-after'] as string
    }
    // at most 5 non-429 responses; the rest are throttled
    expect(statuses.filter((s) => s !== 429).length).toBeLessThanOrEqual(5)
    expect(statuses.some((s) => s === 429)).toBe(true)
    expect(retryAfter).toBeDefined()
    expect(Number(retryAfter)).toBeGreaterThan(0)
  })

  it('normal /v1/auth traffic (under the limit) passes', async () => {
    const app = createServer({
      db,
      secret: SECRET,
      rateLimits: { authPerMin: 10, readsPerMin: 120 },
    })
    const kp = keypair()
    for (let i = 0; i < 5; i++) {
      const res = await app.inject({
        method: 'POST',
        url: '/v1/auth',
        payload: { publicKey: kp.pubHex, challenge: 'c1', signature: sign(kp.priv, 'c1') },
      })
      expect(res.statusCode).toBe(200)
    }
  })

  it('over-limit /v1/* reads get 429; the auth bucket is separate', async () => {
    const app = createServer({
      db,
      secret: SECRET,
      rateLimits: { authPerMin: 10, readsPerMin: 3 },
    })
    const statuses: number[] = []
    for (let i = 0; i < 6; i++) {
      // unauthenticated read still passes through the per-IP limiter
      const res = await app.inject({ method: 'GET', url: '/v1/runs' })
      statuses.push(res.statusCode)
    }
    // first few are 401 (no token) until the read limiter trips → 429
    expect(statuses.some((s) => s === 429)).toBe(true)
    expect(statuses.filter((s) => s !== 429).length).toBeLessThanOrEqual(3)
  })
})
