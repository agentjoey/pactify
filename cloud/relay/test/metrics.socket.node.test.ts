import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { io as ioc, type Socket as ClientSocket } from 'socket.io-client'
import type { Server } from 'socket.io'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { issueToken } from '../src/auth'
import { attachSockets } from '../src/sockets'
import { createMetrics, type MetricsRegistry } from '../src/metrics'

const SECRET = 's3cret'

function connect(port: number, auth: Record<string, unknown>): Promise<ClientSocket> {
  return new Promise((resolve, reject) => {
    const s = ioc(`http://localhost:${port}`, {
      auth,
      transports: ['websocket'],
      reconnection: false,
    })
    s.on('connect', () => resolve(s))
    s.on('connect_error', (e) => reject(e))
  })
}

let db: PrismaClient
let app: Awaited<ReturnType<typeof createServer>>
let io: Server
let port: number
let accountId: string
let metrics: MetricsRegistry
const clients: ClientSocket[] = []

beforeEach(async () => {
  db = await createPgliteDb()
  const acc = await db.account.create({ data: { publicKey: 'pk-' + Math.random() } })
  accountId = acc.id
  await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
  await db.run.create({
    data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
  })
  metrics = createMetrics()
  app = createServer({ db, secret: SECRET, now: () => 1000, metrics })
  await app.listen({ port: 0, host: '127.0.0.1' })
  io = await attachSockets(app.server, {
    db,
    secret: SECRET,
    now: () => 1000,
    rateLimits: { rpcPerMin: 3 },
    metrics,
  })
  port = (app.server.address() as { port: number }).port
})

afterEach(async () => {
  for (const c of clients.splice(0)) c.disconnect()
  await new Promise((r) => io.close(() => r(null)))
  await app.close()
})

describe('socket metrics', () => {
  it('counts connections by role and tracks the gauges', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)
    await new Promise((r) => setTimeout(r, 100))

    expect(metrics.get('socket_connections_total', { role: 'machine' })).toBe(1)
    expect(metrics.get('socket_connections_total', { role: 'client' })).toBe(1)
    expect(metrics.getGauge('connected_sockets')).toBe(2)
    expect(metrics.getGauge('connected_machines')).toBe(1)

    web.disconnect()
    await new Promise((r) => setTimeout(r, 100))
    expect(metrics.getGauge('connected_sockets')).toBe(1)
  })

  it('counts rpc by type and a rate-limit rejection', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const web = await connect(port, { token, role: 'client' })
    clients.push(web)
    for (let i = 0; i < 8; i++) {
      web.emit('rpc', { type: 'send-message', runId: 'r1', text: `m${i}` })
    }
    await new Promise((r) => setTimeout(r, 150))

    expect(metrics.get('rpc_total', { type: 'send-message' })).toBe(8)
    expect(metrics.get('rate_limited_total', { path: 'rpc' })).toBeGreaterThan(0)

    // No secret / account content leaked into the rendered metrics.
    const text = metrics.render()
    expect(text).not.toContain(token)
    expect(text).not.toContain(accountId)
    expect(text).not.toContain(SECRET)
  })

  it('counts ingested events', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)
    machine.emit('ingest', {
      agentKind: 'claude',
      msg: {
        header: {
          v: 1 as const,
          machineId: 'm1',
          runId: 'r1',
          seq: 1,
          ts: 1001,
          state: 'thinking' as const,
          eventKind: 'message' as const,
          pendingApprovals: 0,
          tokensIn: 0,
          tokensOut: 0,
        },
        body: { alg: 'xchacha20poly1305' as const, nonce: 'n', ct: 'c' },
      },
    })
    await new Promise((r) => setTimeout(r, 200))
    expect(metrics.get('ingest_events_total')).toBeGreaterThanOrEqual(1)
  })
})
