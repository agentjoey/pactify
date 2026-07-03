import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db.js'
import { ingestPactEvent, listProjects, getProjectEvents, type PactEventInput } from '../src/pact.js'

function ev(overrides: Partial<PactEventInput> = {}): PactEventInput {
  return {
    projectId: 'acct1:pactify',
    name: 'pactify',
    feature: 'cloud-m0',
    eventType: 'assign',
    task: 'm0-wire',
    seq: 0,
    ts: 1000,
    bodyEnc: 'ciphertext-0',
    ...overrides,
  }
}

describe('relay pact data layer (U2 S1) on PGlite', () => {
  let db: PrismaClient
  let accountId: string

  beforeEach(async () => {
    db = await createPgliteDb()
    const account = await db.account.create({ data: { id: 'acct1', publicKey: 'pk' } })
    accountId = account.id
  })

  it('ingest creates a Project + PactEvent with cleartext operational columns', async () => {
    await ingestPactEvent(db, accountId, ev())
    const [proj] = await listProjects(db, accountId)
    expect(proj?.id).toBe('acct1:pactify')
    expect(proj?.name).toBe('pactify')
    expect(proj?.feature).toBe('cloud-m0')
    expect(proj?.seq).toBe(0)

    const events = await getProjectEvents(db, 'acct1:pactify')
    expect(events).toHaveLength(1)
    expect(events[0]?.eventType).toBe('assign')
    expect(events[0]?.task).toBe('m0-wire')
    expect(events[0]?.bodyEnc).toBe('ciphertext-0')
    expect(events[0]?.accountId).toBe(accountId)
  })

  it('advances the project summary and appends events in seq order', async () => {
    await ingestPactEvent(db, accountId, ev({ seq: 0, eventType: 'assign' }))
    await ingestPactEvent(db, accountId, ev({ seq: 1, eventType: 'checkpoint', bodyEnc: 'c1' }))
    await ingestPactEvent(db, accountId, ev({ seq: 2, eventType: 'accept', feature: 'cloud-m0', bodyEnc: 'c2' }))

    const [proj] = await listProjects(db, accountId)
    expect(proj?.seq).toBe(2)
    const events = await getProjectEvents(db, 'acct1:pactify')
    expect(events.map((e) => e.eventType)).toEqual(['assign', 'checkpoint', 'accept'])
    expect(await getProjectEvents(db, 'acct1:pactify', { afterSeq: 0 })).toHaveLength(2)
  })

  it('duplicate (projectId, seq) ingest is idempotent — no crash, one event', async () => {
    await ingestPactEvent(db, accountId, ev({ seq: 0 }))
    await ingestPactEvent(db, accountId, ev({ seq: 0 })) // retry
    expect(await getProjectEvents(db, 'acct1:pactify')).toHaveLength(1)
  })

  it('a stale (lower-seq) event does not roll back the project summary', async () => {
    await ingestPactEvent(db, accountId, ev({ seq: 2, eventType: 'accept', feature: 'f2' }))
    await ingestPactEvent(db, accountId, ev({ seq: 1, eventType: 'checkpoint', feature: 'f1' }))
    const [proj] = await listProjects(db, accountId)
    expect(proj?.seq).toBe(2)
    expect(proj?.feature).toBe('f2')
    // the late event is still recorded in the log.
    expect((await getProjectEvents(db, 'acct1:pactify')).map((e) => e.seq)).toEqual([1, 2])
  })

  it('projects are scoped per account and ordered by recency', async () => {
    await db.account.create({ data: { id: 'acct2', publicKey: 'pk2' } })
    await ingestPactEvent(db, accountId, ev({ projectId: 'acct1:a', name: 'a', seq: 0, ts: 1000 }))
    await ingestPactEvent(db, accountId, ev({ projectId: 'acct1:b', name: 'b', seq: 0, ts: 2000 }))
    await ingestPactEvent(db, 'acct2', ev({ projectId: 'acct2:c', name: 'c', seq: 0 }))

    const mine = await listProjects(db, accountId)
    expect(mine.map((p) => p.name)).toEqual(['b', 'a']) // most-recent first
    expect(await listProjects(db, 'acct2')).toHaveLength(1)
  })
})
