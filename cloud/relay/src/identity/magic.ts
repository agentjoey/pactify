import { randomBytes } from 'node:crypto'
import type { FastifyReply } from 'fastify'
import { base64urlnopad } from '@scure/base'
import { createWebSession, sessionIdOf, setSessionCookie } from './session.js'
import type { IdentityDeps } from './types.js'

/** One-time magic link token lifetime (15 minutes). */
const MAGIC_TTL_MS = 15 * 60 * 1000
const RESEND_API_URL = 'https://api.resend.com/emails'
const DEFAULT_RESEND_FROM = 'login@pactify.io'

function issueMagicToken(): string {
  return base64urlnopad.encode(randomBytes(32))
}

function normalizeEmail(raw: unknown): string | null {
  if (typeof raw !== 'string') return null
  const trimmed = raw.trim().toLowerCase()
  if (trimmed.length === 0 || trimmed.length > 254) return null
  if (!trimmed.includes('@')) return null
  return trimmed
}

/**
 * Request a magic link. Rate-limit key is the normalized email. The response is
 * always `{ ok: true }` so callers cannot enumerate registered emails.
 */
export async function requestMagicLink(
  deps: IdentityDeps,
  body: { email?: unknown },
  baseUrl: string,
  ua?: string,
): Promise<{ ok: true } | { error: string; retryAfterSec?: number }> {
  const email = normalizeEmail(body.email)
  if (!email) return { error: 'bad request' }

  const token = issueMagicToken()
  const id = sessionIdOf(deps.secret, token)
  const expiresAt = new Date(deps.now() + MAGIC_TTL_MS)
  await deps.db.magicLink.create({ data: { id, email, expiresAt } })

  // The verify endpoint lives on the RELAY (the session cookie must be set on
  // this domain) — never on the web app, which has no such route.
  const link = `${baseUrl}/v1/id/magic/verify?token=${token}`
  if (deps.config.resendApiKey) {
    await sendResendEmail(deps, email, link).catch((err) => {
      deps.log.warn('failed to send magic link', { error: (err as Error).message })
    })
  } else {
    deps.log.info('magic link (RESEND unconfigured)', { email, link })
  }
  return { ok: true }
}

async function sendResendEmail(deps: IdentityDeps, to: string, link: string): Promise<void> {
  const from = deps.config.resendFrom ?? DEFAULT_RESEND_FROM
  const res = await deps.fetch(RESEND_API_URL, {
    method: 'POST',
    headers: {
      Authorization: `Bearer ${deps.config.resendApiKey}`,
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      from,
      to,
      subject: 'Sign in to Pactify',
      html: `<p>Click the link below to sign in. It expires in 15 minutes.</p><p><a href="${link}">${link}</a></p>`,
    }),
  })
  if (!res.ok) throw new Error(`resend returned ${res.status}`)
}

/**
 * Verify a magic link token and establish a web session. Invalid/expired tokens
 * redirect to the web login page with an error (the response body itself never
 * leaks whether the email exists).
 */
export async function verifyMagicLink(
  deps: IdentityDeps,
  query: { token?: unknown },
  reply: FastifyReply,
  ua?: string,
): Promise<void> {
  if (typeof query.token !== 'string' || query.token.length === 0) {
    return reply.redirect(`${deps.config.webUrl}/id/login?error=invalid`)
  }
  const id = sessionIdOf(deps.secret, query.token)
  const magic = await deps.db.magicLink.findUnique({ where: { id } })
  if (!magic || magic.expiresAt.getTime() < deps.now()) {
    return reply.redirect(`${deps.config.webUrl}/id/login?error=invalid`)
  }
  await deps.db.magicLink.delete({ where: { id } })

  let user = await deps.db.user.findUnique({ where: { email: magic.email } })
  if (!user) {
    user = await deps.db.user.create({ data: { email: magic.email } })
  }
  // Record the login method as an identity (provider 'email'); idempotent.
  await deps.db.identity.upsert({
    where: { provider_subject: { provider: 'email', subject: magic.email } },
    update: {},
    create: { userId: user.id, provider: 'email', subject: magic.email },
  })
  const { token } = await createWebSession(deps.db, deps.secret, user.id, deps.now(), ua)
  setSessionCookie(reply, token)
  return reply.redirect(deps.config.webUrl)
}

export { normalizeEmail }
