import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import type { WireMessage } from '@pactify-apps/wire'
import { createPgliteDb } from '../src/db.js'
import { ingestWireMessage, getRun, getRunEvents, deleteRun, deleteMachine } from '../src/repo.js'
import { listRuns } from '../src/queries.js'

/** Build a WireMessage with a default operational header + ciphertext body. */
function wireMessage(overrides: Partial<WireMessage['header']> = {}): WireMessage {
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
      ...overrides,
    },
    body: { alg: 'xchacha20poly1305', nonce: 'n', ct: 'c' },
  }
}

describe('relay repo: ingest/read round-trip on PGlite', () => {
  let db: PrismaClient
  let accountId: string

  beforeEach(async () => {
    db = await createPgliteDb()
    const account = await db.account.create({ data: { publicKey: 'pk' } })
    accountId = account.id
    await db.machine.create({
      data: { id: 'm1', accountId, metadataEnc: 'enc' },
    })
  })

  it('ingest creates a Run + RunEvent', async () => {
    const msg = wireMessage()
    await ingestWireMessage(db, accountId, 'claude', msg)

    const run = await getRun(db, 'r1')
    expect(run).not.toBeNull()
    expect(run?.state).toBe('thinking')
    expect(run?.machineId).toBe('m1')
    expect(run?.agentKind).toBe('claude')
    expect(run?.accountId).toBe(accountId)

    const events = await getRunEvents(db, 'r1')
    expect(events).toHaveLength(1)
    expect(events[0]?.seq).toBe(0)
    expect(events[0]?.eventKind).toBe('message')
    expect(JSON.parse(events[0]?.bodyEnc ?? 'null')).toEqual(msg.body)
  })

  it('duplicate ingest of the same (runId, seq) is idempotent — no crash, one event row', async () => {
    const msg = wireMessage({ seq: 0 })
    await ingestWireMessage(db, accountId, 'claude', msg)
    await ingestWireMessage(db, accountId, 'claude', msg) // retry / reconnect resend
    const events = await getRunEvents(db, 'r1')
    expect(events).toHaveLength(1)
    expect((await getRun(db, 'r1'))?.state).toBe('thinking')
  })

  it('a stale (lower-seq) header does not roll back the run state/seq', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 0, state: 'thinking' }))
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 2, state: 'blocked' }))
    // A stale header (seq 1) whose write settles late must NOT overwrite seq 2.
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, state: 'idle' }))
    const run = await getRun(db, 'r1')
    expect(run?.seq).toBe(2)
    expect(run?.state).toBe('blocked')
    // The late event is still recorded in the log (all seqs kept).
    const events = await getRunEvents(db, 'r1')
    expect(events.map((e) => e.seq).sort((a, b) => a - b)).toEqual([0, 1, 2])
  })

  it('ingest persists the cleartext header branch onto the Run', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ branch: 'feat/c0f' }))
    expect((await getRun(db, 'r1'))?.branch).toBe('feat/c0f')

    // A later header without a branch leaves the column null (no clobber concern
    // here — header carries undefined → null).
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, ts: 2000 }))
    expect((await getRun(db, 'r1'))?.branch).toBeNull()
  })

  it('ingest persists the cleartext header workdir onto the Run', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ workdir: '/Users/me/repo' }))
    expect((await getRun(db, 'r1'))?.workdir).toBe('/Users/me/repo')

    // A later header without a workdir leaves the column null (header carries
    // undefined → null), matching the branch treatment.
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, ts: 2000 }))
    expect((await getRun(db, 'r1'))?.workdir).toBeNull()
  })

  it('ingest persists the cleartext header repoRoot onto the Run', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ repoRoot: '/Users/me/repo' }))
    expect((await getRun(db, 'r1'))?.repoRoot).toBe('/Users/me/repo')

    // A later header without a repoRoot leaves the column null, like branch/workdir.
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, ts: 2000 }))
    expect((await getRun(db, 'r1'))?.repoRoot).toBeNull()
  })

  it('folds the persisted repoRoot into the RunSummary via listRuns', async () => {
    await ingestWireMessage(
      db,
      accountId,
      'claude',
      wireMessage({ repoRoot: '/Users/me/repo', workdir: '/Users/me/repo/packages/wire' }),
    )
    const summaries = await listRuns(db, accountId)
    expect(summaries.find((s) => s.id === 'r1')?.repoRoot).toBe('/Users/me/repo')
  })

  it('ingest persists the cleartext header title onto the Run on creation, and a later header without a title does not clobber it', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ title: 'My Session' }))
    expect((await getRun(db, 'r1'))?.title).toBe('My Session')

    // Unlike branch/workdir, title is create-only: a later header without a
    // title MUST NOT null the column, so a post-spawn rename survives ongoing
    // header ingests.
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, ts: 2000 }))
    expect((await getRun(db, 'r1'))?.title).toBe('My Session')
  })

  it('a spawn title surfaces in the RunSummary listing', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ title: 'My Session' }))
    const summaries = await listRuns(db, accountId)
    expect(summaries).toHaveLength(1)
    expect(summaries[0]?.title).toBe('My Session')
  })

  it('ingest persists the cleartext header startedAt (epoch ms) onto the Run', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ startedAt: 1719000000000 }))
    expect((await getRun(db, 'r1'))?.startedAtMs).toBe(1719000000000n)

    // A later header without startedAt leaves the column null (header undefined → null).
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, ts: 2000 }))
    expect((await getRun(db, 'r1'))?.startedAtMs).toBeNull()
  })

  it('second ingest updates operational fields + appends an event', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage())
    await ingestWireMessage(
      db,
      accountId,
      'claude',
      wireMessage({
        seq: 1,
        state: 'awaiting-approval',
        pendingApprovals: 1,
        tokensIn: 12,
        ts: 2000,
      }),
    )

    const run = await getRun(db, 'r1')
    expect(run?.state).toBe('awaiting-approval')
    expect(run?.pendingApprovals).toBe(1)
    expect(run?.tokensIn).toBe(12)

    const events = await getRunEvents(db, 'r1')
    expect(events).toHaveLength(2)
    expect(events.map((e) => e.seq)).toEqual([0, 1])
  })

  it('server never stores plaintext — bodyEnc is exactly the ciphertext envelope', async () => {
    const msg = wireMessage()
    await ingestWireMessage(db, accountId, 'claude', msg)

    const events = await getRunEvents(db, 'r1')
    expect(events[0]?.bodyEnc).toBe(JSON.stringify(msg.body))
    // Only the opaque ciphertext envelope keys are present — no event content.
    const stored = JSON.parse(events[0]?.bodyEnc ?? 'null') as Record<string, unknown>
    expect(Object.keys(stored).sort()).toEqual(['alg', 'ct', 'nonce'])
  })
})

describe('relay repo: deleteRun (account-scoped transactional delete)', () => {
  let db: PrismaClient
  let accountId: string

  beforeEach(async () => {
    db = await createPgliteDb()
    const account = await db.account.create({ data: { publicKey: 'pk' } })
    accountId = account.id
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'enc' } })
  })

  it('deletes the run + its events for the owner and reports it deleted', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage())
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, ts: 2000 }))

    const deleted = await deleteRun(db, accountId, 'r1')

    expect(deleted).toBe(true)
    expect(await getRun(db, 'r1')).toBeNull()
    expect(await db.runEvent.count({ where: { runId: 'r1' } })).toBe(0)
  })

  it('does not delete a run owned by another account and reports false', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage())

    const deleted = await deleteRun(db, 'other-account', 'r1')

    expect(deleted).toBe(false)
    expect(await getRun(db, 'r1')).not.toBeNull()
    expect(await db.runEvent.count({ where: { runId: 'r1' } })).toBe(1)
  })

  it('reports false for an unknown run id', async () => {
    expect(await deleteRun(db, accountId, 'does-not-exist')).toBe(false)
  })
})

describe('relay repo: deleteMachine (account-scoped cascade delete)', () => {
  let db: PrismaClient
  let accountId: string

  beforeEach(async () => {
    db = await createPgliteDb()
    const account = await db.account.create({ data: { publicKey: 'pk' } })
    accountId = account.id
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'enc' } })
  })

  it('deletes the machine and cascades its runs + events (RunEvent -> Run -> Machine)', async () => {
    // Two runs on m1, each with events, to prove the full cascade order.
    await ingestWireMessage(db, accountId, 'claude', wireMessage())
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ seq: 1, ts: 2000 }))
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ runId: 'r2', seq: 0, ts: 3000 }))

    const deleted = await deleteMachine(db, accountId, 'm1')

    expect(deleted).toBe(true)
    expect(await db.machine.count({ where: { id: 'm1' } })).toBe(0)
    expect(await db.run.count({ where: { machineId: 'm1' } })).toBe(0)
    expect(await getRun(db, 'r1')).toBeNull()
    expect(await getRun(db, 'r2')).toBeNull()
    expect(await db.runEvent.count({ where: { runId: 'r1' } })).toBe(0)
    expect(await db.runEvent.count({ where: { runId: 'r2' } })).toBe(0)
  })

  it('cascades even for an online machine with live (non-terminal) runs', async () => {
    // Manual delete is a deliberate owner action: presence/online and live runs
    // do not block it (mirrors deleteRun, which deletes regardless of state).
    await db.machine.update({ where: { id: 'm1' }, data: { online: true, presence: 'online' } })
    await ingestWireMessage(db, accountId, 'claude', wireMessage({ state: 'thinking' }))

    expect(await deleteMachine(db, accountId, 'm1')).toBe(true)
    expect(await db.machine.count({ where: { id: 'm1' } })).toBe(0)
    expect(await getRun(db, 'r1')).toBeNull()
  })

  it('deletes a machine that owns no runs', async () => {
    expect(await deleteMachine(db, accountId, 'm1')).toBe(true)
    expect(await db.machine.count({ where: { id: 'm1' } })).toBe(0)
  })

  it('does not delete a machine owned by another account and reports false', async () => {
    await ingestWireMessage(db, accountId, 'claude', wireMessage())

    const deleted = await deleteMachine(db, 'other-account', 'm1')

    expect(deleted).toBe(false)
    expect(await db.machine.count({ where: { id: 'm1' } })).toBe(1)
    expect(await getRun(db, 'r1')).not.toBeNull()
    expect(await db.runEvent.count({ where: { runId: 'r1' } })).toBe(1)
  })

  it('reports false for an unknown machine id', async () => {
    expect(await deleteMachine(db, accountId, 'does-not-exist')).toBe(false)
  })
})

describe('relay repo: auto-provisions Account + Machine on first ingest', () => {
  it('ingest on a fresh DB (no pre-seed) creates the Account, Machine, and Run', async () => {
    // A first-time linxd connect must not FK-fail: ingest provisions the
    // Account (by id) and Machine (header.machineId, linked to the account)
    // before the Run upsert. No Account/Machine pre-seeded here.
    const db = await createPgliteDb()
    const accountId = 'acct-fresh'
    const msg = wireMessage({ machineId: 'm-new', runId: 'r-new' })

    await ingestWireMessage(db, accountId, 'claude', msg)

    const account = await db.account.findUnique({ where: { id: accountId } })
    expect(account?.id).toBe(accountId)

    const machine = await db.machine.findUnique({ where: { id: 'm-new' } })
    expect(machine?.id).toBe('m-new')
    expect(machine?.accountId).toBe(accountId)

    const run = await getRun(db, 'r-new')
    expect(run?.machineId).toBe('m-new')
    expect(run?.accountId).toBe(accountId)
  })
})
