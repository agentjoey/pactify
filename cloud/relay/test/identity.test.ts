import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { issueSessionToken, sessionIdOf } from '../src/identity/session'
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

function createApp(opts: { identity?: IdentityPlaneConfig; fetch?: typeof fetch; now?: () => number } = {}) {
  return createServer({
    db,
    secret: SECRET,
    identity: opts.identity ?? BASE_IDENTITY,
    now: opts.now,
    fetch: opts.fetch,
  })
}

function extractCookies(res: {
  cookies: Array<{ name: string; value: string; httpOnly?: boolean }>
  headers: Record<string, string | string[] | undefined>
}) {
  const map = new Map<string, { value: string; httpOnly?: boolean }>()
  for (const c of res.cookies) map.set(c.name, { value: c.value, httpOnly: c.httpOnly })
  const raw = res.headers['set-cookie']
  const setCookies = Array.isArray(raw) ? raw : raw ? [raw] : []
  for (const line of setCookies) {
    const [nameValue] = line.split(';')
    const [name, value] = nameValue.split('=')
    const httpOnly = line.toLowerCase().includes('httponly')
    map.set(name.trim(), { value: value.trim(), httpOnly })
  }
  return map
}

function githubFetch(
  userId: number,
  emails: Array<{ email: string; primary: boolean; verified: boolean }>,
  accessToken = 'tok123',
): typeof fetch {
  return (input) => {
    const url = input.toString()
    if (url.includes('github.com/login/oauth/access_token')) {
      return Promise.resolve(
        new Response(JSON.stringify({ access_token: accessToken }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
      )
    }
    if (url === 'https://api.github.com/user') {
      return Promise.resolve(
        new Response(JSON.stringify({ id: userId }), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
      )
    }
    if (url === 'https://api.github.com/user/emails') {
      return Promise.resolve(
        new Response(JSON.stringify(emails), {
          status: 200,
          headers: { 'content-type': 'application/json' },
        }),
      )
    }
    return Promise.resolve(new Response(JSON.stringify({}), { status: 200 }))
  }
}

async function githubSession(
  emails: Array<{ email: string; primary: boolean; verified: boolean }>,
  userId = 42,
) {
  const app = createApp({ fetch: githubFetch(userId, emails) })
  const start = await app.inject({ method: 'GET', url: '/v1/id/oauth/github/start' })
  expect(start.statusCode).toBe(302)
  const location = start.headers.location as string
  const state = new URL(location).searchParams.get('state')!
  const code = 'gh_code_123'

  const callback = await app.inject({
    method: 'GET',
    url: `/v1/id/oauth/github/callback?code=${code}&state=${state}`,
  })
  expect(callback.statusCode).toBe(302)
  expect(callback.headers.location).toBe(WEB_URL)
  const cookies = extractCookies(callback)
  const session = cookies.get('aw_session')?.value
  expect(session).toBeDefined()
  expect(cookies.get('aw_session')?.httpOnly).toBe(true)
  // Cross-site: the web can never read a relay cookie, so the CSRF token is
  // delivered in the /me body instead of a cookie.
  const meRes = await app.inject({ method: 'GET', url: '/v1/id/me', cookies: { aw_session: session! } })
  expect(meRes.statusCode).toBe(200)
  const csrf = (meRes.json() as { csrf?: string }).csrf
  expect(csrf).toBeTruthy()
  return { app, session: session!, csrf: csrf! }
}

describe('/v1/id/oauth/github', () => {
  it('start redirects to GitHub with state + PKCE when configured', async () => {
    const app = createApp()
    const res = await app.inject({ method: 'GET', url: '/v1/id/oauth/github/start' })
    expect(res.statusCode).toBe(302)
    const location = res.headers.location as string
    expect(location.startsWith('https://github.com/login/oauth/authorize')).toBe(true)
    const url = new URL(location)
    expect(url.searchParams.get('client_id')).toBe('gh_client')
    expect(url.searchParams.get('response_type')).toBeNull()
    expect(url.searchParams.get('code_challenge_method')).toBe('S256')
    expect(url.searchParams.get('scope')).toBe('user:email')
    expect(url.searchParams.get('state')).toBeTruthy()
    expect(url.searchParams.get('code_challenge')).toBeTruthy()
  })

  it('start returns 503 when GitHub client id is not configured', async () => {
    const app = createApp({ identity: { webUrl: WEB_URL } })
    const res = await app.inject({ method: 'GET', url: '/v1/id/oauth/github/start' })
    expect(res.statusCode).toBe(503)
    expect(res.json()).toEqual({ error: 'oauth not configured' })
  })

  it('callback creates a User + Identity, issues cookies, and /me reflects the session', async () => {
    const { app, session } = await githubSession([
      { email: 'sso@example.com', primary: true, verified: true },
    ])

    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: session },
    })
    expect(me.statusCode).toBe(200)
    expect(me.json()).toMatchObject({
      user: { email: 'sso@example.com' },
      identities: ['github'],
      accounts: [],
    })

    const user = await db.user.findUnique({ where: { email: 'sso@example.com' } })
    expect(user).not.toBeNull()
    const identity = await db.identity.findFirst({ where: { provider: 'github', subject: '42' } })
    expect(identity?.userId).toBe(user?.id)
  })

  it('callback rejects a missing/invalid state', async () => {
    const app = createApp()
    const res = await app.inject({
      method: 'GET',
      url: '/v1/id/oauth/github/callback?code=xyz&state=nope',
    })
    expect(res.statusCode).toBe(302)
    expect(res.headers.location).toBe(`${WEB_URL}/id/login?error=invalid`)
  })

  it('callback rejects an unverified primary email', async () => {
    const app = createApp({
      fetch: githubFetch(7, [{ email: 'unverified@example.com', primary: true, verified: false }], 'tok'),
    })
    const start = await app.inject({ method: 'GET', url: '/v1/id/oauth/github/start' })
    const state = new URL(start.headers.location as string).searchParams.get('state')!

    const res = await app.inject({
      method: 'GET',
      url: `/v1/id/oauth/github/callback?code=c&state=${state}`,
    })
    expect(res.statusCode).toBe(302)
    expect(res.headers.location).toBe(`${WEB_URL}/id/login?error=unverified`)
    expect(await db.user.count()).toBe(0)
  })
})

describe('/v1/id/magic', () => {
  it('request → verify → me creates a user and session', async () => {
    const app = createApp()
    const req = await app.inject({
      method: 'POST',
      url: '/v1/id/magic',
      // The initial request has no session yet, so it cannot carry a CSRF token.
      // The endpoint is CSRF-gated for *authenticated* POSTs; the magic request
      // endpoint itself issues no session until verify.
      payload: { email: 'magic@example.com' },
    })
    expect(req.statusCode).toBe(200)
    expect(req.json()).toEqual({ ok: true })

    // Extract the logged/mailed link from the single MagicLink row.
    const row = await db.magicLink.findFirst()
    expect(row).not.toBeNull()
    expect(row!.email).toBe('magic@example.com')

    const verify = await app.inject({
      method: 'GET',
      url: `/v1/id/magic/verify?token=${issueSessionToken()}`,
    })
    // Wrong token → redirect to login error page (no email leak).
    expect(verify.statusCode).toBe(302)
    expect(verify.headers.location).toBe(`${WEB_URL}/id/login?error=invalid`)

    // Need the real token. Since the token itself is not stored, we can't recover
    // it from the row. Instead, request again and intercept the logged link.
    const req2 = await app.inject({
      method: 'POST',
      url: '/v1/id/magic',
      payload: { email: 'magic@example.com' },
    })
    expect(req2.statusCode).toBe(200)

    // For this test we issue the token ourselves and store its HMAC to bypass
    // the opaque token generation, then verify via the endpoint.
    const token = issueSessionToken()
    await db.magicLink.deleteMany()
    await db.magicLink.create({
      data: { id: sessionIdOf(SECRET, token), email: 'magic@example.com', expiresAt: new Date(Date.now() + 15 * 60 * 1000) },
    })

    const verify2 = await app.inject({
      method: 'GET',
      url: `/v1/id/magic/verify?token=${token}`,
    })
    expect(verify2.statusCode).toBe(302)
    expect(verify2.headers.location).toBe(WEB_URL)
    const cookies = extractCookies(verify2)
    const session = cookies.get('aw_session')?.value
    expect(session).toBeDefined()

    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: session! },
    })
    expect(me.statusCode).toBe(200)
    expect(me.json()).toMatchObject({ user: { email: "magic@example.com" }, identities: ["email"], accounts: [] })
  })

  it('magic link response is always ok=true and never leaks email existence', async () => {
    const app = createApp()
    const first = await app.inject({
      method: 'POST',
      url: '/v1/id/magic',
      payload: { email: 'new@example.com' },
    })
    expect(first.json()).toEqual({ ok: true })

    await db.user.create({ data: { email: 'existing@example.com' } })
    const second = await app.inject({
      method: 'POST',
      url: '/v1/id/magic',
      payload: { email: 'existing@example.com' },
    })
    expect(second.statusCode).toBe(200)
    expect(second.json()).toEqual({ ok: true })
  })

  it('rate-limits magic link sends per email', async () => {
    const app = createApp()
    const results: number[] = []
    for (let i = 0; i < 5; i++) {
      const res = await app.inject({
        method: 'POST',
        url: '/v1/id/magic',
        payload: { email: 'limit@example.com' },
      })
      results.push(res.statusCode)
    }
    expect(results.filter((s) => s === 200).length).toBe(3)
    expect(results.filter((s) => s === 429).length).toBe(2)
  })
})

describe('/v1/id/me and /v1/id/logout', () => {
  it('returns 401 without a valid session', async () => {
    const app = createApp()
    const res = await app.inject({ method: 'GET', url: '/v1/id/me' })
    expect(res.statusCode).toBe(401)
  })

  it('slides the expiry on each authenticated read', async () => {
    let t = 0
    const app = createApp({ now: () => t })
    const user = await db.user.create({ data: { email: 'slide@example.com' } })
    const token = issueSessionToken()
    await db.webSession.create({
      data: { id: sessionIdOf(SECRET, token), userId: user.id, expiresAt: new Date(30 * 24 * 60 * 60 * 1000) },
    })
    t = 1_000_000
    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(me.statusCode).toBe(200)

    const updated = await db.webSession.findUnique({ where: { id: sessionIdOf(SECRET, token) } })
    expect(updated!.expiresAt.getTime()).toBe(t + 30 * 24 * 60 * 60 * 1000)

    // Past the original expiry but within the new slide → still valid.
    t = 2_000_000
    const again = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(again.statusCode).toBe(200)
  })

  it('rejects an expired session', async () => {
    let t = 0
    const app = createApp({ now: () => t })
    const user = await db.user.create({ data: { email: 'expired@example.com' } })
    const token = issueSessionToken()
    await db.webSession.create({
      data: { id: sessionIdOf(SECRET, token), userId: user.id, expiresAt: new Date(30 * 24 * 60 * 60 * 1000 - 1) },
    })

    t = 30 * 24 * 60 * 60 * 1000
    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(me.statusCode).toBe(401)
  })

  it('logout deletes the session and clears cookies', async () => {
    const app = createApp()
    const user = await db.user.create({ data: { email: 'logout@example.com' } })
    const token = issueSessionToken()
    await db.webSession.create({
      data: { id: sessionIdOf(SECRET, token), userId: user.id, expiresAt: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000) },
    })
    const csrf = csrfTokenFor(SECRET, sessionIdOf(SECRET, token))

    const res = await app.inject({
      method: 'POST',
      url: '/v1/id/logout',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrf },
    })
    expect(res.statusCode).toBe(200)
    expect(res.json()).toEqual({ ok: true })
    expect(await db.webSession.count()).toBe(0)
    const cleared = extractCookies(res)
    expect(cleared.get('aw_session')?.value).toBe('')
  })

  it('/me includes account memberships with tier', async () => {
    const app = createApp()
    const user = await db.user.create({ data: { email: 'member@example.com' } })
    const account = await db.account.create({ data: { publicKey: 'pk1', tier: 'personal' } })
    await db.accountMember.create({ data: { userId: user.id, accountId: account.id, role: 'owner' } })

    const token = issueSessionToken()
    await db.webSession.create({
      data: { id: sessionIdOf(SECRET, token), userId: user.id, expiresAt: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000) },
    })
    const me = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(me.json()).toMatchObject({
      accounts: [{ accountId: account.id, role: 'owner', tier: 'personal' }],
    })
  })
})

describe('/v1/id CSRF', () => {
  it('requires the session-derived x-aw-csrf header for POST routes', async () => {
    const app = createApp()
    const user = await db.user.create({ data: { email: 'csrf@example.com' } })
    const token = issueSessionToken()
    await db.webSession.create({
      data: { id: sessionIdOf(SECRET, token), userId: user.id, expiresAt: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000) },
    })

    const noHeader = await app.inject({
      method: 'POST',
      url: '/v1/id/logout',
      cookies: { aw_session: token },
    })
    expect(noHeader.statusCode).toBe(403)

    const mismatch = await app.inject({
      method: 'POST',
      url: '/v1/id/logout',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': 'other' },
    })
    expect(mismatch.statusCode).toBe(403)

    const noSession = await app.inject({
      method: 'POST',
      url: '/v1/id/logout',
      headers: { 'x-aw-csrf': 'whatever' },
    })
    expect(noSession.statusCode).toBe(401)

    const ok = await app.inject({
      method: 'POST',
      url: '/v1/id/logout',
      cookies: { aw_session: token },
      headers: { 'x-aw-csrf': csrfTokenFor(SECRET, sessionIdOf(SECRET, token)) },
    })
    expect(ok.statusCode).toBe(200)
  })
})

describe('/v1/id origin guard', () => {
  it('rejects credentialed requests from foreign origins, allows the web app and no-origin clients', async () => {
    const app = createApp()
    const user = await db.user.create({ data: { email: 'origin@example.com' } })
    const token = issueSessionToken()
    await db.webSession.create({
      data: { id: sessionIdOf(SECRET, token), userId: user.id, expiresAt: new Date(Date.now() + 30 * 24 * 60 * 60 * 1000) },
    })

    const foreign = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
      headers: { origin: 'https://evil.example' },
    })
    expect(foreign.statusCode).toBe(403)

    const fromWeb = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
      headers: { origin: new URL(WEB_URL).origin },
    })
    expect(fromWeb.statusCode).toBe(200)

    const localhost = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
      headers: { origin: 'http://localhost:5173' },
    })
    expect(localhost.statusCode).toBe(200)

    const noOrigin = await app.inject({
      method: 'GET',
      url: '/v1/id/me',
      cookies: { aw_session: token },
    })
    expect(noOrigin.statusCode).toBe(200)
  })
})
