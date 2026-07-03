import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { io as ioc, type Socket as ClientSocket } from 'socket.io-client'
import type { Server } from 'socket.io'
import type { PrismaClient } from '@prisma/client'
import { MachineInfo } from '@pactify/wire'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { issueToken } from '../src/auth'
import { attachSockets } from '../src/sockets'

const SECRET = 's3cret'

function header(machineId: string, runId: string, seq: number) {
  return {
    v: 1 as const,
    machineId,
    runId,
    seq,
    ts: 1000 + seq,
    state: 'thinking' as const,
    eventKind: 'message' as const,
    pendingApprovals: 0,
    tokensIn: 0,
    tokensOut: 0,
  }
}
function wire(machineId: string, runId: string, seq: number) {
  return {
    header: header(machineId, runId, seq),
    body: { alg: 'xchacha20poly1305' as const, nonce: 'n', ct: 'c' },
  }
}
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
  app = createServer({ db, secret: SECRET, now: () => 1000 })
  await app.listen({ port: 0, host: '127.0.0.1' })
  io = await attachSockets(app.server, { db, secret: SECRET, now: () => 1000 })
  port = (app.server.address() as { port: number }).port
})

afterEach(async () => {
  for (const c of clients.splice(0)) c.disconnect()
  await new Promise((r) => io.close(() => r(null)))
  await app.close()
})

describe('relay sockets', () => {
  it('rejects a connection with a bad token', async () => {
    await expect(connect(port, { token: 'bad', role: 'client' })).rejects.toBeTruthy()
  })

  it('machine ingest is persisted and fanned out to web clients; rpc routes back to the machine', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    const evP = once<{ runId: string; seq: number; body: { ct: string } }>(web, 'event')
    const msg = { agentKind: 'claude', msg: wire('m1', 'r1', 0) }
    machine.emit('ingest', msg)
    const got = await evP
    expect(got.runId).toBe('r1')
    expect(got.seq).toBe(0)
    expect(got.body.ct).toBe('c')
    // give the async persist a tick, then assert it stored
    await new Promise((r) => setTimeout(r, 50))
    const run = await db.run.findUnique({ where: { id: 'r1' } })
    expect(run?.machineId).toBe('m1')

    // web sends an rpc for r1 → machine receives it
    const rpcP = once<{ type: string; runId: string }>(machine, 'rpc')
    web.emit('rpc', { type: 'send-message', runId: 'r1', text: 'hi' })
    const rpc = await rpcP
    expect(rpc.type).toBe('send-message')
    expect(rpc.runId).toBe('r1')
  })

  it('routes a run-less list-dirs rpc to the machine by machineId', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    // A list-dirs RPC carries no runId — it routes by machineId like discover.
    const rpcP = once<{ type: string; machineId: string; requestId: string; path?: string }>(
      machine,
      'rpc',
    )
    web.emit('rpc', { type: 'list-dirs', machineId: 'm1', requestId: 'dir-1', path: '/repo' })
    const rpc = await rpcP
    expect(rpc.type).toBe('list-dirs')
    expect(rpc.machineId).toBe('m1')
    expect(rpc.requestId).toBe('dir-1')
    expect(rpc.path).toBe('/repo')
  })

  it('ephemeral reply is fanned out as event but NEVER persisted as a run (no phantom runs)', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    const evP = once<{ runId: string; seq: number; body: { ct: string } }>(web, 'event')
    let gotRunUpdated = false
    web.on('run-updated', () => (gotRunUpdated = true))

    // A run-less reply (dir-list/discovered) couriered via a requestId in runId,
    // flagged ephemeral — it must reach the web for matching but never upsert a Run.
    const h = { ...header('m1', 'req-dir-123', 0), ephemeral: true }
    machine.emit('ingest', {
      agentKind: 'claude',
      msg: { header: h, body: { alg: 'xchacha20poly1305' as const, nonce: 'n', ct: 'c' } },
    })

    const ev = await evP
    expect(ev.runId).toBe('req-dir-123')
    expect(ev.body.ct).toBe('c')
    // give any (incorrect) persist a tick, then assert NO run was created
    await new Promise((r) => setTimeout(r, 60))
    const run = await db.run.findUnique({ where: { id: 'req-dir-123' } })
    expect(run).toBeNull()
    expect(gotRunUpdated).toBe(false)
  })

  it('an ephemeral file-list for an EXISTING run does not touch its row or reorder the board (no run-updated)', async () => {
    // The @-mention file picker replies via an ephemeral header on the run's own
    // id. Unlike the run-less case above, the run already exists — so this guards
    // that a transient @-completion does NOT update the run's state/seq/
    // lastActiveAt (which would re-sort the fleet board to the top) and fires no
    // run-updated.
    const oldActive = new Date('2020-01-01T00:00:00Z')
    await db.run.create({
      data: {
        id: 'r-live',
        accountId,
        machineId: 'm1',
        agentKind: 'claude',
        state: 'idle',
        seq: 5,
        lastActiveAt: oldActive,
      },
    })

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    const evP = once<{ runId: string; seq: number; body: { ct: string } }>(web, 'event')
    let gotRunUpdated = false
    web.on('run-updated', () => (gotRunUpdated = true))

    // A file-list reply: ephemeral header on the run's own id, a higher seq.
    const h = { ...header('m1', 'r-live', 9), state: 'thinking' as const, ephemeral: true }
    machine.emit('ingest', {
      agentKind: 'claude',
      msg: { header: h, body: { alg: 'xchacha20poly1305' as const, nonce: 'n', ct: 'c' } },
    })

    const ev = await evP
    expect(ev.runId).toBe('r-live')
    expect(ev.body.ct).toBe('c')

    await new Promise((r) => setTimeout(r, 60))
    // The run row is byte-for-byte unchanged: no new event, no seq/state bump,
    // and crucially lastActiveAt is untouched so the board ordering is stable.
    const run = await db.run.findUnique({ where: { id: 'r-live' } })
    expect(run?.seq).toBe(5)
    expect(run?.state).toBe('idle')
    expect(run?.lastActiveAt.getTime()).toBe(oldActive.getTime())
    expect(await db.runEvent.count({ where: { runId: 'r-live' } })).toBe(0)
    expect(gotRunUpdated).toBe(false)
  })

  it('ingest fans out BOTH event {runId,seq,body} and run-updated {summary} with updated counters', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    const evP = once<{ runId: string; seq: number; body: { ct: string } }>(web, 'event')
    const updP = once<{ summary: Record<string, unknown> }>(web, 'run-updated')

    const h = header('m1', 'r1', 3)
    h.pendingApprovals = 1
    h.tokensIn = 100
    h.tokensOut = 200
    const msg = {
      agentKind: 'claude',
      msg: { header: h, body: { alg: 'xchacha20poly1305' as const, nonce: 'n', ct: 'c' } },
    }
    machine.emit('ingest', msg)

    const ev = await evP
    expect(ev.runId).toBe('r1')
    expect(ev.seq).toBe(3)
    expect(ev.body.ct).toBe('c')

    const upd = await updP
    expect(upd.summary).toMatchObject({
      id: 'r1',
      machineId: 'm1',
      seq: 3,
      pendingApprovals: 1,
      tokensIn: 100,
      tokensOut: 200,
      state: 'thinking',
    })
  })

  it('broadcasts events in ascending-seq order even when their DB writes settle out of order', async () => {
    // Reproduce the live scramble: Socket.IO dispatches the ingest handlers in
    // arrival (seq) order, but each runs as its own async task. We force the
    // per-event DB write (`runEvent.create`) to settle in DESCENDING seq order
    // (seq 0 slowest, seq 2 fastest). If the relay emits the `event` AFTER
    // awaiting persistence, the broadcast order follows DB-completion order
    // (2,1,0). The fix emits synchronously on arrival, so it stays 0,1,2.
    const delayForSeq = (seq: number) => (2 - seq) * 80 + 20

    // Self-contained server so the only socket.io instance on this http server
    // is the one backed by the reordering db (no clash with the beforeEach io).
    const db2 = await createPgliteDb()
    const acc2 = await db2.account.create({ data: { publicKey: 'pk-reorder-' + Math.random() } })
    await db2.machine.create({ data: { id: 'm1', accountId: acc2.id, metadataEnc: 'x' } })
    const reorderingDb2 = makeProxy(db2)
    const app2 = createServer({ db: db2, secret: SECRET, now: () => 1000 })
    await app2.listen({ port: 0, host: '127.0.0.1' })
    const io2 = await attachSockets(app2.server, {
      db: reorderingDb2,
      secret: SECRET,
      now: () => 1000,
    })
    const port2 = (app2.server.address() as { port: number }).port
    const local: ClientSocket[] = []
    try {
      const token = issueToken(SECRET, acc2.id, 60_000, 1000)
      const machine = await connect(port2, { token, role: 'machine', machineId: 'm1' })
      const web = await connect(port2, { token, role: 'client' })
      local.push(machine, web)

      const seqs: number[] = []
      const allThree = new Promise<void>((resolve) => {
        web.on('event', (e: { runId: string; seq: number }) => {
          if (e.runId !== 'r1') return
          seqs.push(e.seq)
          if (seqs.length === 3) resolve()
        })
      })

      // Emit three events for one run in seq order; their DB writes settle
      // reversed because of the injected descending delays.
      for (const seq of [0, 1, 2]) {
        machine.emit('ingest', { agentKind: 'claude', msg: wire('m1', 'r1', seq) })
      }

      await allThree
      expect(seqs).toEqual([0, 1, 2])

      // Persistence still completes (gapless) regardless of emit timing. The
      // synchronous emit resolves immediately; the writes finish later (the
      // slowest injected delay is for seq 0), so poll until all three land.
      let stored: { seq: number }[] = []
      for (let i = 0; i < 50 && stored.length < 3; i++) {
        await new Promise((r) => setTimeout(r, 20))
        stored = await db2.runEvent.findMany({ where: { runId: 'r1' }, orderBy: { seq: 'asc' } })
      }
      expect(stored.map((e) => e.seq)).toEqual([0, 1, 2])
    } finally {
      for (const c of local) c.disconnect()
      await new Promise((r) => io2.close(() => r(null)))
      await app2.close()
    }

    function makeProxy(target: PrismaClient): PrismaClient {
      return new Proxy(target, {
        get(t, prop, receiver) {
          if (prop === 'runEvent') {
            const real = t.runEvent
            return new Proxy(real, {
              get(t2, p2, r2) {
                if (p2 === 'create') {
                  return async (args: { data: { seq: number } }) => {
                    await new Promise((r) => setTimeout(r, delayForSeq(args.data.seq)))
                    return (t2 as typeof real).create(args as never)
                  }
                }
                const v = Reflect.get(t2, p2, r2) as unknown
                return typeof v === 'function' ? (v as (...a: unknown[]) => unknown).bind(t2) : v
              },
            })
          }
          return Reflect.get(t, prop, receiver)
        },
      }) as PrismaClient
    }
  })

  it('subscribe with afterSeqByRun replays exactly seq>N in order then replay-end; foreign run not replayed', async () => {
    // r1 belongs to our account; events seq 0..3
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })
    for (const seq of [0, 1, 2, 3]) {
      await db.runEvent.create({
        data: {
          runId: 'r1',
          seq,
          state: 'thinking',
          eventKind: 'message',
          ts: BigInt(1000 + seq),
          bodyEnc: JSON.stringify({ alg: 'xchacha20poly1305', nonce: 'n', ct: `c${seq}` }),
        },
      })
    }
    // rX belongs to a different account
    const other = await db.account.create({ data: { publicKey: 'pk-other-' + Math.random() } })
    await db.machine.create({ data: { id: 'm2', accountId: other.id, metadataEnc: 'x' } })
    await db.run.create({
      data: {
        id: 'rX',
        accountId: other.id,
        machineId: 'm2',
        agentKind: 'claude',
        state: 'thinking',
      },
    })
    await db.runEvent.create({
      data: {
        runId: 'rX',
        seq: 9,
        state: 'thinking',
        eventKind: 'message',
        ts: BigInt(2000),
        bodyEnc: JSON.stringify({ alg: 'xchacha20poly1305', nonce: 'n', ct: 'secret' }),
      },
    })

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const web = await connect(port, { token, role: 'client' })
    clients.push(web)

    const events: Array<{ runId: string; seq: number; body: { ct: string } }> = []
    web.on('event', (e: { runId: string; seq: number; body: { ct: string } }) => events.push(e))
    const endP = once<{ runId: string; lastSeq: number }>(web, 'replay-end')

    web.emit('subscribe', { runIds: ['r1', 'rX'], afterSeqByRun: { r1: 1, rX: 0 } })

    const end = await endP
    expect(end).toEqual({ runId: 'r1', lastSeq: 3 })
    // exactly seq 2,3 in order, only r1 (foreign rX not replayed)
    expect(events.map((e) => ({ runId: e.runId, seq: e.seq }))).toEqual([
      { runId: 'r1', seq: 2 },
      { runId: 'r1', seq: 3 },
    ])
    expect(events[0]!.body.ct).toBe('c2')
    expect(events.some((e) => e.runId === 'rX')).toBe(false)
  })

  it('replies to ping with pong', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const web = await connect(port, { token, role: 'client' })
    clients.push(web)
    const pongP = once(web, 'pong')
    web.emit('ping')
    await pongP
  })

  it('machine register upserts the machine and broadcasts machines to the account', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    const machinesP = once<{ machines: unknown[] }>(web, 'machines')
    machine.emit('register', {
      host: 'host1',
      agentKinds: ['claude'],
      workdirs: ['/tmp'],
    })
    const push = await machinesP
    expect(push.machines).toHaveLength(1)
    const info = MachineInfo.parse(push.machines[0])
    expect(info).toMatchObject({
      machineId: 'm1',
      host: 'host1',
      agentKinds: ['claude'],
      workdirs: ['/tmp'],
      online: true,
    })
    expect(typeof info.lastSeenAt).toBe('number')

    // HTTP view agrees.
    const res = await app.inject({
      method: 'GET',
      url: '/v1/machines',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(res.statusCode).toBe(200)
    const parsed = MachineInfo.array().parse(res.json())
    expect(parsed[0]?.online).toBe(true)
    expect(parsed[0]?.host).toBe('host1')
  })

  it('reconnect: a machine connecting with a persisted thinking run flips it to idle and broadcasts run-updated', async () => {
    // linxd lost its in-memory handle on restart; the relay still holds r1 in
    // `thinking` → a forever-spinning zombie tile. The machine reconnecting must
    // reconcile it to idle and tell the board.
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    // Web joins first so it is in the account room when the machine connects and
    // the reconcile broadcast fires.
    const web = await connect(port, { token, role: 'client' })
    clients.push(web)
    const updP = once<{ summary: { id: string; state: string } }>(web, 'run-updated')

    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)

    const upd = await updP
    expect(upd.summary.id).toBe('r1')
    expect(upd.summary.state).toBe('idle')

    const run = await db.run.findUnique({ where: { id: 'r1' } })
    expect(run?.state).toBe('idle')
  })

  it('reconnect: blocked and awaiting-approval are reconciled; done/error and another machine/account are untouched', async () => {
    // Eligible: non-terminal active states on THIS account+machine.
    await db.run.create({
      data: { id: 'r-block', accountId, machineId: 'm1', agentKind: 'claude', state: 'blocked' },
    })
    await db.run.create({
      data: {
        id: 'r-appr',
        accountId,
        machineId: 'm1',
        agentKind: 'claude',
        state: 'awaiting-approval',
      },
    })
    // Terminal — left alone.
    await db.run.create({
      data: { id: 'r-done', accountId, machineId: 'm1', agentKind: 'claude', state: 'done' },
    })
    await db.run.create({
      data: { id: 'r-err', accountId, machineId: 'm1', agentKind: 'claude', state: 'error' },
    })
    // Same account, DIFFERENT machine — not this machine's connection.
    await db.machine.create({ data: { id: 'm2', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: { id: 'r-m2', accountId, machineId: 'm2', agentKind: 'claude', state: 'thinking' },
    })
    // DIFFERENT account — never touched.
    const other = await db.account.create({ data: { publicKey: 'pk-other-' + Math.random() } })
    await db.machine.create({ data: { id: 'mX', accountId: other.id, metadataEnc: 'x' } })
    await db.run.create({
      data: {
        id: 'r-other',
        accountId: other.id,
        machineId: 'mX',
        agentKind: 'claude',
        state: 'thinking',
      },
    })

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)
    // Let the connect-time reconcile settle.
    await new Promise((r) => setTimeout(r, 80))

    expect((await db.run.findUnique({ where: { id: 'r-block' } }))?.state).toBe('idle')
    expect((await db.run.findUnique({ where: { id: 'r-appr' } }))?.state).toBe('idle')
    expect((await db.run.findUnique({ where: { id: 'r-done' } }))?.state).toBe('done')
    expect((await db.run.findUnique({ where: { id: 'r-err' } }))?.state).toBe('error')
    // Another machine on the same account is NOT this connection → untouched.
    expect((await db.run.findUnique({ where: { id: 'r-m2' } }))?.state).toBe('thinking')
    // Cross-account → untouched.
    expect((await db.run.findUnique({ where: { id: 'r-other' } }))?.state).toBe('thinking')
  })

  it('reconnect: the register heartbeat does NOT re-reconcile (reconcile runs once per connection)', async () => {
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)
    // Wait for the once-per-connection reconcile.
    await new Promise((r) => setTimeout(r, 80))
    expect((await db.run.findUnique({ where: { id: 'r1' } }))?.state).toBe('idle')

    // Simulate a genuinely-live run advancing back to thinking on the SAME
    // connection (as a real ingest would). The ~30s register heartbeat must NOT
    // flip it back to idle — else a live run flickers every heartbeat.
    await db.run.update({ where: { id: 'r1' }, data: { state: 'thinking' } })
    for (let i = 0; i < 3; i++) {
      machine.emit('register', { host: 'host1', agentKinds: ['claude'], workdirs: ['/tmp'] })
      // Each register round-trips through the machines broadcast.
      await once(machine, 'machines').catch(() => undefined)
      await new Promise((r) => setTimeout(r, 30))
    }

    // Still thinking: the heartbeat-register reconciled nothing.
    expect((await db.run.findUnique({ where: { id: 'r1' } }))?.state).toBe('thinking')
  })

  it('reconnect: a reconciled run is self-correcting — a later ingested event restores its real state', async () => {
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })

    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    clients.push(machine)
    await new Promise((r) => setTimeout(r, 80))
    expect((await db.run.findUnique({ where: { id: 'r1' } }))?.state).toBe('idle')

    // A real event arrives for the run → normal ingest path updates state.
    machine.emit('ingest', { agentKind: 'claude', msg: wire('m1', 'r1', 7) })
    await new Promise((r) => setTimeout(r, 80))
    const run = await db.run.findUnique({ where: { id: 'r1' } })
    expect(run?.state).toBe('thinking')
    expect(run?.seq).toBe(7)
  })

  it('machine disconnect marks the machine offline and broadcasts machines', async () => {
    const token = issueToken(SECRET, accountId, 60_000, 1000)
    const machine = await connect(port, { token, role: 'machine', machineId: 'm1' })
    const web = await connect(port, { token, role: 'client' })
    clients.push(machine, web)

    const onlineP = once<{ machines: unknown[] }>(web, 'machines')
    machine.emit('register', { host: 'host1', agentKinds: ['claude'] })
    await onlineP

    const offlineP = once<{ machines: unknown[] }>(web, 'machines')
    machine.disconnect()
    const offline = await offlineP
    expect(offline.machines).toHaveLength(1)
    const info = MachineInfo.parse(offline.machines[0])
    expect(info.online).toBe(false)

    const res = await app.inject({
      method: 'GET',
      url: '/v1/machines',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(MachineInfo.array().parse(res.json())[0]?.online).toBe(false)
  })
})
