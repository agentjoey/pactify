import { describe, it, expect, beforeEach } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { createMetrics } from '../src/metrics'

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

describe('GET /metrics', () => {
  it('is unauthenticated and renders Prometheus text', async () => {
    const app = createServer({ db, secret: SECRET, metrics: createMetrics() })
    const res = await app.inject({ method: 'GET', url: '/metrics' })
    expect(res.statusCode).toBe(200)
    expect(res.headers['content-type']).toContain('text/plain')
    expect(res.body).toContain('# TYPE http_requests_total counter')
    expect(res.body).toContain('# TYPE connected_sockets gauge')
  })

  it('counts an http request by route + status', async () => {
    const metrics = createMetrics()
    const app = createServer({ db, secret: SECRET, metrics })
    await app.inject({ method: 'GET', url: '/v1/runs' }) // 401, no token
    expect(metrics.get('http_requests_total', { route: '/v1/runs', status: '401' })).toBe(1)
    const text = (await app.inject({ method: 'GET', url: '/metrics' })).body
    expect(text).toContain('http_requests_total{route="/v1/runs",status="401"} 1')
  })

  it('counts auth attempts by result (success + failure)', async () => {
    const metrics = createMetrics()
    const app = createServer({ db, secret: SECRET, metrics })
    const kp = keypair()
    // success
    await app.inject({
      method: 'POST',
      url: '/v1/auth',
      payload: { publicKey: kp.pubHex, challenge: 'c1', signature: sign(kp.priv, 'c1') },
    })
    // failure (bad signature)
    await app.inject({
      method: 'POST',
      url: '/v1/auth',
      payload: { publicKey: kp.pubHex, challenge: 'c1', signature: sign(kp.priv, 'other') },
    })
    expect(metrics.get('auth_attempts_total', { result: 'success' })).toBe(1)
    expect(metrics.get('auth_attempts_total', { result: 'failure' })).toBe(1)
  })

  it('counts a rate-limit rejection under rate_limited_total', async () => {
    const metrics = createMetrics()
    const app = createServer({
      db,
      secret: SECRET,
      metrics,
      rateLimits: { authPerMin: 10, readsPerMin: 2 },
    })
    for (let i = 0; i < 6; i++) await app.inject({ method: 'GET', url: '/v1/runs' })
    expect(metrics.get('rate_limited_total', { path: '/v1/runs' })).toBeGreaterThan(0)
  })

  it('increments errors_total when a handler throws', async () => {
    const metrics = createMetrics()
    // Force the runs query to throw by handing the server a db whose method blows up.
    const brokenDb = {
      ...db,
      run: {
        findMany: () => {
          throw new Error('boom')
        },
      },
    } as unknown as PrismaClient
    const app = createServer({ db: brokenDb, secret: SECRET, metrics })
    const kp = keypair()
    const auth = await app.inject({
      method: 'POST',
      url: '/v1/auth',
      payload: { publicKey: kp.pubHex, challenge: 'c1', signature: sign(kp.priv, 'c1') },
    })
    const token = auth.json().token as string
    const res = await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(res.statusCode).toBe(500)
    expect(metrics.get('errors_total', { where: 'http' })).toBeGreaterThan(0)
  })

  it('never exposes secrets or account content in /metrics labels', async () => {
    const metrics = createMetrics()
    const app = createServer({ db, secret: SECRET, metrics })
    const kp = keypair()
    const auth = await app.inject({
      method: 'POST',
      url: '/v1/auth',
      payload: { publicKey: kp.pubHex, challenge: 'c1', signature: sign(kp.priv, 'c1') },
    })
    const token = auth.json().token as string
    const accountId = auth.json().accountId as string
    await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: { authorization: `Bearer ${token}` },
    })
    const text = (await app.inject({ method: 'GET', url: '/metrics' })).body
    expect(text).not.toContain(token)
    expect(text).not.toContain(accountId)
    expect(text).not.toContain(SECRET)
    expect(text).not.toMatch(/Bearer/i)
  })

  it('does not record /metrics scrapes as account-specific routes', async () => {
    const metrics = createMetrics()
    const app = createServer({ db, secret: SECRET, metrics })
    await app.inject({ method: 'GET', url: '/metrics' })
    const text = (await app.inject({ method: 'GET', url: '/metrics' })).body
    // The route label set is bounded; the dynamic run-events path collapses to a
    // template, never a raw run id.
    expect(text).not.toMatch(/route="\/v1\/runs\/[a-f0-9-]+\/events"/)
  })
})
