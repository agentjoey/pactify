import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db.js'
import {
  sweepExpired,
  startRetention,
  DEFAULT_RUN_TTL_MS,
  DEFAULT_SWEEP_INTERVAL_MS,
} from '../src/retention.js'
import type { LogRecord } from '../src/log.js'
import { createLogger } from '../src/log.js'

/** Seed an Account + Machine, returning ids (machine id === accountId+'-m'). */
async function seedAccount(
  db: PrismaClient,
  publicKey: string,
): Promise<{ accountId: string; machineId: string }> {
  const account = await db.account.create({ data: { publicKey } })
  const machineId = `${account.id}-m`
  await db.machine.create({ data: { id: machineId, accountId: account.id, metadataEnc: 'enc' } })
  return { accountId: account.id, machineId }
}

/** Insert a run with a given state + lastActiveAt and `n` events. */
async function seedRun(
  db: PrismaClient,
  ids: { accountId: string; machineId: string },
  id: string,
  state: string,
  lastActiveAt: Date,
  events = 0,
): Promise<void> {
  await db.run.create({
    data: {
      id,
      accountId: ids.accountId,
      machineId: ids.machineId,
      agentKind: 'claude',
      state,
      lastActiveAt,
    },
  })
  for (let seq = 0; seq < events; seq++) {
    await db.runEvent.create({
      data: { runId: id, seq, state, eventKind: 'message', ts: BigInt(seq), bodyEnc: 'enc' },
    })
  }
}

describe('sweepExpired on PGlite', () => {
  let db: PrismaClient
  const now = new Date('2026-06-25T00:00:00Z')
  const ttlMs = 7 * 24 * 60 * 60 * 1000 // 7 days
  const old = new Date(now.getTime() - ttlMs - 1) // just past the ttl
  const fresh = new Date(now.getTime() - 60_000) // a minute ago

  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('deletes terminal runs older than the ttl and their events, returning the count', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a, 'old-done', 'done', old, 3)
    await seedRun(db, a, 'old-error', 'error', old, 2)

    const deleted = await sweepExpired(db, now, { ttlMs })

    expect(deleted).toBe(2)
    expect(await db.run.findUnique({ where: { id: 'old-done' } })).toBeNull()
    expect(await db.run.findUnique({ where: { id: 'old-error' } })).toBeNull()
    expect(await db.runEvent.count()).toBe(0)
  })

  it('keeps recent terminal runs and non-terminal runs (even when old)', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a, 'recent-done', 'done', fresh, 1)
    await seedRun(db, a, 'old-thinking', 'thinking', old, 1)
    await seedRun(db, a, 'old-idle', 'idle', old, 1)
    await seedRun(db, a, 'old-awaiting', 'awaiting-approval', old, 1)
    await seedRun(db, a, 'old-blocked', 'blocked', old, 1)
    await seedRun(db, a, 'old-done', 'done', old, 1)

    const deleted = await sweepExpired(db, now, { ttlMs })

    expect(deleted).toBe(1)
    expect(await db.run.findUnique({ where: { id: 'recent-done' } })).not.toBeNull()
    expect(await db.run.findUnique({ where: { id: 'old-thinking' } })).not.toBeNull()
    expect(await db.run.findUnique({ where: { id: 'old-idle' } })).not.toBeNull()
    expect(await db.run.findUnique({ where: { id: 'old-awaiting' } })).not.toBeNull()
    expect(await db.run.findUnique({ where: { id: 'old-blocked' } })).not.toBeNull()
    expect(await db.run.findUnique({ where: { id: 'old-done' } })).toBeNull()
    // the 5 surviving runs keep their events; only the swept run's event is gone.
    expect(await db.runEvent.count()).toBe(5)
  })

  it('is a no-op when ttlMs <= 0', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a, 'old-done', 'done', old, 2)

    expect(await sweepExpired(db, now, { ttlMs: 0 })).toBe(0)
    expect(await sweepExpired(db, now, { ttlMs: -1 })).toBe(0)
    expect(await db.run.findUnique({ where: { id: 'old-done' } })).not.toBeNull()
    expect(await db.runEvent.count()).toBe(2)
  })

  it('exposes sane defaults (7 days, 1 hour)', () => {
    expect(DEFAULT_RUN_TTL_MS).toBe(7 * 24 * 60 * 60 * 1000)
    expect(DEFAULT_SWEEP_INTERVAL_MS).toBe(60 * 60 * 1000)
  })
})

describe('startRetention scheduler', () => {
  let db: PrismaClient
  const ttlMs = 7 * 24 * 60 * 60 * 1000
  const nowMs = new Date('2026-06-25T00:00:00Z').getTime()
  const old = new Date(nowMs - ttlMs - 1)

  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('sweeps on each interval tick and logs a structured info line with the count', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a, 'old-done', 'done', old, 1)
    const records: LogRecord[] = []
    const logger = createLogger({ name: 'relay', level: 'info', sink: (r) => records.push(r) })

    const sweeper = startRetention(db, {
      ttlMs,
      intervalMs: 5,
      now: () => nowMs,
      logger,
    })
    // give the interval a couple of ticks
    await new Promise((r) => setTimeout(r, 30))
    sweeper.stop()

    expect(await db.run.findUnique({ where: { id: 'old-done' } })).toBeNull()
    const sweepLog = records.find((r) => r.msg === 'retention sweep')
    expect(sweepLog).toBeDefined()
    expect(sweepLog?.level).toBe('info')
    expect(sweepLog?.fields).toMatchObject({ deleted: 1, ttlMs })
  })

  it('is a no-op (never sweeps) when ttlMs <= 0', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedRun(db, a, 'old-done', 'done', old, 1)
    const records: LogRecord[] = []
    const logger = createLogger({ name: 'relay', level: 'info', sink: (r) => records.push(r) })

    const sweeper = startRetention(db, { ttlMs: 0, intervalMs: 5, now: () => nowMs, logger })
    await new Promise((r) => setTimeout(r, 30))
    sweeper.stop()

    expect(await db.run.findUnique({ where: { id: 'old-done' } })).not.toBeNull()
    expect(records.some((r) => r.msg === 'retention sweep')).toBe(false)
  })
})
