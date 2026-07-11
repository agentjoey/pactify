import { describe, it, expect, beforeEach } from 'vitest'
import type { FastifyInstance } from 'fastify'
import type { PrismaClient } from '@prisma/client'
import { deriveAccountKeypair, generateMasterSecret } from '@pactify-apps/crypto'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { issueSessionToken, sessionIdOf, SESSION_TTL_MS } from '../src/identity/session'
import { csrfTokenFor } from '../src/identity/csrf'
import type { IdentityPlaneConfig } from '../src/identity/types'

const SECRET = 's3cret'
const WEB_URL = 'https://web.test'

const BASE_IDENTITY: IdentityPlaneConfig = {
  webUrl: WEB_URL,
  githubClientId: 'gh_client',
  githubClientSecret: 'gh_secret',
}

let db: PrismaClient
beforeEach(async () => {
  db = await createPgliteDb()
})

function createApp(opts: { identity?: IdentityPlaneConfig; now?: () => number } = {}) {
  return createServer({
    db,
    secret: SECRET,
    identity: opts.identity ?? BASE_IDENTITY,
    now: opts.now,
  })
}

async function createSession(
  app: FastifyInstance,
  email: string,
  opts: { ua?: string; now?: () => number } = {},
) {
  const user = await db.user.create({ data: { email } })
  const token = issueSessionToken()
  const session = await db.webSession.create({
    data: {
      id: sessionIdOf(SECRET, token),
      userId: user.id,
      expiresAt: new Date((opts.now ? opts.now() : Date.now()) + SESSION_TTL_MS),
      ua: opts.ua,
    },
  })
  const csrf = csrfTokenFor(SECRET, session.id)
  return { user, token, sessionId: session.id, csrf }
}

describe('/v1/id/accounts', () => {
  it('creates a free account and makes the user the owner', async () => {
    const app = createApp()
    const { token, csrf } = await createSession(app, 'new@example.com')
    const master = generateMasterSecret()
    const { publicKeyHex } = deriveAccountKeypair(master)

    const res = await app.inject({
      method: 'POST',
      url: '/v1/id/accounts',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { publicKey: publicKeyHex },
    })
    expect(res.statusCode).toBe(200)
    const body = res.json() as { accountId: string }

    const account = await db.account.findUnique({ where: { id: body.accountId } })
    expect(account?.publicKey).toBe(publicKeyHex)
    expect(account?.tier).toBe('free')

    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(me.json()).toMatchObject({
      accounts: [{ accountId: body.accountId, role: 'owner', tier: 'free' }],
    })
  })

  it('returns 409 when the public key is already claimed', async () => {
    const app = createApp()
    const { token, csrf } = await createSession(app, 'owner@example.com')
    const master = generateMasterSecret()
    const { publicKeyHex } = deriveAccountKeypair(master)

    const first = await app.inject({
      method: 'POST',
      url: '/v1/id/accounts',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { publicKey: publicKeyHex },
    })
    expect(first.statusCode).toBe(200)

    const { token: token2, csrf: csrf2 } = await createSession(app, 'other@example.com')
    const second = await app.inject({
      method: 'POST',
      url: '/v1/id/accounts',
      cookies: { aw_session: token2 },
      headers: { 'x-aw-csrf': csrf2 },
      payload: { publicKey: publicKeyHex },
    })
    expect(second.statusCode).toBe(409)
  })
})

describe('/v1/id/link', () => {
  it('links an existing account after signing the session-bound challenge', async () => {
    const app = createApp()
    const master = generateMasterSecret()
    const { publicKeyHex, sign } = deriveAccountKeypair(master)
    const account = await db.account.create({ data: { publicKey: publicKeyHex } })
    const { token, csrf } = await createSession(app, 'link@example.com')

    const challengeRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link/challenge',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    expect(challengeRes.statusCode).toBe(200)
    const { challenge } = challengeRes.json() as { challenge: string }

    const linkRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { publicKey: publicKeyHex, challenge, signature: sign(challenge) },
    })
    expect(linkRes.statusCode).toBe(200)
    expect((linkRes.json() as { accountId: string }).accountId).toBe(account.id)

    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(me.json()).toMatchObject({
      accounts: [{ accountId: account.id, role: 'owner', tier: 'free' }],
    })
  })

  it('rejects a bad signature without leaking account existence', async () => {
    const app = createApp()
    const master = generateMasterSecret()
    const { publicKeyHex } = deriveAccountKeypair(master)
    await db.account.create({ data: { publicKey: publicKeyHex } })
    const { token, csrf } = await createSession(app, 'bad@example.com')

    const challengeRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link/challenge',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    const { challenge } = challengeRes.json() as { challenge: string }

    const linkRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { publicKey: publicKeyHex, challenge, signature: '00'.repeat(64) },
    })
    expect(linkRes.statusCode).toBe(401)
  })

  it('rejects an expired challenge', async () => {
    let t = 0
    const app = createApp({ now: () => t })
    const master = generateMasterSecret()
    const { publicKeyHex, sign } = deriveAccountKeypair(master)
    await db.account.create({ data: { publicKey: publicKeyHex } })
    const { token, csrf } = await createSession(app, 'expired@example.com', { now: () => t })

    const challengeRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link/challenge',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    const { challenge } = challengeRes.json() as { challenge: string }

    t = 5 * 60 * 1_000 + 1
    const linkRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { publicKey: publicKeyHex, challenge, signature: sign(challenge) },
    })
    expect(linkRes.statusCode).toBe(401)
  })

  it('rejects a challenge bound to a different session', async () => {
    const app = createApp()
    const master = generateMasterSecret()
    const { publicKeyHex, sign } = deriveAccountKeypair(master)
    await db.account.create({ data: { publicKey: publicKeyHex } })
    const { token: tokenA, csrf: csrfA } = await createSession(app, 'a@example.com')
    const { token: tokenB, csrf: csrfB } = await createSession(app, 'b@example.com')

    const challengeRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link/challenge',
      cookies: { aw_session: tokenA },
      headers: { 'x-aw-csrf': csrfA },
    })
    const { challenge } = challengeRes.json() as { challenge: string }

    const linkRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link',
      cookies: { aw_session: tokenB },
      headers: { 'x-aw-csrf': csrfB },
      payload: { publicKey: publicKeyHex, challenge, signature: sign(challenge) },
    })
    expect(linkRes.statusCode).toBe(401)
  })

  it('returns 404 when the public key has no account', async () => {
    const app = createApp()
    const master = generateMasterSecret()
    const { publicKeyHex, sign } = deriveAccountKeypair(master)
    const { token, csrf } = await createSession(app, ' orphan@example.com')

    const challengeRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link/challenge',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    const { challenge } = challengeRes.json() as { challenge: string }

    const linkRes = await app.inject({
      method: 'POST',
      url: '/v1/id/link',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { publicKey: publicKeyHex, challenge, signature: sign(challenge) },
    })
    expect(linkRes.statusCode).toBe(404)
  })
})

describe('/v1/id/token', () => {
  it('issues a bearer isomorphic to /v1/auth for a member account', async () => {
    const app = createApp()
    const master = generateMasterSecret()
    const { publicKeyHex } = deriveAccountKeypair(master)
    const { token, csrf, user } = await createSession(app, 'token@example.com')
    const account = await db.account.create({ data: { publicKey: publicKeyHex } })
    await db.accountMember.create({ data: { userId: user.id, accountId: account.id, role: 'owner' } })

    const res = await app.inject({
      method: 'POST',
      url: '/v1/id/token',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { accountId: account.id },
    })
    expect(res.statusCode).toBe(200)
    const body = res.json() as { token: string; accountId: string; expiresAt: number }
    expect(body.accountId).toBe(account.id)
    expect(body.expiresAt).toBeGreaterThan(Date.now())

    // The issued bearer actually authenticates the existing /v1/runs surface.
    const runs = await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: { authorization: `Bearer ${body.token}` },
    })
    expect(runs.statusCode).toBe(200)
    expect(runs.json()).toEqual([])
  })

  it('returns 403 for an account the user is not a member of', async () => {
    const app = createApp()
    const account = await db.account.create({ data: { publicKey: 'pk_foreign' } })
    const { token, csrf } = await createSession(app, 'nope@example.com')

    const res = await app.inject({
      method: 'POST',
      url: '/v1/id/token',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
      payload: { accountId: account.id },
    })
    expect(res.statusCode).toBe(403)
  })
})

describe('/v1/id/sessions', () => {
  it('lists sessions and marks the current one', async () => {
    const app = createApp()
    const { token, csrf, sessionId } = await createSession(app, 'sessions@example.com', {
      ua: 'test-ua',
    })
    // A second session for the same user.
    const user = await db.user.findUnique({ where: { email: 'sessions@example.com' } })
    const otherToken = issueSessionToken()
    const otherSession = await db.webSession.create({
      data: {
        id: sessionIdOf(SECRET, otherToken),
        userId: user!.id,
        expiresAt: new Date(Date.now() + SESSION_TTL_MS),
        ua: 'other-ua',
      },
    })

    const res = await app.inject({
      method: 'GET',
      url: '/v1/id/sessions',
      cookies: { aw_session: token },
    })
    expect(res.statusCode).toBe(200)
    const list = (res.json() as { sessions: Array<{ id: string; ua: string | null; current: boolean }> }).sessions
    expect(list).toHaveLength(2)
    expect(list.find((s) => s.id === sessionId)).toMatchObject({ current: true, ua: 'test-ua' })
    expect(list.find((s) => s.id === otherSession.id)).toMatchObject({ current: false, ua: 'other-ua' })

    // Revoke the other session.
    const del = await app.inject({
      method: 'DELETE',
      url: `/v1/id/sessions/${otherSession.id}`,
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    expect(del.statusCode).toBe(204)

    const stale = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: otherToken },
    })
    expect(stale.statusCode).toBe(401)
  })

  it('revoking the current session logs out the caller', async () => {
    const app = createApp()
    const { token, csrf, sessionId } = await createSession(app, 'self@example.com')

    const del = await app.inject({
      method: 'DELETE',
      url: `/v1/id/sessions/${sessionId}`,
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    expect(del.statusCode).toBe(204)

    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(me.statusCode).toBe(401)
  })
})

describe('/v1/id/identities', () => {
  it('lists identities and forbids deleting the last one', async () => {
    const app = createApp()
    const { token, csrf, user } = await createSession(app, 'identities@example.com')
    const github = await db.identity.create({
      data: { userId: user.id, provider: 'github', subject: '123' },
    })
    const email = await db.identity.create({
      data: { userId: user.id, provider: 'email', subject: 'identities@example.com' },
    })

    const list = await app.inject({
      method: 'GET',
      url: '/v1/id/identities',
      cookies: { aw_session: token },
    })
    expect(list.statusCode).toBe(200)
    const identities = (list.json() as { identities: Array<{ id: string; provider: string }> }).identities
    expect(identities).toHaveLength(2)
    expect(identities.map((i) => i.provider).sort()).toEqual(['email', 'github'])

    const del = await app.inject({
      method: 'DELETE',
      url: `/v1/id/identities/${github.id}`,
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    expect(del.statusCode).toBe(204)

    const last = await app.inject({
      method: 'DELETE',
      url: `/v1/id/identities/${email.id}`,
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    expect(last.statusCode).toBe(409)

    const remaining = await app.inject({
      method: 'GET',
      url: '/v1/id/identities',
      cookies: { aw_session: token },
    })
    expect((remaining.json() as { identities: unknown[] }).identities).toHaveLength(1)
  })
})
