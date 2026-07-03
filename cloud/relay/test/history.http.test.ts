import { describe, it, expect, beforeEach } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import type { PrismaClient } from '@prisma/client'
import { RunSummary } from '@pactify/wire'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'

const SECRET = 's3cret'
function keypair() {
  const priv = ed25519.utils.randomSecretKey()
  return { priv, pubHex: bytesToHex(ed25519.getPublicKey(priv)) }
}
function sign(priv: Uint8Array, m: string) {
  return bytesToHex(ed25519.sign(utf8ToBytes(m), priv))
}

let db: PrismaClient
beforeEach(async () => {
  db = await createPgliteDb()
})

async function login(app: ReturnType<typeof createServer>, kp = keypair()) {
  const res = await app.inject({
    method: 'POST',
    url: '/v1/auth',
    payload: { publicKey: kp.pubHex, challenge: 'c1', signature: sign(kp.priv, 'c1') },
  })
  return {
    kp,
    token: res.json().token as string,
    accountId: res.json().accountId as string,
  }
}

async function seedRun(
  accountId: string,
  machineId: string,
  r: { id: string; state?: string; agentKind?: string; branch?: string; lastActiveAt: string },
) {
  await db.run.create({
    data: {
      id: r.id,
      accountId,
      machineId,
      agentKind: r.agentKind ?? 'claude',
      state: r.state ?? 'idle',
      branch: r.branch ?? null,
      lastActiveAt: new Date(r.lastActiveAt),
    },
  })
}

describe('GET /v1/history', () => {
  it('rejects unauthenticated requests', async () => {
    const app = createServer({ db, secret: SECRET })
    const res = await app.inject({ method: 'GET', url: '/v1/history' })
    expect(res.statusCode).toBe(401)
  })

  it("returns the authed account's runs newest-first, account-scoped", async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    const other = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.machine.create({ data: { id: 'm2', accountId: other.accountId, metadataEnc: 'x' } })
    await seedRun(accountId, 'm1', { id: 'a-old', lastActiveAt: '2026-06-01T00:00:00Z' })
    await seedRun(accountId, 'm1', { id: 'a-new', lastActiveAt: '2026-06-10T00:00:00Z' })
    await seedRun(other.accountId, 'm2', { id: 'b1', lastActiveAt: '2026-06-20T00:00:00Z' })

    const res = await app.inject({
      method: 'GET',
      url: '/v1/history',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(res.statusCode).toBe(200)
    const body = res.json() as { runs: unknown[]; nextCursor?: number }
    const runs = RunSummary.array().parse(body.runs)
    expect(runs.map((r) => r.id)).toEqual(['a-new', 'a-old'])
  })

  it('includes terminal/old runs (history is durable, not the board)', async () => {
    const app = createServer({ db, secret: SECRET, runTtlMs: 1 })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await seedRun(accountId, 'm1', {
      id: 'ancient-done',
      state: 'done',
      lastActiveAt: '2020-01-01T00:00:00Z',
    })

    const res = await app.inject({
      method: 'GET',
      url: '/v1/history',
      headers: { authorization: `Bearer ${token}` },
    })
    const body = res.json() as { runs: Array<{ id: string }> }
    expect(body.runs.map((r) => r.id)).toContain('ancient-done')
  })

  it('applies q / state / agentKind / machineId / before filters from query params', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.machine.create({ data: { id: 'm2', accountId, metadataEnc: 'x' } })
    await seedRun(accountId, 'm1', {
      id: 'deploy-1',
      state: 'done',
      agentKind: 'claude',
      lastActiveAt: '2026-06-03T00:00:00Z',
    })
    await seedRun(accountId, 'm2', {
      id: 'deploy-2',
      state: 'idle',
      agentKind: 'kimi',
      lastActiveAt: '2026-06-04T00:00:00Z',
    })
    await seedRun(accountId, 'm1', {
      id: 'other',
      state: 'idle',
      agentKind: 'claude',
      lastActiveAt: '2026-06-05T00:00:00Z',
    })

    const byQ = await app.inject({
      method: 'GET',
      url: '/v1/history?q=deploy',
      headers: { authorization: `Bearer ${token}` },
    })
    expect((byQ.json() as { runs: Array<{ id: string }> }).runs.map((r) => r.id).sort()).toEqual([
      'deploy-1',
      'deploy-2',
    ])

    const byState = await app.inject({
      method: 'GET',
      url: '/v1/history?state=done',
      headers: { authorization: `Bearer ${token}` },
    })
    expect((byState.json() as { runs: Array<{ id: string }> }).runs.map((r) => r.id)).toEqual([
      'deploy-1',
    ])

    const byKind = await app.inject({
      method: 'GET',
      url: '/v1/history?agentKind=kimi',
      headers: { authorization: `Bearer ${token}` },
    })
    expect((byKind.json() as { runs: Array<{ id: string }> }).runs.map((r) => r.id)).toEqual([
      'deploy-2',
    ])

    const byMachine = await app.inject({
      method: 'GET',
      url: '/v1/history?machineId=m2',
      headers: { authorization: `Bearer ${token}` },
    })
    expect((byMachine.json() as { runs: Array<{ id: string }> }).runs.map((r) => r.id)).toEqual([
      'deploy-2',
    ])
  })

  it('paginates via limit + nextCursor', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    for (let i = 0; i < 4; i++) {
      await seedRun(accountId, 'm1', { id: `r${i}`, lastActiveAt: `2026-06-0${i + 1}T00:00:00Z` })
    }

    const page1 = await app.inject({
      method: 'GET',
      url: '/v1/history?limit=2',
      headers: { authorization: `Bearer ${token}` },
    })
    const b1 = page1.json() as { runs: Array<{ id: string }>; nextCursor?: number }
    expect(b1.runs.map((r) => r.id)).toEqual(['r3', 'r2'])
    expect(typeof b1.nextCursor).toBe('number')

    const page2 = await app.inject({
      method: 'GET',
      url: `/v1/history?limit=2&before=${b1.nextCursor}`,
      headers: { authorization: `Bearer ${token}` },
    })
    const b2 = page2.json() as { runs: Array<{ id: string }>; nextCursor?: number }
    expect(b2.runs.map((r) => r.id)).toEqual(['r1', 'r0'])
    expect(b2.nextCursor).toBeUndefined()
  })

  it('finds a renamed run in history by its TITLE (rename endpoint → history search, real flow)', async () => {
    // The end-to-end flow a user actually performs: rename a run to a memorable
    // name, then later look it up in history by that name. The rename persists
    // via POST /v1/runs/:id/rename; the search must then surface it by title.
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await seedRun(accountId, 'm1', { id: 'run-opaque-9f3', lastActiveAt: '2026-06-03T00:00:00Z' })

    const auth = { authorization: `Bearer ${token}` }

    // Before the rename, the visible name does not exist anywhere — search misses.
    const before = await app.inject({
      method: 'GET',
      url: '/v1/history?q=invoice',
      headers: auth,
    })
    expect((before.json() as { runs: Array<{ id: string }> }).runs).toEqual([])

    // Rename the run (the same endpoint the console "rename" action calls).
    const renamed = await app.inject({
      method: 'POST',
      url: '/v1/runs/run-opaque-9f3/rename',
      headers: { ...auth, 'content-type': 'application/json' },
      payload: { title: 'Invoice import bugfix' },
    })
    expect(renamed.statusCode).toBe(200)

    // Now searching history by the visible title finds it, with the title shown.
    const after = await app.inject({
      method: 'GET',
      url: '/v1/history?q=invoice',
      headers: auth,
    })
    const runs = RunSummary.array().parse((after.json() as { runs: unknown[] }).runs)
    expect(runs.map((r) => r.id)).toEqual(['run-opaque-9f3'])
    expect(runs[0]?.title).toBe('Invoice import bugfix')
  })

  it('is rate-limited per IP under the reads bucket', async () => {
    const app = createServer({ db, secret: SECRET, rateLimits: { readsPerMin: 5 } })
    const { token } = await login(app)
    const results: number[] = []
    for (let i = 0; i < 8; i++) {
      const res = await app.inject({
        method: 'GET',
        url: '/v1/history',
        headers: { authorization: `Bearer ${token}` },
      })
      results.push(res.statusCode)
    }
    expect(results.some((s) => s === 429)).toBe(true)
  })
})
