import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db.js'
import { sweepExpiredMachines, DEFAULT_MACHINE_TTL_MS } from '../src/machines.js'
import { listMachines } from '../src/machines.js'

/** Create an Account and return its id. */
async function seedAccount(db: PrismaClient, publicKey: string): Promise<string> {
  const account = await db.account.create({ data: { publicKey } })
  return account.id
}

/** Insert a Machine with explicit online/lastSeenAt (epoch ms). */
async function seedMachine(
  db: PrismaClient,
  accountId: string,
  id: string,
  online: boolean,
  lastSeenMs: number,
): Promise<void> {
  await db.machine.create({
    data: { id, accountId, metadataEnc: 'enc', online, lastSeenAt: BigInt(lastSeenMs) },
  })
}

describe('sweepExpiredMachines on PGlite', () => {
  let db: PrismaClient
  const nowMs = new Date('2026-06-25T00:00:00Z').getTime()
  const now = new Date(nowMs)
  const ttlMs = 7 * 24 * 60 * 60 * 1000 // 7 days
  const oldMs = nowMs - ttlMs - 1 // just past the ttl
  const freshMs = nowMs - 60_000 // a minute ago

  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('deletes offline machines older than the ttl, returning the count', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedMachine(db, a, 'old-offline-1', false, oldMs)
    await seedMachine(db, a, 'old-offline-2', false, oldMs)

    const deleted = await sweepExpiredMachines(db, now, { ttlMs })

    expect(deleted).toBe(2)
    expect(await db.machine.findUnique({ where: { id: 'old-offline-1' } })).toBeNull()
    expect(await db.machine.findUnique({ where: { id: 'old-offline-2' } })).toBeNull()
  })

  it('keeps online machines (even when stale) and recent offline machines', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedMachine(db, a, 'online-stale', true, oldMs)
    await seedMachine(db, a, 'offline-recent', false, freshMs)
    await seedMachine(db, a, 'offline-stale', false, oldMs)

    const deleted = await sweepExpiredMachines(db, now, { ttlMs })

    expect(deleted).toBe(1)
    expect(await db.machine.findUnique({ where: { id: 'online-stale' } })).not.toBeNull()
    expect(await db.machine.findUnique({ where: { id: 'offline-recent' } })).not.toBeNull()
    expect(await db.machine.findUnique({ where: { id: 'offline-stale' } })).toBeNull()
  })

  it('is a no-op when ttlMs <= 0', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedMachine(db, a, 'offline-stale', false, oldMs)

    expect(await sweepExpiredMachines(db, now, { ttlMs: 0 })).toBe(0)
    expect(await sweepExpiredMachines(db, now, { ttlMs: -1 })).toBe(0)
    expect(await db.machine.findUnique({ where: { id: 'offline-stale' } })).not.toBeNull()
  })

  it('is FK-safe: does not delete a stale offline machine still referenced by runs', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedMachine(db, a, 'offline-with-run', false, oldMs)
    await db.run.create({
      data: {
        id: 'r1',
        accountId: a,
        machineId: 'offline-with-run',
        agentKind: 'claude',
        state: 'done',
      },
    })

    const deleted = await sweepExpiredMachines(db, now, { ttlMs })

    expect(deleted).toBe(0)
    expect(await db.machine.findUnique({ where: { id: 'offline-with-run' } })).not.toBeNull()
  })

  it('exposes a sane default ttl (7 days)', () => {
    expect(DEFAULT_MACHINE_TTL_MS).toBe(7 * 24 * 60 * 60 * 1000)
  })
})

describe('listMachines board hygiene', () => {
  let db: PrismaClient
  const nowMs = new Date('2026-06-25T00:00:00Z').getTime()
  const ttlMs = 7 * 24 * 60 * 60 * 1000
  const oldMs = nowMs - ttlMs - 1
  const freshMs = nowMs - 60_000

  beforeEach(async () => {
    db = await createPgliteDb()
  })

  it('excludes a stale offline machine but keeps online + recent ones', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedMachine(db, a, 'online-stale', true, oldMs)
    await seedMachine(db, a, 'offline-recent', false, freshMs)
    await seedMachine(db, a, 'offline-stale', false, oldMs)

    const machines = await listMachines(db, a, { ttlMs, now: () => nowMs })
    const ids = machines.map((m) => m.machineId).sort()

    expect(ids).toEqual(['offline-recent', 'online-stale'])
  })

  it('keeps all machines when no ttl is given', async () => {
    const a = await seedAccount(db, 'pk-a')
    await seedMachine(db, a, 'offline-stale', false, oldMs)

    const machines = await listMachines(db, a)
    expect(machines.map((m) => m.machineId)).toEqual(['offline-stale'])
  })
})
