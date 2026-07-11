import type { FastifyInstance } from 'fastify'
import cookie from '@fastify/cookie'
import { TokenBucketLimiter } from '../rateLimit.js'
import { csrfTokenFor, originAllowed, verifyCsrf } from './csrf.js'
import { buildGithubHandlers } from './github.js'
import { requestMagicLink, verifyMagicLink } from './magic.js'
import { sessionOf } from './session.js'
import type { IdentityDeps } from './types.js'

// 3 magic-link sends per hour per normalized email.
const MAGIC_CAPACITY_PER_HOUR = 3

/**
 * Register the `/v1/id/*` identity plane routes on `app`. Requires the cookie
 * plugin (registered internally with the prefix). All POST routes except OAuth
 * callbacks require a CSRF double-submit cookie/header pair.
 */
export async function registerIdentityRoutes(app: FastifyInstance, deps: IdentityDeps): Promise<void> {
  const magicLimiter = new TokenBucketLimiter({
    capacity: MAGIC_CAPACITY_PER_HOUR,
    refillPerMs: MAGIC_CAPACITY_PER_HOUR / 3_600_000,
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
    //  2. Session-derived CSRF header on state-mutating POSTs. Exempt: OAuth
    //     endpoints and the magic-link request (login entry points — no
    //     session exists yet). The token is delivered in the /me body
    //     (cross-site JS can never read a relay cookie, so double-submit is
    //     structurally impossible here).
    idApp.addHook('preHandler', async (req, reply) => {
      if (!originAllowed(req, deps.config.webUrl)) {
        return reply.code(403).send({ error: 'origin' })
      }
      if (req.method !== 'POST') return
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
  }, { prefix: '/v1/id' })
}
