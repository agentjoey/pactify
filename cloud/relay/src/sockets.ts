import type { Server as HttpServer } from 'node:http'
import { Server } from 'socket.io'
import { Prisma, type PrismaClient } from '@prisma/client'
import { AgentKind, RpcRequest, WireMessage } from '@pactify-apps/wire'
import { verifyToken } from './auth.js'
import { ingestWireMessage, getRun, reconcileMachineRuns } from './repo.js'
import { getRunEventsAfter, toRunSummary } from './queries.js'
import { listMachines } from './machines.js'
import { maybeMakeAdapter } from './redis.js'
import { TokenBucketLimiter } from './rateLimit.js'
import { createLogger, type Logger } from './log.js'
import { createMetrics, type MetricsRegistry } from './metrics.js'
import { deliverRunNotification, notifyTypeForHeader, type WebPushSender } from './push.js'

/** Per-minute socket rate limits, enforced per account. Env-overridable. */
export interface SocketRateLimits {
  /** `rpc` events (spawn / send-message / etc.). Default 60/min/account. */
  rpcPerMin?: number
}

export interface SocketDeps {
  db: PrismaClient
  secret: string
  now?: () => number
  redisUrl?: string
  /** Override the per-account socket rate limits; safe defaults otherwise. */
  rateLimits?: SocketRateLimits
  /** Structured logger; defaults to one reading its level from `RELAY_LOG`. */
  logger?: Logger
  /**
   * In-process metrics registry; share the one from {@link createServer} so the
   * `/metrics` surface reflects socket activity. Defaults to a fresh registry.
   */
  metrics?: MetricsRegistry
  /**
   * Injectable web-push sender. When present, a run transition into a
   * notify-worthy state (needs-you / done / error) fans a THIN, zero-knowledge
   * payload out to the owning account's subscriptions. Omitted → push delivery
   * is off (no-op). Tests pass a mock so no network is touched.
   */
  pushSender?: WebPushSender
}

const DEFAULT_RPC_PER_MIN = 60

/**
 * Resolve the effective per-account rpc limit: explicit `deps.rateLimits` wins,
 * then `RELAY_RATE_RPC_PER_MIN`, then the safe default. Non-positive/NaN values
 * are ignored so a bad override can't disable the guard.
 */
function resolveRpcPerMin(deps: SocketDeps): number {
  const override = deps.rateLimits?.rpcPerMin
  if (override !== undefined && Number.isFinite(override) && override > 0) return override
  const raw = process.env.RELAY_RATE_RPC_PER_MIN
  if (raw !== undefined) {
    const n = Number(raw)
    if (Number.isFinite(n) && n > 0) return n
  }
  return DEFAULT_RPC_PER_MIN
}

export async function attachSockets(httpServer: HttpServer, deps: SocketDeps): Promise<Server> {
  const { db, secret, redisUrl, pushSender } = deps
  const now = deps.now ?? (() => Date.now())
  const log = deps.logger ?? createLogger({ name: 'relay', envVar: 'RELAY_LOG' })
  const metrics = deps.metrics ?? createMetrics()
  // Per-account token bucket for the rpc event.
  const rpcLimiter = TokenBucketLimiter.fromPerMinute(resolveRpcPerMin(deps), now)
  // The web connects cross-origin (vercel.app); socket auth is by token in the
  // handshake, so any origin is allowed.
  const io = new Server(httpServer, { cors: { origin: true } })
  const adapter = await maybeMakeAdapter(redisUrl)
  if (adapter) io.adapter(adapter)

  async function broadcastMachines(accountId: string): Promise<void> {
    io.to(`acct:${accountId}`).emit('machines', { machines: await listMachines(db, accountId) })
  }

  io.use((socket, next) => {
    const auth = socket.handshake.auth as { token?: string; role?: string; machineId?: string }
    const v = verifyToken(secret, auth.token ?? '', now())
    // Auth result only — never the token itself.
    if (!v) {
      log.warn('socket auth failed', { role: auth.role, reason: 'invalid token' })
      return next(new Error('unauthorized'))
    }
    if (auth.role === 'machine' && !auth.machineId) {
      log.warn('socket auth failed', { role: auth.role, reason: 'machineId required' })
      return next(new Error('machineId required'))
    }
    socket.data.accountId = v.accountId
    socket.data.role = auth.role
    socket.data.machineId = auth.machineId
    next()
  })

  io.on('connection', (socket) => {
    const accountId = socket.data.accountId as string
    const role = socket.data.role as string | undefined
    const machineId = socket.data.machineId as string | undefined
    void socket.join(`acct:${accountId}`)
    // Bounded role label only (never the account id / machine id).
    const isMachine = role === 'machine' && !!machineId
    const roleLabel = isMachine ? 'machine' : role === 'client' ? 'client' : 'unknown'
    metrics.inc('socket_connections_total', { role: roleLabel })
    metrics.addGauge('connected_sockets', 1)
    if (isMachine) metrics.addGauge('connected_machines', 1)
    log.info('socket connect', {
      account: accountId,
      ...(role ? { role } : {}),
      ...(machineId ? { machineId } : {}),
    })

    socket.on('disconnect', (reason: string) => {
      metrics.addGauge('connected_sockets', -1)
      if (isMachine) metrics.addGauge('connected_machines', -1)
      log.info('socket disconnect', {
        account: accountId,
        ...(role ? { role } : {}),
        ...(machineId ? { machineId } : {}),
        reason,
      })
    })

    if (role === 'machine' && machineId) {
      void socket.join(`machine:${machineId}`)

      // Reconnect reconciliation — runs exactly ONCE here, on this fresh socket
      // CONNECTION (NOT in the `register` handler below, which re-fires on the
      // ~30s heartbeat). linxd keeps run handles in memory; a restart/crash
      // loses them, but the relay still holds those runs in a non-terminal
      // active state (thinking/blocked/awaiting-approval) → the board shows a
      // forever-spinning "zombie" tile. When the machine reconnects we know
      // nothing is driving them, so flip this account+machine's stale runs to
      // `idle` and broadcast a `run-updated` for each so the board clears the
      // spinner. Account- AND machine-scoped (never another account's/machine's
      // runs); terminal (done/error) and already-idle runs are left untouched.
      // Self-correcting: a genuinely-live run's next ingested event restores its
      // real state, and resume-on-rpc re-drives it on the next message. Putting
      // this in `register` instead would flip a live run idle every heartbeat
      // (flicker), so it MUST stay on connect.
      void (async () => {
        try {
          const reconciled = await reconcileMachineRuns(db, accountId, machineId)
          for (const run of reconciled) {
            io.to(`acct:${accountId}`).emit('run-updated', { summary: toRunSummary(run) })
          }
          if (reconciled.length > 0) {
            metrics.inc('reconciled_runs_total', {}, reconciled.length)
            log.info('reconnect reconcile', {
              account: accountId,
              machineId,
              count: reconciled.length,
            })
          }
        } catch (err) {
          metrics.inc('errors_total', { where: 'reconcile' })
          log.error('reconnect reconcile failed', {
            account: accountId,
            machineId,
            error: err instanceof Error ? err.message : String(err),
          })
        }
      })()

      socket.on(
        'register',
        (info: { host?: string; agentKinds?: string[]; workdirs?: string[] }) => {
          void (async () => {
            try {
              await db.machine.upsert({
                where: { id: machineId },
                create: {
                  id: machineId,
                  accountId,
                  metadataEnc: '',
                  host: info.host ?? null,
                  agentKinds: info.agentKinds ?? [],
                  workdirs: info.workdirs === undefined ? Prisma.JsonNull : info.workdirs,
                  online: true,
                  lastSeenAt: BigInt(now()),
                },
                update: {
                  host: info.host ?? null,
                  agentKinds: info.agentKinds ?? [],
                  workdirs: info.workdirs === undefined ? Prisma.JsonNull : info.workdirs,
                  online: true,
                  lastSeenAt: BigInt(now()),
                },
              })
              await broadcastMachines(accountId)
            } catch (err) {
              // A DB failure here must not become an unhandled rejection — the
              // machine would silently appear stale/offline. Log + count instead.
              metrics.inc('errors_total', { where: 'register' })
              log.warn('register failed', {
                machineId,
                error: err instanceof Error ? err.message : String(err),
              })
            }
          })()
        },
      )

      socket.on('disconnect', () => {
        void (async () => {
          try {
            await db.machine.updateMany({
              where: { id: machineId, accountId },
              data: { online: false },
            })
            await broadcastMachines(accountId)
          } catch (err) {
            metrics.inc('errors_total', { where: 'disconnect' })
            log.warn('machine disconnect handler failed', {
              machineId,
              error: err instanceof Error ? err.message : String(err),
            })
          }
        })()
      })

      socket.on('ingest', (payload: { agentKind: string; msg: unknown }) => {
        void (async () => {
          // Validate at the trust boundary: reject a non-conforming agentKind or
          // WireMessage (wrong version, missing/oversized fields, unknown keys)
          // before it touches the DB or a broadcast. See spec §2.5. A conforming
          // linxd (typed against the same schema) always passes.
          const kind = AgentKind.safeParse(payload?.agentKind)
          const parsed = WireMessage.safeParse(payload?.msg)
          if (!kind.success || !parsed.success) {
            metrics.inc('ingest_rejected_total')
            return
          }
          const msg = parsed.data
          const { header, body } = msg
          // runId/seq only — `body` is opaque ciphertext and is never logged.
          log.debug('ingest', { runId: header.runId, seq: header.seq, machineId })
          try {
            // Transient query reply (dir-list/discovered/file-list) couriered via
            // a one-off requestId in `runId`: forward to the account room for
            // client-side requestId matching, but NEVER persist — else each
            // run-less reply upserts a phantom Run (one per directory click).
            if (header.ephemeral) {
              io.to(`acct:${accountId}`).emit('event', {
                runId: header.runId,
                seq: header.seq,
                body,
              })
              return
            }
            // Broadcast the per-run event SYNCHRONOUSLY, before any await, so the
            // delivery order matches arrival/seq order. Socket.IO dispatches these
            // ingest handlers in arrival (seq) order, but each runs as its own
            // async task; emitting after `await getRun`/`await ingestWireMessage`
            // let a later event whose DB ops settled first jump ahead, scrambling
            // the streamed reply. The emit needs only the cleartext header.seq and
            // the opaque body, so it can fire here; persistence, run-updated and
            // push stay async below. (ServerWsMessage `event` variant shape.)
            io.to(`acct:${accountId}`).emit('event', {
              runId: header.runId,
              seq: header.seq,
              body,
            })
            // Prior cleartext state, captured BEFORE the upsert, so we can fire
            // a push only on a genuine transition into a notify-worthy state
            // (not on every header while the run sits in that state). The emit
            // above does not touch the DB, so this still reflects pre-upsert state.
            const prevState = (await getRun(db, header.runId))?.state ?? null
            await ingestWireMessage(db, accountId, kind.data, msg)
            metrics.inc('ingest_events_total')
            // Board update from the just-upserted header.
            const run = await getRun(db, header.runId)
            if (run) io.to(`acct:${accountId}`).emit('run-updated', { summary: toRunSummary(run) })

            // Web-push delivery: on a transition into a notify-worthy state
            // (needs-you / done / error), fan a THIN, zero-knowledge payload out
            // to THIS account's subscriptions. Cleartext operational fields only
            // — never decrypted agent text. Account-scoped, so account A's run
            // never pushes to account B. Fire-and-forget so a slow/failing push
            // service never stalls ingest.
            if (run && pushSender) {
              // Decide off THIS header (eventKind for approval; the header's own
              // terminal state vs the pre-upsert state for done/error) — never
              // the re-fetched `run.state`, which a concurrent ingest may have
              // already advanced.
              const notifyType = notifyTypeForHeader(prevState, header.eventKind, header.state)
              if (notifyType) {
                void deliverRunNotification(
                  db,
                  pushSender,
                  accountId,
                  {
                    type: notifyType,
                    runId: run.id,
                    machineId: run.machineId,
                    agentKind: run.agentKind,
                  },
                  log,
                )
                  .then(({ sent, pruned }) => {
                    if (sent > 0) metrics.inc('push_sent_total', { type: notifyType }, sent)
                    if (pruned > 0) metrics.inc('push_pruned_total', {}, pruned)
                  })
                  .catch((err: unknown) => {
                    metrics.inc('errors_total', { where: 'push' })
                    log.error('push delivery failed', {
                      type: notifyType,
                      error: err instanceof Error ? err.message : String(err),
                    })
                  })
              }
            }
          } catch (err) {
            metrics.inc('errors_total', { where: 'ingest' })
            log.error('ingest failed', {
              runId: header.runId,
              seq: header.seq,
              error: err instanceof Error ? err.message : String(err),
            })
          }
        })()
      })
    }

    // Resume / catch-up: replay missed events per run, then close the burst.
    socket.on(
      'subscribe',
      (payload: { runIds?: string[]; afterSeqByRun?: Record<string, number> }) => {
        void (async () => {
          const afterSeqByRun = payload.afterSeqByRun ?? {}
          const runIds = payload.runIds ?? Object.keys(afterSeqByRun)
          for (const runId of runIds) {
            const run = await getRun(db, runId)
            // Only replay runs this account owns.
            if (!run || run.accountId !== accountId) continue
            if (!(runId in afterSeqByRun)) continue
            const afterSeq = afterSeqByRun[runId]!
            const events = await getRunEventsAfter(db, runId, afterSeq)
            let lastSeq = afterSeq
            for (const e of events) {
              socket.emit('event', {
                runId,
                seq: e.seq,
                body: JSON.parse(e.bodyEnc) as unknown,
              })
              lastSeq = e.seq
            }
            socket.emit('replay-end', { runId, lastSeq })
          }
        })()
      },
    )

    socket.on('ping', () => socket.emit('pong'))

    socket.on('rpc', (raw: unknown) => {
      // Validate at the trust boundary before anything reads rpc fields — an
      // unvalidated `raw.type` would otherwise be forwarded to a machine and
      // injected into the metrics label set (cardinality attack). See spec §2.5.
      const parsedRpc = RpcRequest.safeParse(raw)
      if (!parsedRpc.success) {
        metrics.inc('rpc_rejected_total')
        socket.emit('rpc-error', { error: 'invalid rpc' })
        return
      }
      const rpc = parsedRpc.data
      // `rpc.type` is a bounded enum (spawn / send-message / …), safe as a label.
      metrics.inc('rpc_total', { type: rpc.type })
      // Per-account rate limit: silently drop the over-limit message (emit a
      // soft 'rate-limited' notice) without killing the connection.
      if (!rpcLimiter.take(accountId).allowed) {
        metrics.inc('rate_limited_total', { path: 'rpc' })
        log.warn('rate limited', { account: accountId, event: 'rpc', type: rpc.type })
        socket.emit('rate-limited', { event: 'rpc' })
        return
      }
      void (async () => {
        const targetRooms: string[] = []
        // Machine-targeted RPCs (spawn/discover/attach) carry a `machineId` and
        // route straight to that machine; run-targeted RPCs route via the run's
        // owning machine. Both stay zero-knowledge — only ids are inspected.
        if ('machineId' in rpc) {
          if (rpc.machineId) {
            targetRooms.push(`machine:${rpc.machineId}`)
          } else {
            // Best-effort fallback: the web may not know the real machine id yet;
            // broadcast to every machine belonging to this account. With the Redis
            // adapter only the connected instance(s) deliver; absent machines are a
            // no-op.
            const machines = await listMachines(db, accountId)
            for (const m of machines) targetRooms.push(`machine:${m.machineId}`)
          }
        } else {
          const run = await getRun(db, rpc.runId)
          if (run && run.accountId === accountId) targetRooms.push(`machine:${run.machineId}`)
        }
        for (const room of targetRooms) {
          io.to(room).emit('rpc', rpc)
        }
      })()
    })
  })

  return io
}
