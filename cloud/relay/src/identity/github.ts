import { randomBytes } from 'node:crypto'
import { sha256 } from '@noble/hashes/sha2.js'
import { utf8ToBytes } from '@noble/hashes/utils.js'
import { base64urlnopad } from '@scure/base'
import type { PrismaClient } from '@prisma/client'
import type { FastifyReply } from 'fastify'
import { createWebSession, setSessionCookie, sessionIdOf } from './session.js'
import type { IdentityDeps } from './types.js'

const GITHUB_AUTHORIZE_URL = 'https://github.com/login/oauth/authorize'
const GITHUB_TOKEN_URL = 'https://github.com/login/oauth/access_token'
const GITHUB_USER_URL = 'https://api.github.com/user'
const GITHUB_EMAILS_URL = 'https://api.github.com/user/emails'
const OAUTH_STATE_TTL_MS = 10 * 60 * 1000

interface OAuthState {
  codeVerifier: string
  createdAt: number
}

interface GitHubUser {
  id: number
}

interface GitHubEmail {
  email: string
  primary: boolean
  verified: boolean
}

function sha256Base64url(s: string): string {
  return base64urlnopad.encode(sha256(utf8ToBytes(s)))
}

function issueState(): string {
  return base64urlnopad.encode(randomBytes(32))
}

function issueCodeVerifier(): string {
  return base64urlnopad.encode(randomBytes(32))
}

function cleanupExpiredStates(states: Map<string, OAuthState>, now: number): void {
  const cutoff = now - OAUTH_STATE_TTL_MS
  for (const [key, value] of states) {
    if (value.createdAt < cutoff) states.delete(key)
  }
}

/**
 * Build the GitHub OAuth start handler. State and PKCE verifier are kept in an
 * in-memory map with a short TTL (sufficient for the redirect round-trip).
 */
export function buildGithubHandlers(deps: IdentityDeps, states: Map<string, OAuthState>) {
  const { config, now } = deps

  async function start(reply: FastifyReply): Promise<void> {
    if (!config.githubClientId) {
      return reply.code(503).send({ error: 'oauth not configured' })
    }
    const codeVerifier = issueCodeVerifier()
    const state = issueState()
    cleanupExpiredStates(states, now())
    states.set(state, { codeVerifier, createdAt: now() })
    const challenge = sha256Base64url(codeVerifier)
    const url = new URL(GITHUB_AUTHORIZE_URL)
    url.searchParams.set('client_id', config.githubClientId)
    url.searchParams.set('state', state)
    url.searchParams.set('code_challenge', challenge)
    url.searchParams.set('code_challenge_method', 'S256')
    url.searchParams.set('scope', 'user:email')
    return reply.redirect(url.toString())
  }

  async function callback(
    query: { code?: string; state?: string },
    reply: FastifyReply,
    ua?: string,
  ): Promise<void> {
    if (!config.githubClientId || !config.githubClientSecret) {
      return reply.code(503).send({ error: 'oauth not configured' })
    }
    const { code, state } = query
    if (typeof code !== 'string' || typeof state !== 'string') {
      return reply.redirect(`${config.webUrl}/id/login?error=invalid`)
    }
    const stored = states.get(state)
    states.delete(state)
    if (!stored || stored.createdAt < now() - OAUTH_STATE_TTL_MS) {
      return reply.redirect(`${config.webUrl}/id/login?error=invalid`)
    }

    const tokenJson = await exchangeCode(deps, code, stored.codeVerifier)
    const accessToken = tokenJson.access_token
    if (typeof accessToken !== 'string') {
      return reply.redirect(`${config.webUrl}/id/login?error=invalid`)
    }

    const [userJson, emailsJson] = await Promise.all([
      fetchGitHubUser(deps, accessToken),
      fetchGitHubEmails(deps, accessToken),
    ])
    const subject = String(userJson.id)
    const emails = Array.isArray(emailsJson) ? (emailsJson as GitHubEmail[]) : []
    const primary = emails.find((e) => e.primary && e.verified)
    if (!primary) {
      return reply.redirect(`${config.webUrl}/id/login?error=unverified`)
    }

    const user = await upsertGithubIdentity(deps.db, primary.email, subject)
    const { token } = await createWebSession(deps.db, deps.secret, user.id, now(), ua)
    setSessionCookie(reply, token)
    return reply.redirect(config.webUrl)
  }

  return { start, callback }
}

async function exchangeCode(
  deps: IdentityDeps,
  code: string,
  codeVerifier: string,
): Promise<{ access_token?: unknown }> {
  const res = await deps.fetch(GITHUB_TOKEN_URL, {
    method: 'POST',
    headers: {
      Accept: 'application/json',
      'Content-Type': 'application/json',
    },
    body: JSON.stringify({
      client_id: deps.config.githubClientId,
      client_secret: deps.config.githubClientSecret,
      code,
      code_verifier: codeVerifier,
    }),
  })
  if (!res.ok) throw new Error(`github token exchange failed: ${res.status}`)
  return (await res.json()) as { access_token?: unknown }
}

async function fetchGitHubUser(deps: IdentityDeps, accessToken: string): Promise<GitHubUser> {
  const res = await deps.fetch(GITHUB_USER_URL, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      Accept: 'application/vnd.github+json',
    },
  })
  if (!res.ok) throw new Error(`github user fetch failed: ${res.status}`)
  return (await res.json()) as GitHubUser
}

async function fetchGitHubEmails(
  deps: IdentityDeps,
  accessToken: string,
): Promise<GitHubEmail[]> {
  const res = await deps.fetch(GITHUB_EMAILS_URL, {
    headers: {
      Authorization: `Bearer ${accessToken}`,
      Accept: 'application/vnd.github+json',
    },
  })
  if (!res.ok) throw new Error(`github emails fetch failed: ${res.status}`)
  return (await res.json()) as GitHubEmail[]
}

async function upsertGithubIdentity(db: PrismaClient, email: string, subject: string) {
  // Connect-or-create the User by email, then ensure the GitHub identity exists.
  const existingIdentity = await db.identity.findUnique({
    where: { provider_subject: { provider: 'github', subject } },
    include: { user: true },
  })
  if (existingIdentity) return existingIdentity.user

  let user = await db.user.findUnique({ where: { email } })
  if (!user) {
    user = await db.user.create({ data: { email } })
  }
  await db.identity.create({
    data: { provider: 'github', subject, userId: user.id },
  })
  return user
}

// Re-export for tests that want to assert on the HMAC derivation.
export { sessionIdOf }
