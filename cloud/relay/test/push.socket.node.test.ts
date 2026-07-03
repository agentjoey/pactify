import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { io as ioc, type Socket as ClientSocket } from 'socket.io-client'
import type { Server } from 'socket.io'
import type { PrismaClient } from '@prisma/client'
import type { WireMessage } from '@pactify/wire'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { issueToken } from '../src/auth'
import { attachSockets } from '../src/sockets'
import { subscribePush, PushSendError, type WebPushSender } from '../src/push'

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

function wireMessage(over: Partial<WireMessage['header']> = {}): WireMessage {
  return {
    header: {
      v: 1,
      machineId: 'm1',
      runId: 'r1',
      seq: 0,
      ts: 1000,
      state: 'thinking',
      eventKind: 'message',
      pendingApprovals: 0,
      tokensIn: 0,
      tokensOut: 0,
      ...over,
    },
    body: { alg: 'xchacha20poly1305', nonce: 'n', ct: 'ciphertext-never-pushed' },
  }
}

/** Mock web-push sender recording every send, optionally failing endpoints. */
function mockSender(failures: Record<string, number> = {}): WebPushSender & {
  calls: { endpoint: string; payload: string }[]
} {
  const calls: { endpoint: string; payload: string }[] = []
  return {
    calls,
    async send(subscription, payload) {
      calls.push({ endpoint: subscription.endpoint, payload })
      const status = failures[subscription.endpoint]
      if (status) throw new PushSendError(status)
    },
  }
}

let db: PrismaClient
let app: Awaited<ReturnType<typeof createServer>>
let io: Server
let port: number
const clients: ClientSocket[] = []

async function seedAccount(publicKey: string): Promise<string> {
  const acc = await db.account.create({ data: { publicKey } })
  return acc.id
}

afterEach(async () => {
  for (const c of clients.splice(0)) c.disconnect()
  await new Promise((r) => io.close(() => r(null)))
  await app.close()
})

async function boot(sender: WebPushSender): Promise<void> {
  app = createServer({ db, secret: SECRET, now: () => 1000 })
  await app.listen({ port: 0, host: '127.0.0.1' })
  io = await attachSockets(app.server, { db, secret: SECRET, now: () => 1000, pushSender: sender })
  port = (app.server.address() as { port: number }).port
}

describe('socket ingest → web-push delivery', () => {
  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('a transition into awaiting-approval pushes a THIN payload to the account subscriptions', async () => {
    const accountId = await seedAccount('pk-a')
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await subscribePush(db, accountId, {
      endpoint: 'https://push.example/a',
      keys: { p256dh: 'p', auth: 'a' },
    })
    const sender = mockSender()
    await boot(sender)

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)

    // First a non-notify header (thinking), then an approval-request event.
    machine.emit('ingest', { agentKind: 'claude', msg: wireMessage({ seq: 0, state: 'thinking' }) })
    machine.emit('ingest', {
      agentKind: 'claude',
      msg: wireMessage({
        seq: 1,
        ts: 2000,
        state: 'awaiting-approval',
        eventKind: 'approval-request',
        pendingApprovals: 1,
      }),
    })
    await new Promise((r) => setTimeout(r, 250))

    // Exactly one push — for the transition, not the thinking header.
    expect(sender.calls).toHaveLength(1)
    expect(sender.calls[0]?.endpoint).toBe('https://push.example/a')
    const parsed = JSON.parse(sender.calls[0]?.payload ?? '{}') as Record<string, unknown>
    expect(parsed).toEqual({ type: 'approval', runId: 'r1', machineId: 'm1', agentKind: 'claude' })
    // Zero-knowledge: the ciphertext envelope never rides the push.
    expect(sender.calls[0]?.payload).not.toContain('ciphertext-never-pushed')
  })

  it('a transition into done pushes exactly one THIN payload; a repeated terminal header does NOT re-push', async () => {
    const accountId = await seedAccount('pk-a')
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await subscribePush(db, accountId, {
      endpoint: 'https://push.example/a',
      keys: { p256dh: 'p', auth: 'a' },
    })
    const sender = mockSender()
    await boot(sender)

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)

    // thinking (no push) → done (push, the transition) → a duplicate done header
    // (no push — the run already sits in `done`, so the dedup must suppress it).
    machine.emit('ingest', { agentKind: 'claude', msg: wireMessage({ seq: 0, state: 'thinking' }) })
    await new Promise((r) => setTimeout(r, 80))
    machine.emit('ingest', {
      agentKind: 'claude',
      msg: wireMessage({ seq: 1, ts: 2000, state: 'done', eventKind: 'run-ended' }),
    })
    await new Promise((r) => setTimeout(r, 120))
    machine.emit('ingest', {
      agentKind: 'claude',
      msg: wireMessage({ seq: 2, ts: 3000, state: 'done', eventKind: 'delta' }),
    })
    await new Promise((r) => setTimeout(r, 150))

    // Exactly one push — for the thinking→done transition, not the repeat.
    expect(sender.calls).toHaveLength(1)
    const parsed = JSON.parse(sender.calls[0]?.payload ?? '{}') as Record<string, unknown>
    expect(parsed).toEqual({ type: 'done', runId: 'r1', machineId: 'm1', agentKind: 'claude' })
    // Zero-knowledge: the ciphertext envelope never rides the push.
    expect(sender.calls[0]?.payload).not.toContain('ciphertext-never-pushed')
  })

  it('a transition into error pushes a THIN error payload to the account subscriptions', async () => {
    const accountId = await seedAccount('pk-a')
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await subscribePush(db, accountId, {
      endpoint: 'https://push.example/a',
      keys: { p256dh: 'p', auth: 'a' },
    })
    const sender = mockSender()
    await boot(sender)

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)

    machine.emit('ingest', { agentKind: 'claude', msg: wireMessage({ seq: 0, state: 'thinking' }) })
    await new Promise((r) => setTimeout(r, 80))
    machine.emit('ingest', {
      agentKind: 'claude',
      msg: wireMessage({ seq: 1, ts: 2000, state: 'error', eventKind: 'run-ended' }),
    })
    await new Promise((r) => setTimeout(r, 150))

    expect(sender.calls).toHaveLength(1)
    const parsed = JSON.parse(sender.calls[0]?.payload ?? '{}') as Record<string, unknown>
    expect(parsed).toEqual({ type: 'error', runId: 'r1', machineId: 'm1', agentKind: 'claude' })
  })

  it('cross-account isolation: account A run transition never pushes to account B subscriptions', async () => {
    const accountA = await seedAccount('pk-a')
    const accountB = await seedAccount('pk-b')
    await db.machine.create({ data: { id: 'm1', accountId: accountA, metadataEnc: 'x' } })
    // Only B has a subscription.
    await subscribePush(db, accountB, {
      endpoint: 'https://push.example/b-only',
      keys: { p256dh: 'p', auth: 'a' },
    })
    const sender = mockSender()
    await boot(sender)

    const token = issueToken(SECRET, accountA, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)

    machine.emit('ingest', {
      agentKind: 'claude',
      msg: wireMessage({ seq: 0, state: 'done' }),
    })
    await new Promise((r) => setTimeout(r, 250))

    // A's run is done, but A has no subs and B's sub is never contacted.
    expect(sender.calls).toHaveLength(0)
  })
})
