import Fastify, { type FastifyInstance, type FastifyRequest } from 'fastify'
import cors from '@fastify/cors'
import type { PrismaClient } from '@prisma/client'
import {
  MachineInfo,
  PairComplete,
  PairInit,
  PairReady,
  PairStatus,
  RenameRequest,
  RunSummary,
} from '@pactify-apps/wire'
import { authenticate, verifyToken } from './auth.js'
import { registerIdentityRoutes } from './identity/index.js'
import type { IdentityPlaneConfig } from './identity/types.js'
import { getRun, deleteRun, deleteMachine, renameRun } from './repo.js'
import {
  subscribePush,
  unsubscribePush,
  deliverPactNotification,
  type PushSubscriptionJson,
  type WebPushSender,
} from './push.js'
import { listRuns, listPendingApprovals, getRunEventsAfter, searchRuns } from './queries.js'
import {
  ingestPactEvent,
  listProjects,
  getProjectEvents,
  getProject,
  PactIngestRequest,
} from './pact.js'
import type { Broadcaster } from './broadcast.js'
import { listMachines } from './machines.js'
import { PairStore } from './pair.js'
import { TokenBucketLimiter } from './rateLimit.js'
import { createLogger, type Logger } from './log.js'
import { createMetrics, METRICS_CONTENT_TYPE, type MetricsRegistry } from './metrics.js'

/** Per-minute HTTP rate limits, enforced per source IP. Env-overridable. */
export interface HttpRateLimits {
  /** `POST /v1/auth` — the brute-force target, kept tight. Default 10/min. */
  authPerMin?: number
  /** Every other `/v1/*` endpoint. Default 120/min. */
  readsPerMin?: number
}

export interface ServerOptions {
  db: PrismaClient
  secret: string
  tokenTtlMs?: number
  now?: () => number
  /** Override the per-IP HTTP rate limits; safe defaults otherwise. */
  rateLimits?: HttpRateLimits
  /** Structured logger; defaults to one reading its level from `RELAY_LOG`. */
  logger?: Logger
  /**
   * In-process metrics registry; shared with {@link attachSockets} so HTTP and
   * socket hot paths feed one `/metrics` surface. Defaults to a fresh registry.
   */
  metrics?: MetricsRegistry
  /**
   * Run TTL in ms for board hygiene: the `/v1/runs` board hides terminal runs
   * stale past this even before a retention sweep. `<= 0`/omitted keeps all runs.
   */
  runTtlMs?: number
  /**
   * Machine TTL in ms for board hygiene: the `/v1/machines` rail hides offline
   * machines stale past this even before a machine sweep. `<= 0`/omitted keeps
   * all machines.
   */
  machineTtlMs?: number
  /**
   * The VAPID public key the web fetches from `GET /v1/push/vapid` to create a
   * PushSubscription. Omitted when push is unconfigured — the endpoint then 404s
   * so the web knows push delivery is off (no redeploy needed on key rotation).
   */
  vapidPublicKey?: string
  /**
   * Late-bound HTTP→socket bridge so POST /v1/pact/ingest can fan a `pact-event`
   * out to the account's board watchers. server-main binds the real `io`; unset
   * (tests) it is a safe no-op.
   */
  broadcaster?: Broadcaster
  /**
   * Web-push sender for U2 "needs-you" pact notifications (awaiting review /
   * changes requested). Omitted when push is unconfigured. Shared with the run
   * push path via {@link createWebPushSender}.
   */
  pushSender?: WebPushSender
  /**
   * Identity-plane (SSO / magic link) configuration. OAuth endpoints 503 when
   * the relevant IdP secrets are absent; magic links fall back to logging.
   */
  identity?: IdentityPlaneConfig
  /**
   * Fetch implementation for the identity module (mainly OAuth IdP calls).
   * Defaults to `globalThis.fetch`; injectable so tests can stub the IdP.
   */
  fetch?: typeof fetch
}

// Pact event types that mean a human is needed on the board — the U2 push trigger.
const PACT_NEEDS_YOU: Record<string, true> = { checkpoint: true, changes_requested: true }

/**
 * Parse + validate a standard browser PushSubscription JSON body. Returns the
 * narrowed subscription, or `null` if `endpoint`/`keys.p256dh`/`keys.auth` are
 * missing or not strings (so the route can 400).
 */
function parsePushSubscription(body: unknown): PushSubscriptionJson | null {
  if (typeof body !== 'object' || body === null) return null
  const b = body as { endpoint?: unknown; keys?: { p256dh?: unknown; auth?: unknown } }
  const endpoint = b.endpoint
  const p256dh = b.keys?.p256dh
  const auth = b.keys?.auth
  if (typeof endpoint !== 'string' || endpoint.length === 0) return null
  if (typeof p256dh !== 'string' || typeof auth !== 'string') return null
  return { endpoint, keys: { p256dh, auth } }
}

const ONE_DAY = 24 * 60 * 60 * 1000
const DEFAULT_AUTH_PER_MIN = 10
const DEFAULT_READS_PER_MIN = 120

/**
 * Resolve the effective per-minute limits, layering (priority order) the
 * explicit `opts.rateLimits`, then `RELAY_RATE_*` env overrides, then the safe
 * defaults. Non-positive/NaN values are ignored so a bad override can't silently
 * disable the guard.
 */
function resolveHttpLimits(opts: ServerOptions): { authPerMin: number; readsPerMin: number } {
  const fromEnv = (name: string): number | undefined => {
    const raw = process.env[name]
    if (raw === undefined) return undefined
    const n = Number(raw)
    return Number.isFinite(n) && n > 0 ? n : undefined
  }
  const pick = (override: number | undefined, env: number | undefined, def: number): number => {
    if (override !== undefined && Number.isFinite(override) && override > 0) return override
    return env ?? def
  }
  return {
    authPerMin: pick(
      opts.rateLimits?.authPerMin,
      fromEnv('RELAY_RATE_AUTH_PER_MIN'),
      DEFAULT_AUTH_PER_MIN,
    ),
    readsPerMin: pick(
      opts.rateLimits?.readsPerMin,
      fromEnv('RELAY_RATE_READS_PER_MIN'),
      DEFAULT_READS_PER_MIN,
    ),
  }
}

export function createServer(opts: ServerOptions): FastifyInstance {
  const { db, secret } = opts
  const ttl = opts.tokenTtlMs ?? ONE_DAY
  const now = opts.now ?? (() => Date.now())
  const log = opts.logger ?? createLogger({ name: 'relay', envVar: 'RELAY_LOG' })
  const metrics = opts.metrics ?? createMetrics()
  const app = Fastify()
  const pairStore = new PairStore(now)

  const limits = resolveHttpLimits(opts)
  // Per-IP token buckets: a strict one for the auth brute-force path, a looser
  // one for the remaining /v1/* endpoints.
  const authLimiter = TokenBucketLimiter.fromPerMinute(limits.authPerMin, now)
  const readsLimiter = TokenBucketLimiter.fromPerMinute(limits.readsPerMin, now)

  // Identity plane: SSO sessions live on `/v1/id/*`. The registration is async
  // because it mounts a cookie plugin; the returned app is usable immediately
  // because Fastify's inject waits for the ready lifecycle.
  const identityConfig = opts.identity ?? { webUrl: 'https://pactify-linx-linx-web.vercel.app' }
  void registerIdentityRoutes(app, {
    db,
    secret,
    fetch: opts.fetch ?? globalThis.fetch,
    now,
    log,
    config: identityConfig,
    tokenTtlMs: ttl,
    authPerMin: limits.authPerMin,
  })

  // The web (any vercel.app preview/prod origin) calls this API cross-origin.
  // The bearer API is auth'd by header (no cookies), so reflecting the request
  // origin is safe. `credentials` is on for the /v1/id session-cookie routes —
  // safe under reflection ONLY because the identity module enforces its own
  // server-side Origin allowlist (identity/csrf.ts originAllowed): a foreign
  // origin's credentialed request is rejected there with 403.
  void app.register(cors, {
    origin: true,
    credentials: true,
    methods: ['GET', 'POST', 'DELETE', 'OPTIONS'],
    allowedHeaders: ['Content-Type', 'Authorization', 'x-aw-csrf'],
  })

  // The bounded, low-cardinality route label for a request: prefer Fastify's
  // matched route template (e.g. `/v1/runs/:id/events`) so dynamic ids never
  // become labels; fall back to the static path for unmatched (404) requests,
  // which is itself bounded for our surface.
  function routeLabel(req: FastifyRequest): string {
    const tmpl = req.routeOptions?.url
    if (tmpl) return tmpl
    return req.url.split('?')[0] ?? req.url
  }

  function accountIdOf(req: FastifyRequest): string | null {
    const auth = req.headers['authorization']
    if (!auth || !auth.startsWith('Bearer ')) return null
    const v = verifyToken(secret, auth.slice('Bearer '.length), now())
    return v?.accountId ?? null
  }

  // Per-IP rate limiting for the /v1/* surface. CORS preflights (OPTIONS) and
  // the /healthz probe are exempt. POST /v1/auth uses the strict bucket; every
  // other /v1/* path uses the looser reads bucket. Over-limit → 429 +
  // Retry-After (seconds), and we stop the request before the handler runs.
  app.addHook('onRequest', async (req, reply) => {
    // Stamp a start time so onResponse can record request duration.
    ;(req as FastifyRequest & { _startNs?: bigint })._startNs = process.hrtime.bigint()
    if (req.method === 'OPTIONS') return
    const url = req.url.split('?')[0] ?? req.url
    if (!url.startsWith('/v1/')) return
    const isAuth = req.method === 'POST' && url === '/v1/auth'
    const limiter = isAuth ? authLimiter : readsLimiter
    const res = limiter.take(req.ip)
    if (!res.allowed) {
      // Aggregate path only (the matched template if known, else the raw /v1/*
      // path), never per-account content.
      metrics.inc('rate_limited_total', { path: routeLabel(req) })
      return reply
        .code(429)
        .header('Retry-After', String(res.retryAfterSec))
        .send({ error: 'rate limited' })
    }
  })

  // Per-request HTTP metrics: a counter keyed by the matched route template +
  // status, and a duration summary. Labels are aggregate-only (route templates
  // collapse dynamic ids like `:id`), so no account content reaches /metrics.
  app.addHook('onResponse', async (req, reply) => {
    metrics.inc('http_requests_total', {
      route: routeLabel(req),
      status: String(reply.statusCode),
    })
    const start = (req as FastifyRequest & { _startNs?: bigint })._startNs
    if (start !== undefined) {
      metrics.observe('http_request_duration_ms', Number(process.hrtime.bigint() - start) / 1e6)
    }
  })

  // Count unhandled handler errors, then defer to Fastify's default (500).
  app.setErrorHandler((err, req, reply) => {
    metrics.inc('errors_total', { where: 'http' })
    const message = err instanceof Error ? err.message : String(err)
    log.error('request failed', { route: routeLabel(req), error: message })
    if (!reply.statusCode || reply.statusCode < 400) reply.code(500)
    return reply.send({ error: 'internal error' })
  })

  app.get('/healthz', async () => ({ ok: true }))

  // Unauthenticated Prometheus scrape surface. Aggregate-only: counters/gauges
  // with bounded labels, never tokens, account ids, or ciphertext.
  app.get('/metrics', async (_req, reply) => {
    metrics.setGauge('connected_sockets', metrics.getGauge('connected_sockets'))
    return reply.header('Content-Type', METRICS_CONTENT_TYPE).send(metrics.render())
  })

  app.post('/v1/auth', async (req, reply) => {
    const body = req.body as { publicKey?: string; challenge?: string; signature?: string }
    if (!body?.publicKey || !body?.challenge || !body?.signature) {
      metrics.inc('auth_attempts_total', { result: 'failure' })
      return reply.code(400).send({ error: 'bad request' })
    }
    const res = await authenticate(
      db,
      secret,
      { publicKey: body.publicKey, challenge: body.challenge, signature: body.signature },
      ttl,
      now(),
    )
    if (!res) {
      // Failure only — never the challenge, signature, or issued token.
      metrics.inc('auth_attempts_total', { result: 'failure' })
      log.warn('auth failed', { ip: req.ip })
      return reply.code(401).send({ error: 'unauthorized' })
    }
    metrics.inc('auth_attempts_total', { result: 'success' })
    log.info('auth success', { account: res.accountId })
    return res
  })

  app.get('/v1/runs', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    return RunSummary.array().parse(await listRuns(db, accountId, { ttlMs: opts.runTtlMs, now }))
  })

  app.get('/v1/inbox', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    return RunSummary.array().parse(await listPendingApprovals(db, accountId))
  })

  // Durable run history: account-scoped search/filter over ALL of an account's
  // runs (including terminal/old ones, beyond the board's retention window).
  // Under the reads bucket (above) + metrics (onResponse). Returns a page of
  // RunSummary plus an optional next-page cursor (lastActiveAt epoch ms).
  app.get('/v1/history', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const query = req.query as {
      q?: string
      state?: string
      agentKind?: string
      machineId?: string
      before?: string
      limit?: string
    }
    const numeric = (raw: string | undefined): number | undefined => {
      if (raw === undefined) return undefined
      const n = Number(raw)
      return Number.isFinite(n) ? n : undefined
    }
    const result = await searchRuns(db, accountId, {
      q: query.q,
      state: query.state,
      agentKind: query.agentKind,
      machineId: query.machineId,
      before: numeric(query.before),
      limit: numeric(query.limit),
    })
    return {
      runs: RunSummary.array().parse(result.runs),
      ...(result.nextCursor !== undefined ? { nextCursor: result.nextCursor } : {}),
    }
  })

  app.get('/v1/machines', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    return MachineInfo.array().parse(
      await listMachines(db, accountId, { ttlMs: opts.machineTtlMs, now }),
    )
  })

  // Account-scoped machine delete: removes the machine and cascades everything
  // it owns (its runs + their events) in one transaction. 404 for a missing OR
  // foreign-account machine (uniform, like DELETE /v1/runs/:id), so a caller
  // can't probe which machine ids exist on other accounts. The cascade is
  // FK-safe (RunEvent -> Run -> Machine, the documented ghost-cleanup order) and
  // proceeds regardless of the machine's online/presence state or live runs —
  // it's a deliberate owner action, like the run delete. Bumps
  // machines_deleted_total on success.
  app.delete('/v1/machines/:id', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const { id } = req.params as { id: string }
    const deleted = await deleteMachine(db, accountId, id)
    if (!deleted) return reply.code(404).send({ error: 'not found' })
    metrics.inc('machines_deleted_total')
    return reply.code(204).send()
  })

  app.post('/v1/pair/init', async (req, reply) => {
    const body = PairInit.safeParse(req.body)
    if (!body.success) return reply.code(400).send({ error: 'bad request' })
    const code = pairStore.init(body.data.mode, body.data.epkWeb)
    return { code }
  })

  app.get('/v1/pair/:code', async (req, reply) => {
    const { code } = req.params as { code: string }
    const got = pairStore.get(code)
    if (!got) return reply.code(404).send({ error: 'not found' })
    return got
  })

  // Provision-only, UNAUTHENTICATED: the secret-less machine publishes its
  // ephemeral public key so the (authenticated) web can wrap the secret to it.
  // Only a public key crosses the wire here — never plaintext.
  app.post('/v1/pair/:code/ready', async (req, reply) => {
    const { code } = req.params as { code: string }
    const body = PairReady.safeParse(req.body)
    if (!body.success) return reply.code(400).send({ error: 'bad request' })

    const state = pairStore.exists(code)
    if (state === 'unknown') return reply.code(404).send({ error: 'not found' })
    if (state === 'expired') return reply.code(410).send({ error: 'gone' })

    const ok = pairStore.ready(code, body.data.epkMachine)
    if (!ok) return reply.code(410).send({ error: 'gone' })
    return { ok: true }
  })

  app.get('/v1/pair/:code/status', async (req, reply) => {
    const { code } = req.params as { code: string }
    const status = pairStore.status(code)
    if (!status) return reply.code(404).send({ error: 'not found' })
    return PairStatus.parse(status)
  })

  app.post('/v1/pair/:code/complete', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const { code } = req.params as { code: string }
    const body = PairComplete.safeParse(req.body)
    if (!body.success) return reply.code(400).send({ error: 'bad request' })

    const state = pairStore.exists(code)
    if (state === 'unknown') return reply.code(404).send({ error: 'not found' })
    if (state === 'expired' || state === 'completed') return reply.code(410).send({ error: 'gone' })

    const ok = pairStore.complete(code, body.data)
    if (!ok) return reply.code(410).send({ error: 'gone' })
    return { ok: true }
  })

  app.get('/v1/runs/:id/events', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const { id } = req.params as { id: string }
    const run = await getRun(db, id)
    if (!run || run.accountId !== accountId) return reply.code(404).send({ error: 'not found' })
    const afterSeq = Number((req.query as { after_seq?: string }).after_seq ?? '0')
    const events = await getRunEventsAfter(db, id, afterSeq)
    // RunEvent.ts is BigInt — serialize to number for JSON.
    return events.map((e) => ({ ...e, ts: Number(e.ts) }))
  })

  // ── U2 Mission Control: pact events ────────────────────────────────────────
  // pactify machines POST their `.pact` log events here (cleartext operational
  // header + E2E-encrypted body). Persist, then fan a `pact-event` out to the
  // account's board watchers over sockets. HTTP (not socket) because pactify's
  // uploader is Go, where socket.io support is poor.
  app.post('/v1/pact/ingest', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const parsed = PactIngestRequest.safeParse(req.body)
    if (!parsed.success) {
      metrics.inc('pact_rejected_total')
      return reply.code(400).send({ error: 'bad request' })
    }
    const existing = await getProject(db, parsed.data.projectId)
    if (existing && existing.accountId !== accountId) {
      return reply.code(404).send({ error: 'not found' })
    }
    const { created } = await ingestPactEvent(db, accountId, parsed.data)
    metrics.inc('pact_events_total')
    // A re-uploaded event (a machine reconnecting replays its whole ledger) is a
    // storage no-op AND must not re-broadcast or re-push — only genuinely new
    // events drive board updates and notifications.
    if (!created) return reply.code(202).send({ ok: true })
    opts.broadcaster?.emit(accountId, 'pact-event', {
      projectId: parsed.data.projectId,
      seq: parsed.data.seq,
      eventType: parsed.data.eventType,
      task: parsed.data.task ?? null,
      feature: parsed.data.feature ?? null,
      ts: parsed.data.ts,
      bodyEnc: parsed.data.bodyEnc,
    })
    // "Needs you" push (S6): a task awaiting review / changes requested needs a
    // human. Fire-and-forget (never blocks the 202); zero-knowledge payload.
    if (opts.pushSender && PACT_NEEDS_YOU[parsed.data.eventType]) {
      void deliverPactNotification(db, opts.pushSender, accountId, {
        type: 'pact-needs-you',
        projectId: parsed.data.projectId,
        eventType: parsed.data.eventType,
        ...(parsed.data.task ? { task: parsed.data.task } : {}),
      }).catch(() => {})
    }
    return reply.code(202).send({ ok: true })
  })

  app.get('/v1/pact/projects', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const projects = await listProjects(db, accountId)
    return projects.map((p) => ({ ...p, lastEventAt: p.lastEventAt.getTime() }))
  })

  app.get('/v1/pact/projects/:id/events', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const { id } = req.params as { id: string }
    // 404 uniform for missing OR foreign-account projects, so a caller can't
    // probe which project ids exist on other accounts.
    const project = await getProject(db, id)
    if (!project || project.accountId !== accountId) {
      return reply.code(404).send({ error: 'not found' })
    }
    // `after_seq` is a catch-up cursor (events strictly after it). Absent → the
    // full stream from seq 0; do NOT default to 0, which would drop seq 0.
    const rawAfter = (req.query as { after_seq?: string }).after_seq
    const opts = rawAfter !== undefined ? { afterSeq: Number(rawAfter) } : {}
    const events = await getProjectEvents(db, id, opts)
    return events.map((e) => ({ ...e, ts: Number(e.ts) }))
  })

  // Account-scoped run delete: removes the run + its events in one transaction.
  // 404 for a missing OR foreign-account run (uniform, like /v1/runs/:id/events),
  // so a caller can't probe which run ids exist on other accounts. Under the
  // reads rate-limit bucket (onRequest) + metrics (onResponse); also bumps
  // runs_deleted_total on a successful delete.
  app.delete('/v1/runs/:id', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const { id } = req.params as { id: string }
    const deleted = await deleteRun(db, accountId, id)
    if (!deleted) return reply.code(404).send({ error: 'not found' })
    metrics.inc('runs_deleted_total')
    return reply.code(204).send()
  })

  // Account-scoped run rename: persists the operational `title` on the Run row.
  // 404 uniform for missing/foreign-account runs (same as /v1/runs/:id/events
  // and DELETE). The `RenameRequest` wire schema enforces 1..200 char titles.
  app.post('/v1/runs/:id/rename', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const { id } = req.params as { id: string }
    const body = RenameRequest.safeParse({ type: 'rename', ...(req.body as object), runId: id })
    if (!body.success) return reply.code(400).send({ error: 'bad request' })
    const renamed = await renameRun(db, accountId, id, body.data.title)
    if (!renamed) return reply.code(404).send({ error: 'not found' })
    return { ok: true }
  })

  // The VAPID public key the web needs to create a PushSubscription. Fetched at
  // runtime so a key rotation doesn't require a web redeploy. 404 when push is
  // unconfigured (no key), so the web can tell delivery is off.
  app.get('/v1/push/vapid', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    if (!opts.vapidPublicKey) return reply.code(404).send({ error: 'not found' })
    return { publicKey: opts.vapidPublicKey }
  })

  // Persist a browser PushSubscription for this account, idempotent by endpoint
  // (a re-subscribe with the same endpoint upserts, refreshing the keys). The
  // subscription is push-infra material, not agent content.
  app.post('/v1/push/subscribe', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const sub = parsePushSubscription(req.body)
    if (!sub) return reply.code(400).send({ error: 'bad request' })
    await subscribePush(db, accountId, sub)
    return reply.code(204).send()
  })

  // Remove a subscription by endpoint for this account. 404 for an unknown OR
  // foreign-account endpoint (uniform, like the other account-scoped deletes).
  app.delete('/v1/push/subscribe', async (req, reply) => {
    const accountId = accountIdOf(req)
    if (!accountId) return reply.code(401).send({ error: 'unauthorized' })
    const endpoint = (req.body as { endpoint?: unknown } | undefined)?.endpoint
    if (typeof endpoint !== 'string' || endpoint.length === 0) {
      return reply.code(400).send({ error: 'bad request' })
    }
    const removed = await unsubscribePush(db, accountId, endpoint)
    if (!removed) return reply.code(404).send({ error: 'not found' })
    return reply.code(204).send()
  })

  return app
}
