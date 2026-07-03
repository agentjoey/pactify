import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { io as ioc, type Socket as ClientSocket } from 'socket.io-client'
import type { Server } from 'socket.io'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { issueToken } from '../src/auth'
import { attachSockets } from '../src/sockets'

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
function once<T>(s: ClientSocket, ev: string): Promise<T> {
  return new Promise((resolve) => s.once(ev, resolve as (v: unknown) => void))
}

let db: PrismaClient
let app: Awaited<ReturnType<typeof createServer>>
let io: Server
let port: number
let accountId: string
const clients: ClientSocket[] = []

beforeEach(async () => {
  db = await createPgliteDb()
  const acc = await db.account.create({ data: { publicKey: 'pk-' + Math.random() } })
  accountId = acc.id
  await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
  await db.run.create({
    data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
  })
  app = createServer({ db, secret: SECRET, now: () => 1000 })
  await app.listen({ port: 0, host: '127.0.0.1' })
  io = await attachSockets(app.server, {
    db,
    secret: SECRET,
    now: () => 1000,
    rateLimits: { rpcPerMin: 3 },
  })
  port = (app.server.address() as { port: number }).port
})

afterEach(async () => {
  for (const c of clients.splice(0)) c.disconnect()
  await new Promise((r) => io.close(() => r(null)))
  await app.close()
})

describe('socket rpc rate limiting', () => {
  it('drops over-limit rpc messages without killing the connection', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    const received: string[] = []
    machine.on('rpc', (r: { runId?: string; text?: string }) => received.push(r.text ?? ''))

    // fire well over the per-account limit (3/min)
    for (let i = 0; i < 8; i++) {
      web.emit('rpc', { type: 'send-message', runId: 'r1', text: `m${i}` })
    }
    // let the server process the burst
    await new Promise((r) => setTimeout(r, 150))

    // only the first 3 should have been routed to the machine
    expect(received.length).toBeLessThanOrEqual(3)
    expect(received.length).toBeGreaterThan(0)

    // the socket is still alive: ping/pong works
    const pongP = once(web, 'pong')
    web.emit('ping')
    await pongP
    expect(web.connected).toBe(true)
  })
})
