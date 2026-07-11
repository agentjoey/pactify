import { randomBytes } from 'node:crypto'
import type { FastifyInstance } from 'fastify'
import cookie from '@fastify/cookie'
import { base64urlnopad } from '@scure/base'
import { issueToken, timingSafeEqualStr, verifyChallenge } from '../auth.js'
import { TokenBucketLimiter } from '../rateLimit.js'
import { csrfTokenFor, originAllowed, verifyCsrf } from './csrf.js'
import { buildGithubHandlers } from './github.js'
import { requestMagicLink, verifyMagicLink } from './magic.js'
import { sessionOf } from './session.js'
import type { IdentityDeps } from './types.js'

// 3 magic-link sends per hour per normalized email.
const MAGIC_CAPACITY_PER_HOUR = 3

// ACCT A1-2: key-ownership challenge lives for 5 minutes and is single-use.
const CHALLENGE_TTL_MS = 5 * 60 * 1_000
const DEFAULT_AUTH_PER_MIN = 10

/**
 * Register the `/v1/id/*` identity plane routes on `app`. Requires the cookie
 * plugin (registered internally with the prefix). All state-mutating routes
 * (POST/PUT/PATCH/DELETE) except OAuth callbacks and the magic-link request
 * require a CSRF double-submit cookie/header pair.
 */
export async function registerIdentityRoutes(app: FastifyInstance, deps: IdentityDeps): Promise<void> {
  const magicLimiter = new TokenBucketLimiter({
    capacity: MAGIC_CAPACITY_PER_HOUR,
    refillPerMs: MAGIC_CAPACITY_PER_HOUR / 3_600_000,
    now: deps.now,
  })
  const linkLimiter = new TokenBucketLimiter({
    capacity: Math.max(1, deps.authPerMin),
    refillPerMs: Math.max(1, deps.authPerMin) / 60_000,
    now: deps.now,
  })

  await app.register(async (idApp) => {
    await idApp.register(cookie)

    // Two-layer request guard for every /v1/id route:
    //  1. Origin allowlist on EVERYTHING (GET included): the CORS layer
    //     reflects any origin with credentials for the bearer API's sake, so
    //     cross-site credentialed reads/writes must be rejected server-side
    //     here (a foreign page must not read /me with the victim's cookie).
    //     OAuth start/callback and magic verify are top-level navigations
    //     (no Origin or browser-set on GET nav) and are naturally exempted by
    //     the no-Origin pass; POSTs from the web app carry its Origin.
    //  2. Session-derived CSRF header on state-mutating POST/PUT/PATCH/DELETE.
    //     Exempt: OAuth endpoints and the magic-link request (login entry
    //     points — no session exists yet). The token is delivered in the /me
    //     body (cross-site JS can never read a relay cookie, so double-submit
    //     is structurally impossible here).
    idApp.addHook('preHandler', async (req, reply) => {
      if (!originAllowed(req, deps.config.webUrl)) {
        return reply.code(403).send({ error: 'origin' })
      }
      const method = req.method
      if (method !== 'POST' && method !== 'PUT' && method !== 'PATCH' && method !== 'DELETE') return
      const path = req.routeOptions?.url ?? (req.url.split('?')[0] ?? req.url)
      if (path.startsWith('/oauth/') || path.includes('/oauth/')) return
      if (path === '/magic' || path === '/v1/id/magic') return
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) {
        return reply.code(401).send({ error: 'unauthorized' })
      }
      if (!verifyCsrf(req, deps.secret, ses.session.id)) {
        return reply.code(403).send({ error: 'csrf' })
      }
    })

    const oauthStates = new Map<string, { codeVerifier: string; createdAt: number }>()
    const github = buildGithubHandlers(deps, oauthStates)

    idApp.get('/oauth/github/start', async (_req, reply) => {
      return github.start(reply)
    })

    idApp.get('/oauth/github/callback', async (req, reply) => {
      return github.callback(req.query as { code?: string; state?: string }, reply, req.headers['user-agent'])
    })

    idApp.post('/magic', async (req, reply) => {
      const body = req.body as { email?: unknown }
      const email = typeof body.email === 'string' ? body.email.trim().toLowerCase() : ''
      const limited = magicLimiter.take(`magic:${email}`)
      if (!limited.allowed) {
        return reply
          .code(429)
          .header('Retry-After', String(limited.retryAfterSec))
          .send({ error: 'rate limited' })
      }
      const result = await requestMagicLink(deps, body, req.headers['user-agent'])
      if ('error' in result) {
        return reply.code(400).send({ error: result.error })
      }
      return { ok: true }
    })

    idApp.get('/magic/verify', async (req, reply) => {
      return verifyMagicLink(deps, req.query as { token?: unknown }, reply, req.headers['user-agent'])
    })

    idApp.get('/me', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })
      const [identities, members] = await Promise.all([
        deps.db.identity.findMany({ where: { userId: ses.user.id }, select: { provider: true } }),
        deps.db.accountMember.findMany({
          where: { userId: ses.user.id },
          include: { account: { select: { id: true, tier: true } } },
        }),
      ])
      return {
        user: { id: ses.user.id, email: ses.user.email },
        identities: identities.map((i) => i.provider),
        accounts: members.map((m) => ({ accountId: m.account.id, role: m.role, tier: m.account.tier })),
        // Synchronizer CSRF token (see csrf.ts): client holds it in memory and
        // echoes it via `x-aw-csrf` on every POST.
        csrf: csrfTokenFor(deps.secret, ses.session.id),
      }
    })

    idApp.post('/logout', async (req, reply) => {
      const token = req.cookies?.aw_session
      if (typeof token === 'string' && token.length > 0) {
        const { sessionIdOf } = await import('./session.js')
        const id = sessionIdOf(deps.secret, token)
        await deps.db.webSession.deleteMany({ where: { id } })
      }
      reply.clearCookie('aw_session', { path: '/' })
      return { ok: true }
    })

    // ── ACCT A1-2: key-ownership proof linking ─────────────────────────────────

    idApp.post('/link/challenge', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })
      const challenge = base64urlnopad.encode(randomBytes(32))
      const expiresAt = new Date(deps.now() + CHALLENGE_TTL_MS)
      await deps.db.linkChallenge.upsert({
        where: { id: ses.session.id },
        update: { challenge, expiresAt },
        create: { id: ses.session.id, challenge, expiresAt },
      })
      return { challenge }
    })

    idApp.post('/link', async (req, reply) => {
      const limited = linkLimiter.take(req.ip)
      if (!limited.allowed) {
        return reply
          .code(429)
          .header('Retry-After', String(limited.retryAfterSec))
          .send({ error: 'rate limited' })
      }
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })

      const body = req.body as { publicKey?: unknown; challenge?: unknown; signature?: unknown }
      const publicKey = typeof body.publicKey === 'string' ? body.publicKey.trim() : ''
      const challenge = typeof body.challenge === 'string' ? body.challenge : ''
      const signature = typeof body.signature === 'string' ? body.signature.trim() : ''
      if (!publicKey || !challenge || !signature) {
        return reply.code(400).send({ error: 'bad request' })
      }

      const stored = await deps.db.linkChallenge.findUnique({ where: { id: ses.session.id } })
      if (!stored || stored.expiresAt.getTime() < deps.now()) {
        return reply.code(401).send({ error: 'unauthorized' })
      }
      if (!timingSafeEqualStr(stored.challenge, challenge)) {
        return reply.code(401).send({ error: 'unauthorized' })
      }
      // Single-use: consume the challenge before verifying the signature.
      await deps.db.linkChallenge.delete({ where: { id: ses.session.id } })

      if (!verifyChallenge(publicKey, challenge, signature)) {
        return reply.code(401).send({ error: 'unauthorized' })
      }
      const account = await deps.db.account.findUnique({ where: { publicKey } })
      if (!account) return reply.code(404).send({ error: 'not found' })

      await deps.db.accountMember.upsert({
        where: { userId_accountId: { userId: ses.user.id, accountId: account.id } },
        update: {},
        create: { userId: ses.user.id, accountId: account.id, role: 'owner' },
      })
      return { accountId: account.id }
    })

    // ── ACCT A1-2: account creation (new-user path) ────────────────────────────

    idApp.post('/accounts', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })

      const body = req.body as { publicKey?: unknown }
      const publicKey = typeof body.publicKey === 'string' ? body.publicKey.trim() : ''
      if (!publicKey) return reply.code(400).send({ error: 'bad request' })

      const existing = await deps.db.account.findUnique({ where: { publicKey } })
      if (existing) return reply.code(409).send({ error: 'conflict' })

      const account = await deps.db.account.create({
        data: { publicKey, tier: 'free' },
      })
      await deps.db.accountMember.create({
        data: { userId: ses.user.id, accountId: account.id, role: 'owner' },
      })
      return { accountId: account.id }
    })

    // ── ACCT A1-2: WebSession proxies a relay bearer ───────────────────────────

    idApp.post('/token', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })

      const body = req.body as { accountId?: unknown }
      const accountId = typeof body.accountId === 'string' ? body.accountId.trim() : ''
      if (!accountId) return reply.code(400).send({ error: 'bad request' })

      const member = await deps.db.accountMember.findUnique({
        where: { userId_accountId: { userId: ses.user.id, accountId } },
      })
      if (!member) return reply.code(403).send({ error: 'forbidden' })

      const now = deps.now()
      const token = issueToken(deps.secret, accountId, deps.tokenTtlMs, now)
      return { token, accountId, expiresAt: now + deps.tokenTtlMs }
    })

    // ── ACCT A1-2: session & identity management ───────────────────────────────

    idApp.get('/sessions', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })

      const sessions = await deps.db.webSession.findMany({
        where: { userId: ses.user.id },
        orderBy: { createdAt: 'desc' },
      })
      return {
        sessions: sessions.map((s) => ({
          id: s.id.slice(0, 8),
          ua: s.ua ?? null,
          createdAt: s.createdAt.toISOString(),
          current: s.id === ses.session.id,
        })),
      }
    })

    idApp.delete('/sessions/:id', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })

      const { id } = req.params as { id: string }
      const result = await deps.db.webSession.deleteMany({
        where: { id, userId: ses.user.id },
      })
      if (result.count === 0) return reply.code(404).send({ error: 'not found' })
      if (id === ses.session.id) reply.clearCookie('aw_session', { path: '/' })
      return reply.code(204).send()
    })

    idApp.get('/identities', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })

      const identities = await deps.db.identity.findMany({
        where: { userId: ses.user.id },
        select: { id: true, provider: true },
        orderBy: { provider: 'asc' },
      })
      return { identities }
    })

    idApp.delete('/identities/:id', async (req, reply) => {
      const ses = await sessionOf(deps.db, deps.secret, req, deps.now())
      if (!ses) return reply.code(401).send({ error: 'unauthorized' })

      const { id } = req.params as { id: string }
      const remaining = await deps.db.identity.count({ where: { userId: ses.user.id } })
      if (remaining <= 1) return reply.code(409).send({ error: 'sole identity' })

      const result = await deps.db.identity.deleteMany({
        where: { id, userId: ses.user.id },
      })
      if (result.count === 0) return reply.code(404).send({ error: 'not found' })
      return reply.code(204).send()
    })
  }, { prefix: '/v1/id' })
}
