import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db.js'
import { searchRuns } from '../src/queries.js'

/** Seed an Account + Machine, returning the account id (machine id === accountId+'-m'). */
async function seedAccount(
  db: PrismaClient,
  publicKey: string,
): Promise<{ accountId: string; machineId: string }> {
  const account = await db.account.create({ data: { publicKey } })
  const machineId = `${account.id}-m`
  await db.machine.create({ data: { id: machineId, accountId: account.id, metadataEnc: 'enc' } })
  return { accountId: account.id, machineId }
}

interface RunSeed {
  id: string
  state?: string
  agentKind?: string
  branch?: string | null
  title?: string | null
  lastActiveAt: string
}

async function seedRun(
  db: PrismaClient,
  accountId: string,
  machineId: string,
  r: RunSeed,
): Promise<void> {
  await db.run.create({
    data: {
      id: r.id,
      accountId,
      machineId,
      agentKind: r.agentKind ?? 'claude',
      state: r.state ?? 'idle',
      branch: r.branch ?? null,
      title: r.title ?? null,
      lastActiveAt: new Date(r.lastActiveAt),
    },
  })
}

describe('searchRuns on PGlite', () => {
  let db: PrismaClient

  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('is account-scoped: account A never sees account B history', async () => {
    const a = await seedAccount(db, 'pk-a')
    const b = await seedAccount(db, 'pk-b')
    await seedRun(db, a.accountId, a.machineId, { id: 'a1', lastActiveAt: '2026-06-01T00:00:00Z' })
    await seedRun(db, b.accountId, b.machineId, { id: 'b1', lastActiveAt: '2026-06-02T00:00:00Z' })

    const res = await searchRuns(db, a.accountId, {})
    expect(res.runs.map((r) => r.id)).toEqual(['a1'])
    expect(res.runs.some((r) => r.id === 'b1')).toBe(false)
  })

  it('orders by recency (lastActiveAt desc)', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a.accountId, a.machineId, { id: 'old', lastActiveAt: '2026-06-01T00:00:00Z' })
    await seedRun(db, a.accountId, a.machineId, { id: 'new', lastActiveAt: '2026-06-20T00:00:00Z' })
    await seedRun(db, a.accountId, a.machineId, { id: 'mid', lastActiveAt: '2026-06-10T00:00:00Z' })

    const res = await searchRuns(db, a.accountId, {})
    expect(res.runs.map((r) => r.id)).toEqual(['new', 'mid', 'old'])
  })

  it('includes terminal/old runs (no board retention exclusion)', async () => {
    const a = await seedAccount(db, 'pk-a')
    // very old terminal run that the board would hide
    await seedRun(db, a.accountId, a.machineId, {
      id: 'ancient-done',
      state: 'done',
      lastActiveAt: '2020-01-01T00:00:00Z',
    })
    await seedRun(db, a.accountId, a.machineId, {
      id: 'err',
      state: 'error',
      lastActiveAt: '2020-02-01T00:00:00Z',
    })

    const res = await searchRuns(db, a.accountId, {})
    const ids = res.runs.map((r) => r.id)
    expect(ids).toContain('ancient-done')
    expect(ids).toContain('err')
  })

  it('filters by substring q over runId / branch / machineId', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a.accountId, a.machineId, {
      id: 'deploy-prod-1',
      branch: 'main',
      lastActiveAt: '2026-06-03T00:00:00Z',
    })
    await seedRun(db, a.accountId, a.machineId, {
      id: 'feature-x',
      branch: 'feat/deploy-thing',
      lastActiveAt: '2026-06-02T00:00:00Z',
    })
    await seedRun(db, a.accountId, a.machineId, {
      id: 'unrelated',
      branch: 'chore',
      lastActiveAt: '2026-06-01T00:00:00Z',
    })

    // matches runId of the first and branch of the second
    const res = await searchRuns(db, a.accountId, { q: 'deploy' })
    expect(res.runs.map((r) => r.id).sort()).toEqual(['deploy-prod-1', 'feature-x'])

    // q over machineId
    const byMachine = await searchRuns(db, a.accountId, { q: a.machineId })
    expect(byMachine.runs.length).toBe(3)
  })

  it('filters by substring q over the run TITLE (the human label shown on board/console/history)', async () => {
    // A user renames a run "Nightly deploy"; the board, console and history list
    // all show that title as the run's label. Searching history for that same
    // visible name MUST find it — otherwise the search surface is inconsistent
    // with every display surface (the run is unfindable by what the user sees).
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a.accountId, a.machineId, {
      id: 'run-abc123', // opaque id — the user never types this
      title: 'Nightly deploy pipeline',
      lastActiveAt: '2026-06-03T00:00:00Z',
    })
    await seedRun(db, a.accountId, a.machineId, {
      id: 'run-def456',
      title: 'Refactor auth module',
      lastActiveAt: '2026-06-02T00:00:00Z',
    })

    const byTitle = await searchRuns(db, a.accountId, { q: 'nightly' })
    expect(byTitle.runs.map((r) => r.id)).toEqual(['run-abc123'])

    // Case-insensitive, like the id/branch/machineId match.
    const byTitleMixed = await searchRuns(db, a.accountId, { q: 'AUTH' })
    expect(byTitleMixed.runs.map((r) => r.id)).toEqual(['run-def456'])
  })

  it('q substring match is case-insensitive', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a.accountId, a.machineId, {
      id: 'Deploy-Upper',
      lastActiveAt: '2026-06-03T00:00:00Z',
    })
    const res = await searchRuns(db, a.accountId, { q: 'deploy' })
    expect(res.runs.map((r) => r.id)).toEqual(['Deploy-Upper'])
  })

  it('filters by state', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a.accountId, a.machineId, {
      id: 'd1',
      state: 'done',
      lastActiveAt: '2026-06-03T00:00:00Z',
    })
    await seedRun(db, a.accountId, a.machineId, {
      id: 'i1',
      state: 'idle',
      lastActiveAt: '2026-06-02T00:00:00Z',
    })
    const res = await searchRuns(db, a.accountId, { state: 'done' })
    expect(res.runs.map((r) => r.id)).toEqual(['d1'])
  })

  it('filters by agentKind', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a.accountId, a.machineId, {
      id: 'c1',
      agentKind: 'claude',
      lastActiveAt: '2026-06-03T00:00:00Z',
    })
    await seedRun(db, a.accountId, a.machineId, {
      id: 'k1',
      agentKind: 'kimi',
      lastActiveAt: '2026-06-02T00:00:00Z',
    })
    const res = await searchRuns(db, a.accountId, { agentKind: 'kimi' })
    expect(res.runs.map((r) => r.id)).toEqual(['k1'])
  })

  it('filters by machineId', async () => {
    const a = await seedAccount(db, 'pk-a')
    const m2 = `${a.accountId}-m2`
    await db.machine.create({ data: { id: m2, accountId: a.accountId, metadataEnc: 'enc' } })
    await seedRun(db, a.accountId, a.machineId, { id: 'r1', lastActiveAt: '2026-06-03T00:00:00Z' })
    await seedRun(db, a.accountId, m2, { id: 'r2', lastActiveAt: '2026-06-02T00:00:00Z' })
    const res = await searchRuns(db, a.accountId, { machineId: m2 })
    expect(res.runs.map((r) => r.id)).toEqual(['r2'])
  })

  it('filters by before (updatedAt cursor, exclusive)', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a.accountId, a.machineId, { id: 'r1', lastActiveAt: '2026-06-01T00:00:00Z' })
    await seedRun(db, a.accountId, a.machineId, { id: 'r2', lastActiveAt: '2026-06-10T00:00:00Z' })
    await seedRun(db, a.accountId, a.machineId, { id: 'r3', lastActiveAt: '2026-06-20T00:00:00Z' })

    const cutoff = new Date('2026-06-10T00:00:00Z').getTime()
    const res = await searchRuns(db, a.accountId, { before: cutoff })
    // strictly older than r2's lastActiveAt
    expect(res.runs.map((r) => r.id)).toEqual(['r1'])
  })

  it('paginates via nextCursor', async () => {
    const a = await seedAccount(db, 'pk-a')
    for (let i = 0; i < 5; i++) {
      await seedRun(db, a.accountId, a.machineId, {
        id: `r${i}`,
        lastActiveAt: `2026-06-0${i + 1}T00:00:00Z`,
      })
    }
    const page1 = await searchRuns(db, a.accountId, { limit: 2 })
    expect(page1.runs.map((r) => r.id)).toEqual(['r4', 'r3'])
    expect(page1.nextCursor).toBeDefined()

    const page2 = await searchRuns(db, a.accountId, { limit: 2, before: page1.nextCursor })
    expect(page2.runs.map((r) => r.id)).toEqual(['r2', 'r1'])
    expect(page2.nextCursor).toBeDefined()

    const page3 = await searchRuns(db, a.accountId, { limit: 2, before: page2.nextCursor })
    expect(page3.runs.map((r) => r.id)).toEqual(['r0'])
    expect(page3.nextCursor).toBeUndefined()
  })

  it('caps limit at the max even when a larger limit is requested', async () => {
    const a = await seedAccount(db, 'pk-a')
    for (let i = 0; i < 60; i++) {
      await seedRun(db, a.accountId, a.machineId, {
        id: `r${i}`,
        lastActiveAt: new Date(2026, 5, 1, 0, i).toISOString(),
      })
    }
    const res = await searchRuns(db, a.accountId, { limit: 9999 })
    // default cap is 50
    expect(res.runs.length).toBe(50)
    expect(res.nextCursor).toBeDefined()
  })

  it('defaults to limit 50 when unspecified', async () => {
    const a = await seedAccount(db, 'pk-a')
    for (let i = 0; i < 55; i++) {
      await seedRun(db, a.accountId, a.machineId, {
        id: `r${i}`,
        lastActiveAt: new Date(2026, 5, 1, 0, i).toISOString(),
      })
    }
    const res = await searchRuns(db, a.accountId, {})
    expect(res.runs.length).toBe(50)
  })
})
