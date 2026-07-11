import { createHmac } from 'node:crypto'
import type { FastifyRequest } from 'fastify'
import { base64urlnopad } from '@scure/base'
import { timingSafeEqualStr } from '../auth.js'

/**
 * CSRF for a CROSS-SITE cookie API. The web app runs on a different
 * registrable domain than the relay (vercel.app vs fly.dev), so classic
 * double-submit is impossible: the web's JS can never read a relay cookie.
 * Instead the token is a synchronizer derived from the session — delivered in
 * the `/v1/id/me` response BODY, held by the client in memory, and echoed back
 * via the `x-aw-csrf` header on every state-mutating request. Zero storage:
 * the server recomputes HMAC(secret, "csrf:" + sessionId) and compares.
 */
export function csrfTokenFor(secret: string, sessionId: string): string {
  return base64urlnopad.encode(
    createHmac('sha256', secret).update(`csrf:${sessionId}`).digest(),
  )
}

/** Verify the `x-aw-csrf` header against the session-derived token. */
export function verifyCsrf(req: FastifyRequest, secret: string, sessionId: string): boolean {
  const header = req.headers['x-aw-csrf']
  if (typeof header !== 'string' || header.length === 0) return false
  return timingSafeEqualStr(header, csrfTokenFor(secret, sessionId))
}

/**
 * Origin allowlist guard (defense-in-depth, mirrors serve's writeGuard): a
 * browser request carrying the session cookie must originate from the
 * configured web app (or local dev). Requests WITHOUT an Origin header pass —
 * they are non-browser clients (curl, native) that cannot be CSRF'd. This is
 * what makes the permissive credentialed CORS reflection safe: a foreign
 * origin's credentialed fetch reaches the server, but is rejected HERE before
 * touching identity state.
 */
export function originAllowed(req: FastifyRequest, webUrl: string): boolean {
  const origin = req.headers.origin
  if (typeof origin !== 'string' || origin.length === 0) return true
  let allowed: string
  try {
    allowed = new URL(webUrl).origin
  } catch {
    return false
  }
  if (origin === allowed) return true
  return /^https?:\/\/(localhost|127\.0\.0\.1)(:\d+)?$/.test(origin)
}
