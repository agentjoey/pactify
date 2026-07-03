import { describe, it, expect, beforeEach } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { listPushSubscriptions } from '../src/push'

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

async function login(app: ReturnType<typeof createServer>, challenge = 'c1') {
  const kp = keypair()
  const res = await app.inject({
    method: 'POST',
    url: '/v1/auth',
    payload: { publicKey: kp.pubHex, challenge, signature: sign(kp.priv, challenge) },
  })
  return { token: res.json().token as string, accountId: res.json().accountId as string }
}

function authed(token: string) {
  return { authorization: `Bearer ${token}` }
}

const SUB = {
  endpoint: 'https://push.example/endpoint-1',
  keys: { p256dh: 'p256dh-key', auth: 'auth-key' },
}

describe('http: POST /v1/push/subscribe', () => {
  it('401 without a token', async () => {
    const app = createServer({ db, secret: SECRET })
    const res = await app.inject({ method: 'POST', url: '/v1/push/subscribe', payload: SUB })
    expect(res.statusCode).toBe(401)
  })

  it('400 for a malformed subscription body', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token } = await login(app)
    const res = await app.inject({
      method: 'POST',
      url: '/v1/push/subscribe',
      headers: authed(token),
      payload: { endpoint: 'https://x', keys: { p256dh: 'only-one' } },
    })
    expect(res.statusCode).toBe(400)
  })

  it('persists the subscription (204) and is idempotent by endpoint', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)

    const first = await app.inject({
      method: 'POST',
      url: '/v1/push/subscribe',
      headers: authed(token),
      payload: SUB,
    })
    expect(first.statusCode).toBe(204)

    // Re-subscribe the same endpoint → still one row (idempotent).
    const second = await app.inject({
      method: 'POST',
      url: '/v1/push/subscribe',
      headers: authed(token),
      payload: SUB,
    })
    expect(second.statusCode).toBe(204)

    const subs = await listPushSubscriptions(db, accountId)
    expect(subs).toEqual([SUB])
  })
})

describe('http: DELETE /v1/push/subscribe', () => {
  it('removes a subscription by endpoint (204), then 404 on a second delete', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await app.inject({
      method: 'POST',
      url: '/v1/push/subscribe',
      headers: authed(token),
      payload: SUB,
    })

    const del = await app.inject({
      method: 'DELETE',
      url: '/v1/push/subscribe',
      headers: authed(token),
      payload: { endpoint: SUB.endpoint },
    })
    expect(del.statusCode).toBe(204)
    expect(await listPushSubscriptions(db, accountId)).toHaveLength(0)

    const again = await app.inject({
      method: 'DELETE',
      url: '/v1/push/subscribe',
      headers: authed(token),
      payload: { endpoint: SUB.endpoint },
    })
    expect(again.statusCode).toBe(404)
  })

  it('400 when no endpoint is supplied', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token } = await login(app)
    const res = await app.inject({
      method: 'DELETE',
      url: '/v1/push/subscribe',
      headers: authed(token),
      payload: {},
    })
    expect(res.statusCode).toBe(400)
  })
})

describe('http: GET /v1/push/vapid', () => {
  it('returns the configured public key', async () => {
    const app = createServer({ db, secret: SECRET, vapidPublicKey: 'BVAPIDPUBLICKEY' })
    const { token } = await login(app)
    const res = await app.inject({ method: 'GET', url: '/v1/push/vapid', headers: authed(token) })
    expect(res.statusCode).toBe(200)
    expect(res.json()).toEqual({ publicKey: 'BVAPIDPUBLICKEY' })
  })

  it('404 when push is unconfigured (no key)', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token } = await login(app)
    const res = await app.inject({ method: 'GET', url: '/v1/push/vapid', headers: authed(token) })
    expect(res.statusCode).toBe(404)
  })

  it('401 without a token', async () => {
    const app = createServer({ db, secret: SECRET, vapidPublicKey: 'BVAPIDPUBLICKEY' })
    const res = await app.inject({ method: 'GET', url: '/v1/push/vapid' })
    expect(res.statusCode).toBe(401)
  })
})
