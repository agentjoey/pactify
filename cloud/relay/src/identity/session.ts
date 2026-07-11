import { createHmac, randomBytes } from 'node:crypto'
import type { FastifyRequest, FastifyReply } from 'fastify'
import type { PrismaClient, User, WebSession } from '@prisma/client'
import { base64urlnopad } from '@scure/base'

/** Sliding lifetime of a web session (30 days). */
export const SESSION_TTL_MS = 30 * 24 * 60 * 60 * 1000

/** Cookie attributes for the httpOnly session token. */
export const SESSION_COOKIE_OPTS = {
  httpOnly: true,
  secure: true,
  sameSite: 'none' as const,
  path: '/',
  maxAge: SESSION_TTL_MS,
}

/** Issue a fresh opaque session token (never stored directly). */
export function issueSessionToken(): string {
  return base64urlnopad.encode(randomBytes(32))
}

/** Derive the stored session id: HMAC-SHA256 of the opaque cookie token. */
export function sessionIdOf(secret: string, token: string): string {
  return base64urlnopad.encode(createHmac('sha256', secret).update(token).digest())
}

/** Persist a WebSession and return the cookie token plus the stored row. */
export async function createWebSession(
  db: PrismaClient,
  secret: string,
  userId: string,
  now: number,
  ua?: string,
): Promise<{ token: string; session: WebSession }> {
  const token = issueSessionToken()
  const id = sessionIdOf(secret, token)
  const expiresAt = new Date(now + SESSION_TTL_MS)
  const session = await db.webSession.create({
    data: { id, userId, expiresAt, ua },
  })
  return { token, session }
}

/** Resolve a session from the request cookie, sliding the expiry on success. */
export async function sessionOf(
  db: PrismaClient,
  secret: string,
  req: FastifyRequest,
  now: number,
): Promise<{ user: User; session: WebSession } | null> {
  const token = req.cookies?.aw_session
  if (typeof token !== 'string' || token.length === 0) return null
  const id = sessionIdOf(secret, token)
  const row = await db.webSession.findUnique({
    where: { id },
    include: { user: true },
  })
  if (!row) return null
  if (row.expiresAt.getTime() < now) return null
  // Slide the session on every authenticated read.
  const expiresAt = new Date(now + SESSION_TTL_MS)
  const session = await db.webSession.update({
    where: { id },
    data: { expiresAt },
  })
  return { user: row.user, session }
}

/** Set the session cookie on a reply. */
export function setSessionCookie(reply: FastifyReply, token: string): void {
  reply.setCookie('aw_session', token, SESSION_COOKIE_OPTS)
}
