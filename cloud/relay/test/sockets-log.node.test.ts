import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { io as ioc, type Socket as ClientSocket } from 'socket.io-client'
import type { Server } from 'socket.io'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { issueToken } from '../src/auth'
import { attachSockets } from '../src/sockets'
import { createLogger, type LogRecord } from '../src/log'

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
const records: LogRecord[] = []
const clients: ClientSocket[] = []

beforeEach(async () => {
  records.length = 0
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
    logger: createLogger({ sink: (r) => records.push(r), level: 'debug' }),
  })
  port = (app.server.address() as { port: number }).port
})

afterEach(async () => {
  for (const c of clients.splice(0)) c.disconnect()
  await new Promise((r) => io.close(() => r(null)))
  await app.close()
})

describe('socket logging', () => {
  it('logs a warn when an rpc is rate-limited (and never logs the token)', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    for (let i = 0; i < 8; i++) {
      web.emit('rpc', { type: 'send-message', runId: 'r1', text: `m${i}` })
    }
    await new Promise((r) => setTimeout(r, 150))

    const warn = records.find((r) => r.level === 'warn' && /rate/i.test(r.msg))
    expect(warn).toBeTruthy()
    expect(warn?.fields).toMatchObject({ event: 'rpc' })

    // no token / secret leaked anywhere in the log
    const blob = JSON.stringify(records)
    expect(blob).not.toContain(token)
    expect(blob).not.toContain(SECRET)
  })

  it('logs socket connect at info with account + machine', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)
    await new Promise((r) => setTimeout(r, 100))
    const connectLine = records.find((r) => r.level === 'info' && /connect/i.test(r.msg))
    expect(connectLine).toBeTruthy()
    expect(connectLine?.fields).toMatchObject({ machineId: 'm1' })
  })
})
