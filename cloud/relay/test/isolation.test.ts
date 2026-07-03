import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { io as ioc, type Socket as ClientSocket } from 'socket.io-client'
import type { Server } from 'socket.io'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db.js'
import { createServer } from '../src/server.js'
import { attachSockets } from '../src/sockets.js'
import { issueToken } from '../src/auth.js'
import { listRuns, listPendingApprovals, getRunEventsAfter } from '../src/queries.js'
import { listMachines } from '../src/machines.js'
import { getRun, getRunEvents } from '../src/repo.js'

const SECRET = 's3cret'

/** Seed an Account + one Machine; returns ids. machineId === `${accountId}-m`. */
async function seedAccount(
  db: PrismaClient,
  publicKey: string,
): Promise<{ accountId: string; machineId: string }> {
  const account = await db.account.create({ data: { publicKey } })
  const machineId = `${account.id}-m`
  await db.machine.create({ data: { id: machineId, accountId: account.id, metadataEnc: 'enc' } })
  return { accountId: account.id, machineId }
}

/** Seed a Run owned by `accountId`/`machineId` plus N events (seq 0..n-1). */
async function seedRun(
  db: PrismaClient,
  opts: {
    runId: string
    accountId: string
    machineId: string
    state?: string
    pendingApprovals?: number
    events?: number
  },
): Promise<void> {
  await db.run.create({
    data: {
      id: opts.runId,
      accountId: opts.accountId,
      machineId: opts.machineId,
      agentKind: 'claude',
      state: opts.state ?? 'thinking',
      pendingApprovals: opts.pendingApprovals ?? 0,
    },
  })
  for (let seq = 0; seq < (opts.events ?? 0); seq++) {
    await db.runEvent.create({
      data: {
        runId: opts.runId,
        seq,
        state: 'thinking',
        eventKind: 'message',
        ts: BigInt(1000 + seq),
        bodyEnc: JSON.stringify({
          alg: 'xchacha20poly1305',
          nonce: 'n',
          ct: `${opts.runId}-${seq}`,
        }),
      },
    })
  }
}

describe('two-account isolation: query layer', () => {
  let db: PrismaClient
  let a: { accountId: string; machineId: string }
  let b: { accountId: string; machineId: string }

  beforeEach(async () => {
    db = await createPgliteDb()
    a = await seedAccount(db, 'pk-a')
    b = await seedAccount(db, 'pk-b')
    await seedRun(db, {
      runId: 'a-run',
      accountId: a.accountId,
      machineId: a.machineId,
      pendingApprovals: 1,
      state: 'awaiting-approval',
      events: 3,
    })
    await seedRun(db, {
      runId: 'b-run',
      accountId: b.accountId,
      machineId: b.machineId,
      pendingApprovals: 2,
      state: 'awaiting-approval',
      events: 5,
    })
  })

  it("listRuns never returns the other account's runs", async () => {
    const aRuns = await listRuns(db, a.accountId)
    expect(aRuns.map((r) => r.id)).toEqual(['a-run'])
    const bRuns = await listRuns(db, b.accountId)
    expect(bRuns.map((r) => r.id)).toEqual(['b-run'])
    expect(aRuns.some((r) => r.id === 'b-run')).toBe(false)
    expect(bRuns.some((r) => r.id === 'a-run')).toBe(false)
  })

  it("listPendingApprovals never returns the other account's runs", async () => {
    const aPending = await listPendingApprovals(db, a.accountId)
    expect(aPending.map((r) => r.id)).toEqual(['a-run'])
    const bPending = await listPendingApprovals(db, b.accountId)
    expect(bPending.map((r) => r.id)).toEqual(['b-run'])
  })

  it("listMachines never returns the other account's machines", async () => {
    const aMachines = await listMachines(db, a.accountId)
    expect(aMachines.map((m) => m.machineId)).toEqual([a.machineId])
    const bMachines = await listMachines(db, b.accountId)
    expect(bMachines.map((m) => m.machineId)).toEqual([b.machineId])
  })

  it('getRun for a foreign run reports the true (other) owner — callers must guard', async () => {
    // The repo reader is account-agnostic by signature; isolation is enforced by
    // every caller checking run.accountId === authedAccount. This documents that
    // contract: a B-owned run never reports A as its owner.
    const bRun = await getRun(db, 'b-run')
    expect(bRun?.accountId).toBe(b.accountId)
    expect(bRun?.accountId).not.toBe(a.accountId)
  })

  it('getRunEventsAfter is correct per run and never bleeds across runs', async () => {
    const aEvents = await getRunEventsAfter(db, 'a-run', -1)
    expect(aEvents.map((e) => e.seq)).toEqual([0, 1, 2])
    expect(aEvents.every((e) => e.runId === 'a-run')).toBe(true)
    const bEvents = await getRunEventsAfter(db, 'b-run', -1)
    expect(bEvents.map((e) => e.seq)).toEqual([0, 1, 2, 3, 4])
    expect(bEvents.every((e) => e.runId === 'b-run')).toBe(true)
  })

  it('getRunEvents is scoped to its run only', async () => {
    const aEvents = await getRunEvents(db, 'a-run')
    expect(aEvents.every((e) => e.runId === 'a-run')).toBe(true)
    expect(aEvents).toHaveLength(3)
  })
})

describe('two-account isolation: HTTP surface', () => {
  let db: PrismaClient
  let app: ReturnType<typeof createServer>
  let a: { accountId: string; machineId: string }
  let b: { accountId: string; machineId: string }
  let tokenA: string

  beforeEach(async () => {
    db = await createPgliteDb()
    a = await seedAccount(db, 'pk-a')
    b = await seedAccount(db, 'pk-b')
    await seedRun(db, {
      runId: 'a-run',
      accountId: a.accountId,
      machineId: a.machineId,
      pendingApprovals: 1,
      state: 'awaiting-approval',
      events: 2,
    })
    await seedRun(db, {
      runId: 'b-run',
      accountId: b.accountId,
      machineId: b.machineId,
      pendingApprovals: 1,
      state: 'awaiting-approval',
      events: 4,
    })
    app = createServer({ db, secret: SECRET, now: () => 1000 })
    tokenA = issueToken(SECRET, a.accountId, 60_000, 1000)
  })

  afterEach(async () => {
    await app.close()
  })

  const authA = () => ({ authorization: `Bearer ${tokenA}` })

  it("GET /v1/runs returns only account A's runs", async () => {
    const res = await app.inject({ method: 'GET', url: '/v1/runs', headers: authA() })
    expect(res.statusCode).toBe(200)
    const ids = (res.json() as Array<{ id: string }>).map((r) => r.id)
    expect(ids).toEqual(['a-run'])
  })

  it("GET /v1/inbox returns only account A's pending runs", async () => {
    const res = await app.inject({ method: 'GET', url: '/v1/inbox', headers: authA() })
    expect(res.statusCode).toBe(200)
    const ids = (res.json() as Array<{ id: string }>).map((r) => r.id)
    expect(ids).toEqual(['a-run'])
  })

  it("GET /v1/machines returns only account A's machines", async () => {
    const res = await app.inject({ method: 'GET', url: '/v1/machines', headers: authA() })
    expect(res.statusCode).toBe(200)
    const ids = (res.json() as Array<{ machineId: string }>).map((m) => m.machineId)
    expect(ids).toEqual([a.machineId])
  })

  it("GET /v1/runs/:id/events for B's run, authed as A, is 404 (no event leak)", async () => {
    const res = await app.inject({
      method: 'GET',
      url: '/v1/runs/b-run/events?after_seq=-1',
      headers: authA(),
    })
    expect(res.statusCode).toBe(404)
    expect(JSON.stringify(res.json())).not.toContain('b-run')
  })

  it("GET /v1/runs/:id/events for A's own run returns its events", async () => {
    const res = await app.inject({
      method: 'GET',
      url: '/v1/runs/a-run/events?after_seq=-1',
      headers: authA(),
    })
    expect(res.statusCode).toBe(200)
    const seqs = (res.json() as Array<{ seq: number }>).map((e) => e.seq)
    expect(seqs).toEqual([0, 1])
  })

  it("POST /v1/runs/:id/rename for A's own run sets the title and RunSummary carries it", async () => {
    const res = await app.inject({
      method: 'POST',
      url: '/v1/runs/a-run/rename',
      headers: { ...authA(), 'content-type': 'application/json' },
      payload: { title: 'New Title' },
    })
    expect(res.statusCode).toBe(200)
    expect(res.json()).toEqual({ ok: true })

    const list = await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: authA(),
    })
    const runs = list.json() as Array<{ id: string; title?: string }>
    const run = runs.find((r) => r.id === 'a-run')
    expect(run?.title).toBe('New Title')
  })

  it("POST /v1/runs/:id/rename for B's run, authed as A, is 404 (cross-account rename rejected)", async () => {
    const res = await app.inject({
      method: 'POST',
      url: '/v1/runs/b-run/rename',
      headers: { ...authA(), 'content-type': 'application/json' },
      payload: { title: 'Intrusion' },
    })
    expect(res.statusCode).toBe(404)
  })

  it("DELETE /v1/machines/:id for A's own machine cascades its runs + events and 204s", async () => {
    const res = await app.inject({
      method: 'DELETE',
      url: `/v1/machines/${a.machineId}`,
      headers: authA(),
    })
    expect(res.statusCode).toBe(204)

    // The machine and its run+events are gone; B's machine/run are untouched.
    const machines = await app.inject({ method: 'GET', url: '/v1/machines', headers: authA() })
    expect((machines.json() as Array<{ machineId: string }>).map((m) => m.machineId)).toEqual([])
    const runs = await app.inject({ method: 'GET', url: '/v1/runs', headers: authA() })
    expect((runs.json() as Array<{ id: string }>).map((r) => r.id)).toEqual([])
    expect(await db.run.count({ where: { machineId: a.machineId } })).toBe(0)
    expect(await db.runEvent.count({ where: { runId: 'a-run' } })).toBe(0)
    // B's data is intact.
    expect(await db.machine.count({ where: { id: b.machineId } })).toBe(1)
    expect(await db.run.count({ where: { id: 'b-run' } })).toBe(1)
  })

  it("DELETE /v1/machines/:id for B's machine, authed as A, is 404 (cross-account delete rejected)", async () => {
    const res = await app.inject({
      method: 'DELETE',
      url: `/v1/machines/${b.machineId}`,
      headers: authA(),
    })
    expect(res.statusCode).toBe(404)
    // B's machine, run, and events survive the foreign delete attempt.
    expect(await db.machine.count({ where: { id: b.machineId } })).toBe(1)
    expect(await db.run.count({ where: { id: 'b-run' } })).toBe(1)
    expect(await db.runEvent.count({ where: { runId: 'b-run' } })).toBe(4)
  })

  it('DELETE /v1/machines/:id for an unknown machine is 404', async () => {
    const res = await app.inject({
      method: 'DELETE',
      url: '/v1/machines/does-not-exist',
      headers: authA(),
    })
    expect(res.statusCode).toBe(404)
  })
})

describe('two-account isolation: socket surface', () => {
  let db: PrismaClient
  let app: ReturnType<typeof createServer>
  let io: Server
  let port: number
  let a: { accountId: string; machineId: string }
  let b: { accountId: string; machineId: string }
  const clients: ClientSocket[] = []

  function connect(auth: Record<string, unknown>): Promise<ClientSocket> {
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

  beforeEach(async () => {
    db = await createPgliteDb()
    a = await seedAccount(db, 'pk-a')
    b = await seedAccount(db, 'pk-b')
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

  it("a web client authed as A never receives B's ingest events or run-updated", async () => {
    const tokenA = issueToken(SECRET, a.accountId, 60_000, 1000)
    const tokenB = issueToken(SECRET, b.accountId, 60_000, 1000)
    const webA = await connect({ token: tokenA, role: 'client' })
    const machineB = await connect({ token: tokenB, role: 'machine', machineId: b.machineId })
    clients.push(webA, machineB)

    const leaked: string[] = []
    webA.on('event', (e: { runId: string }) => leaked.push(`event:${e.runId}`))
    webA.on('run-updated', (u: { summary: { id: string } }) =>
      leaked.push(`run-updated:${u.summary.id}`),
    )

    // B's machine ingests a run; only B's room should receive it.
    machineB.emit('ingest', {
      agentKind: 'claude',
      msg: {
        header: {
          v: 1,
          machineId: b.machineId,
          runId: 'b-secret',
          seq: 0,
          ts: 1000,
          state: 'thinking',
          eventKind: 'message',
          pendingApprovals: 0,
          tokensIn: 0,
          tokensOut: 0,
        },
        body: { alg: 'xchacha20poly1305', nonce: 'n', ct: 'secret' },
      },
    })

    // Give the fan-out time to (not) arrive.
    await new Promise((r) => setTimeout(r, 150))
    expect(leaked).toEqual([])
  })

  it("a web client authed as A cannot replay B's run events via subscribe", async () => {
    await seedRun(db, {
      runId: 'b-run',
      accountId: b.accountId,
      machineId: b.machineId,
      events: 4,
    })
    const tokenA = issueToken(SECRET, a.accountId, 60_000, 1000)
    const webA = await connect({ token: tokenA, role: 'client' })
    clients.push(webA)

    const got: Array<{ runId: string; seq: number }> = []
    webA.on('event', (e: { runId: string; seq: number }) => got.push(e))
    let replayEnd = false
    webA.on('replay-end', () => {
      replayEnd = true
    })

    webA.emit('subscribe', { runIds: ['b-run'], afterSeqByRun: { 'b-run': -1 } })

    await new Promise((r) => setTimeout(r, 150))
    // Foreign run is skipped entirely: no events and no replay-end for it.
    expect(got).toEqual([])
    expect(replayEnd).toBe(false)
  })

  it("a web client authed as A receives only A's machine roster, never B's", async () => {
    const tokenA = issueToken(SECRET, a.accountId, 60_000, 1000)
    const tokenB = issueToken(SECRET, b.accountId, 60_000, 1000)
    const webA = await connect({ token: tokenA, role: 'client' })
    const machineB = await connect({ token: tokenB, role: 'machine', machineId: b.machineId })
    clients.push(webA, machineB)

    const rosters: Array<Array<{ machineId: string }>> = []
    webA.on('machines', (p: { machines: Array<{ machineId: string }> }) => rosters.push(p.machines))

    // B registers a machine → broadcasts to acct:B only.
    machineB.emit('register', { host: 'host-b', agentKinds: ['claude'] })
    await new Promise((r) => setTimeout(r, 150))
    // A must not have received B's roster broadcast.
    expect(rosters).toEqual([])
  })

  it("an rpc from A targeting B's run is not routed to B's machine", async () => {
    await seedRun(db, { runId: 'b-run', accountId: b.accountId, machineId: b.machineId })
    const tokenA = issueToken(SECRET, a.accountId, 60_000, 1000)
    const tokenB = issueToken(SECRET, b.accountId, 60_000, 1000)
    const webA = await connect({ token: tokenA, role: 'client' })
    const machineB = await connect({ token: tokenB, role: 'machine', machineId: b.machineId })
    clients.push(webA, machineB)

    let received = false
    machineB.on('rpc', () => {
      received = true
    })

    // A tries to send-message to B's run; guard drops it (run.accountId !== A).
    webA.emit('rpc', { type: 'send-message', runId: 'b-run', text: 'intrusion' })
    await new Promise((r) => setTimeout(r, 150))
    expect(received).toBe(false)
  })
})

describe('scale indexes are applied by init.sql (PGlite)', () => {
  let db: PrismaClient

  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('the new composite/fk indexes exist in the schema', async () => {
    const rows = (await db.$queryRawUnsafe(
      `SELECT indexname FROM pg_indexes WHERE schemaname = 'public'`,
    )) as Array<{ indexname: string }>
    const names = new Set(rows.map((r) => r.indexname))
    expect(names.has('Run_accountId_lastActiveAt_idx')).toBe(true)
    expect(names.has('Run_accountId_state_idx')).toBe(true)
    expect(names.has('Machine_accountId_idx')).toBe(true)
    expect(names.has('RunEvent_runId_idx')).toBe(true)
  })

  it('existing queries still work with the indexes present', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, {
      runId: 'a-run',
      accountId: a.accountId,
      machineId: a.machineId,
      pendingApprovals: 1,
      state: 'awaiting-approval',
      events: 2,
    })
    expect((await listRuns(db, a.accountId)).map((r) => r.id)).toEqual(['a-run'])
    expect((await listPendingApprovals(db, a.accountId)).map((r) => r.id)).toEqual(['a-run'])
    expect((await getRunEventsAfter(db, 'a-run', -1)).map((e) => e.seq)).toEqual([0, 1])
    expect((await listMachines(db, a.accountId)).map((m) => m.machineId)).toEqual([a.machineId])
  })
})
