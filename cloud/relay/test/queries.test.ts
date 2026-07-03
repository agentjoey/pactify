import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db.js'
import { listRuns, listPendingApprovals, getRunEventsAfter, toRunSummary } from '../src/queries.js'

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

describe('relay queries on PGlite', () => {
  let db: PrismaClient

  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('listRuns returns an account newest-active first and excludes other accounts', async () => {
    const a = await seedAccount(db, 'pk-a')
    const b = await seedAccount(db, 'pk-b')

    await db.run.create({
      data: {
        id: 'a-old',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'idle',
        lastActiveAt: new Date('2026-06-01T00:00:00Z'),
      },
    })
    await db.run.create({
      data: {
        id: 'a-new',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'thinking',
        lastActiveAt: new Date('2026-06-20T00:00:00Z'),
      },
    })
    await db.run.create({
      data: {
        id: 'b-run',
        accountId: b.accountId,
        machineId: b.machineId,
        agentKind: 'claude',
        state: 'idle',
        lastActiveAt: new Date('2026-06-22T00:00:00Z'),
      },
    })

    const runs = await listRuns(db, a.accountId)
    expect(runs.map((r) => r.id)).toEqual(['a-new', 'a-old'])
    expect(runs.some((r) => r.id === 'b-run')).toBe(false)
  })

  it('listRuns hides stale terminal runs past the ttl but keeps recent/active ones', async () => {
    const a = await seedAccount(db, 'pk-a')
    const ttlMs = 7 * 24 * 60 * 60 * 1000
    const now = Date.now()
    const stale = new Date(now - ttlMs - 60_000)
    const recent = new Date(now - 60_000)

    // stale terminal -> hidden once a ttl is supplied
    await db.run.create({
      data: {
        id: 'ghost-done',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'done',
        lastActiveAt: stale,
      },
    })
    // recent terminal -> kept
    await db.run.create({
      data: {
        id: 'recent-done',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'done',
        lastActiveAt: recent,
      },
    })
    // stale but non-terminal -> always kept
    await db.run.create({
      data: {
        id: 'stale-live',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'thinking',
        lastActiveAt: stale,
      },
    })

    const withTtl = await listRuns(db, a.accountId, { ttlMs })
    const ids = withTtl.map((r) => r.id)
    expect(ids).toContain('recent-done')
    expect(ids).toContain('stale-live')
    expect(ids).not.toContain('ghost-done')

    // ttl <= 0 disables board hygiene: the ghost is visible again.
    const noTtl = await listRuns(db, a.accountId, { ttlMs: 0 })
    expect(noTtl.map((r) => r.id)).toContain('ghost-done')

    // default (no opts) keeps prior behavior: no hidden runs.
    const dflt = await listRuns(db, a.accountId)
    expect(dflt.map((r) => r.id)).toContain('ghost-done')
  })

  it('listPendingApprovals filters to pendingApprovals > 0', async () => {
    const a = await seedAccount(db, 'pk-a')

    await db.run.create({
      data: {
        id: 'no-approvals',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'idle',
        pendingApprovals: 0,
      },
    })
    await db.run.create({
      data: {
        id: 'with-approvals',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'awaiting-approval',
        pendingApprovals: 2,
      },
    })

    const pending = await listPendingApprovals(db, a.accountId)
    expect(pending.map((r) => r.id)).toEqual(['with-approvals'])
  })

  it('getRunEventsAfter paginates by seq, ascending', async () => {
    const a = await seedAccount(db, 'pk-a')
    await db.run.create({
      data: {
        id: 'r1',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'thinking',
      },
    })
    for (const seq of [0, 1, 2, 3]) {
      await db.runEvent.create({
        data: {
          runId: 'r1',
          seq,
          state: 'thinking',
          eventKind: 'message',
          ts: BigInt(1000 + seq),
          bodyEnc: 'enc',
        },
      })
    }

    const after1 = await getRunEventsAfter(db, 'r1', 1)
    expect(after1.map((e) => e.seq)).toEqual([2, 3])

    const after3 = await getRunEventsAfter(db, 'r1', 3)
    expect(after3).toEqual([])
  })

  it('toRunSummary surfaces the run branch (and undefined when null)', async () => {
    const a = await seedAccount(db, 'pk-a')
    await db.run.create({
      data: {
        id: 'with-branch',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'thinking',
        branch: 'feat/c0f',
      },
    })
    await db.run.create({
      data: {
        id: 'no-branch',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'idle',
      },
    })
    const withBranch = await db.run.findUniqueOrThrow({ where: { id: 'with-branch' } })
    const noBranch = await db.run.findUniqueOrThrow({ where: { id: 'no-branch' } })
    expect(toRunSummary(withBranch).branch).toBe('feat/c0f')
    expect(toRunSummary(noBranch).branch).toBeUndefined()
  })

  it('toRunSummary surfaces the run workdir (and undefined when null)', async () => {
    const a = await seedAccount(db, 'pk-wd')
    await db.run.create({
      data: {
        id: 'with-workdir',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'thinking',
        workdir: '/Users/me/repo',
      },
    })
    await db.run.create({
      data: {
        id: 'no-workdir',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'idle',
      },
    })
    const withWorkdir = await db.run.findUniqueOrThrow({ where: { id: 'with-workdir' } })
    const noWorkdir = await db.run.findUniqueOrThrow({ where: { id: 'no-workdir' } })
    expect(toRunSummary(withWorkdir).workdir).toBe('/Users/me/repo')
    expect(toRunSummary(noWorkdir).workdir).toBeUndefined()
  })

  it('toRunSummary surfaces startedAt as epoch ms (and undefined when null)', async () => {
    const a = await seedAccount(db, 'pk-a')
    await db.run.create({
      data: {
        id: 'with-started',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'thinking',
        startedAtMs: 1719000000000n,
      },
    })
    await db.run.create({
      data: {
        id: 'no-started',
        accountId: a.accountId,
        machineId: a.machineId,
        agentKind: 'claude',
        state: 'idle',
      },
    })
    const withStarted = await db.run.findUniqueOrThrow({ where: { id: 'with-started' } })
    const noStarted = await db.run.findUniqueOrThrow({ where: { id: 'no-started' } })
    expect(toRunSummary(withStarted).startedAt).toBe(1719000000000)
    expect(toRunSummary(noStarted).startedAt).toBeUndefined()
  })
})
