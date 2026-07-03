import process from 'node:process'
import { fileURLToPath } from 'node:url'
import type { PrismaClient } from '@prisma/client'
import { createPostgresDb } from './db.js'
import { createServer } from './server.js'
import { attachSockets } from './sockets.js'
import { createLogger } from './log.js'
import { createMetrics } from './metrics.js'
import { startRetention, DEFAULT_RUN_TTL_MS, DEFAULT_SWEEP_INTERVAL_MS } from './retention.js'
import { DEFAULT_MACHINE_TTL_MS } from './machines.js'
import { loadVapidConfig, createWebPushSender } from './push.js'

/** Validated server configuration, derived from the process environment. */
export interface ServerEnv {
  databaseUrl: string
  secret: string
  port: number
  redisUrl?: string
  /**
   * Run retention TTL in ms (`RELAY_RUN_TTL_MS`); `<= 0` disables retention.
   * Optional on the interface so callers can omit it; {@link parseServerEnv}
   * always fills it and {@link startServer} falls back to the 7-day default.
   */
  runTtlMs?: number
  /**
   * Machine retention TTL in ms (`RELAY_MACHINE_TTL_MS`); `<= 0` disables the
   * machine sweep. Optional on the interface; {@link parseServerEnv} fills it
   * and {@link startServer} falls back to the 7-day default.
   */
  machineTtlMs?: number
}

const DEFAULT_PORT = 4310

/**
 * Parse + validate the server environment. Pure and testable: requires
 * `DATABASE_URL` and `RELAY_SECRET`; `PORT` defaults to {@link DEFAULT_PORT}.
 * `RELAY_RUN_TTL_MS` sets the run retention TTL (default 7 days; `<= 0` disables
 * retention). Returns the parsed {@link ServerEnv} or an `{ error }` describing
 * what's missing — never throws.
 */
export function parseServerEnv(env: NodeJS.ProcessEnv): ServerEnv | { error: string } {
  const databaseUrl = env.DATABASE_URL
  if (!databaseUrl) return { error: 'DATABASE_URL is required' }
  const secret = env.RELAY_SECRET
  if (!secret) return { error: 'RELAY_SECRET is required' }
  const port = env.PORT ? Number(env.PORT) : DEFAULT_PORT
  if (!Number.isInteger(port) || port < 0) return { error: `PORT is invalid: ${env.PORT}` }
  let runTtlMs = DEFAULT_RUN_TTL_MS
  if (env.RELAY_RUN_TTL_MS !== undefined) {
    const n = Number(env.RELAY_RUN_TTL_MS)
    if (!Number.isFinite(n))
      return { error: `RELAY_RUN_TTL_MS is invalid: ${env.RELAY_RUN_TTL_MS}` }
    runTtlMs = n
  }
  let machineTtlMs = DEFAULT_MACHINE_TTL_MS
  if (env.RELAY_MACHINE_TTL_MS !== undefined) {
    const n = Number(env.RELAY_MACHINE_TTL_MS)
    if (!Number.isFinite(n))
      return { error: `RELAY_MACHINE_TTL_MS is invalid: ${env.RELAY_MACHINE_TTL_MS}` }
    machineTtlMs = n
  }
  const redisUrl = env.REDIS_URL
  return redisUrl
    ? { databaseUrl, secret, port, redisUrl, runTtlMs, machineTtlMs }
    : { databaseUrl, secret, port, runTtlMs, machineTtlMs }
}

/**
 * Start the relay: build (or reuse an injected) {@link PrismaClient}, mount the
 * Fastify HTTP API + the socket.io layer, and listen on `cfg.port`/`0.0.0.0`.
 *
 * `db` is injectable so tests can pass a PGlite client and listen on port 0
 * with no Neon/network. Returns a bound `url` and a `close()` that tears down
 * the HTTP server (and thus the socket.io server attached to it).
 */
export async function startServer(
  cfg: ServerEnv,
  db?: PrismaClient,
): Promise<{ close(): Promise<void>; url: string }> {
  const client = db ?? createPostgresDb(cfg.databaseUrl)
  const log = createLogger({ name: 'relay', envVar: 'RELAY_LOG' })
  // One registry shared across HTTP + sockets so `/metrics` covers both.
  const metrics = createMetrics()
  const runTtlMs = cfg.runTtlMs ?? DEFAULT_RUN_TTL_MS
  const machineTtlMs = cfg.machineTtlMs ?? DEFAULT_MACHINE_TTL_MS
  // VAPID config from env (RELAY_VAPID_*). Absent → push delivery is off: the
  // sender is omitted and GET /v1/push/vapid 404s, but the relay still boots.
  const vapid = loadVapidConfig(process.env)
  const pushSender = vapid ? createWebPushSender(vapid) : undefined
  const app = createServer({
    db: client,
    secret: cfg.secret,
    logger: log,
    metrics,
    runTtlMs,
    machineTtlMs,
    ...(vapid ? { vapidPublicKey: vapid.publicKey } : {}),
  })
  await app.listen({ port: cfg.port, host: '0.0.0.0' })
  await attachSockets(app.server, {
    db: client,
    secret: cfg.secret,
    redisUrl: cfg.redisUrl,
    logger: log,
    metrics,
    ...(pushSender ? { pushSender } : {}),
  })
  // Periodic retention sweep (off when runTtlMs <= 0). Expires old terminal
  // runs + their events so metadata/ciphertext don't accumulate forever.
  const sweeper = startRetention(client, {
    ttlMs: runTtlMs,
    machineTtlMs,
    intervalMs: DEFAULT_SWEEP_INTERVAL_MS,
    logger: log,
  })
  const addr = app.server.address()
  const boundPort = typeof addr === 'object' && addr ? addr.port : cfg.port
  log.info('server start', {
    port: boundPort,
    redis: cfg.redisUrl ? true : false,
    runTtlMs,
    machineTtlMs,
  })
  return {
    url: `http://0.0.0.0:${boundPort}`,
    async close() {
      sweeper.stop()
      await app.close()
    },
  }
}

/**
 * Process entrypoint: parse the environment, then listen. On a bad environment,
 * print the error + usage to stderr and set a non-zero exit code.
 */
export async function main(): Promise<void> {
  const cfg = parseServerEnv(process.env)
  if ('error' in cfg) {
    process.stderr.write(`linx-relay: ${cfg.error}\n`)
    process.stderr.write(
      'usage: DATABASE_URL=… RELAY_SECRET=… [PORT=4310] [REDIS_URL=…] [RELAY_RUN_TTL_MS=604800000] [RELAY_MACHINE_TTL_MS=604800000] node dist/server-main.js\n',
    )
    process.exitCode = 1
    return
  }
  const server = await startServer(cfg)
  process.stdout.write(`linx-relay listening on ${server.url}\n`)
}

// When run directly (the container CMD `node dist/server-main.js`), start the
// server. Guarded by the entry-point check so importing this module (tests)
// never invokes main().
if (process.argv[1] && process.argv[1] === fileURLToPath(import.meta.url)) {
  void main()
}
