import type { PrismaClient } from '@prisma/client'
import type { FastifyInstance } from 'fastify'
import type { Logger } from '../log.js'

/**
 * Identity-plane configuration. When omitted from {@link ServerOptions}, the
 * `/v1/id/*` routes are still mounted but OAuth endpoints return 503 and the
 * magic-link endpoint works in "log only" mode (no RESEND).
 */
export interface IdentityPlaneConfig {
  /** Web app URL; OAuth/magic callbacks redirect here. */
  webUrl: string
  /** GitHub OAuth client id; absent → GitHub endpoints 503. */
  githubClientId?: string
  /** GitHub OAuth client secret; absent → GitHub endpoints 503. */
  githubClientSecret?: string
  /** Resend API key; absent → magic links are logged instead of sent. */
  resendApiKey?: string
  /** Sender address for Resend magic-link emails. Defaults to login@pactify.io. */
  resendFrom?: string
}

/**
 * Dependencies injected into the identity module. `fetch` is exposed so tests
 * can stub the GitHub IdP without network calls.
 */
export interface IdentityDeps {
  db: PrismaClient
  secret: string
  fetch: typeof fetch
  now: () => number
  log: Logger
  config: IdentityPlaneConfig
  /** Bearer token TTL in ms; must match `/v1/auth`. */
  tokenTtlMs: number
  /** Per-minute cap for the `/v1/id/link` brute-force path; mirrors `/v1/auth`. */
  authPerMin: number
}

/** Function shape registered by {@link registerIdentityRoutes}. */
export type RegisterIdentityRoutes = (app: FastifyInstance, deps: IdentityDeps) => Promise<void>
