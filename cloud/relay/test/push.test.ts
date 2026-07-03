import { describe, it, expect, beforeEach } from 'vitest'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db.js'
import {
  subscribePush,
  unsubscribePush,
  listPushSubscriptions,
  deliverRunNotification,
  notifyTypeForHeader,
  loadVapidConfig,
  PushSendError,
  WEB_PUSH_PLATFORM,
  type PushSubscriptionJson,
  type ThinPushPayload,
  type WebPushSender,
} from '../src/push.js'

const SECRET_AGENT_TEXT = 'rm -rf / please approve this destructive command'

function sub(
  endpoint: string,
  p256dh = 'p-' + endpoint,
  auth = 'a-' + endpoint,
): PushSubscriptionJson {
  return { endpoint, keys: { p256dh, auth } }
}

/** A mock web-push sender that records every (endpoint, payload) it is asked to
 *  send, and can be told to fail specific endpoints with a given status code. */
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

describe('push: notify-worthy header mapping', () => {
  it('fires approval off the header eventKind, per approval-request event', () => {
    // The approval signal is the per-event eventKind — independent of state, so
    // it is race-free and fires for each approval request.
    expect(notifyTypeForHeader('thinking', 'approval-request', 'awaiting-approval')).toBe(
      'approval',
    )
    // A second approval-request while already awaiting-approval still notifies.
    expect(notifyTypeForHeader('awaiting-approval', 'approval-request', 'awaiting-approval')).toBe(
      'approval',
    )
    // A non-approval event landing in awaiting-approval is NOT a notify.
    expect(notifyTypeForHeader('thinking', 'delta', 'awaiting-approval')).toBeNull()
  })

  it('fires done/error only on the transition into the terminal state', () => {
    // Entering the terminal state notifies (incl. a brand-new run, prev = null).
    expect(notifyTypeForHeader('thinking', 'delta', 'done')).toBe('done')
    expect(notifyTypeForHeader(null, 'message', 'error')).toBe('error')
    // A repeated terminal header (same state) does NOT re-notify.
    expect(notifyTypeForHeader('done', 'delta', 'done')).toBeNull()
    expect(notifyTypeForHeader('error', 'delta', 'error')).toBeNull()
  })

  it('is silent for ordinary in-progress events', () => {
    expect(notifyTypeForHeader('idle', 'delta', 'thinking')).toBeNull()
    expect(notifyTypeForHeader(null, 'message', 'idle')).toBeNull()
    expect(notifyTypeForHeader('thinking', 'delta', 'blocked')).toBeNull()
  })
})

describe('push: loadVapidConfig', () => {
  it('returns null unless all three vars are present', () => {
    expect(loadVapidConfig({})).toBeNull()
    expect(loadVapidConfig({ RELAY_VAPID_PUBLIC_KEY: 'pub' })).toBeNull()
    expect(
      loadVapidConfig({ RELAY_VAPID_PUBLIC_KEY: 'pub', RELAY_VAPID_PRIVATE_KEY: 'priv' }),
    ).toBeNull()
  })

  it('returns the config when all three are present', () => {
    expect(
      loadVapidConfig({
        RELAY_VAPID_PUBLIC_KEY: 'pub',
        RELAY_VAPID_PRIVATE_KEY: 'priv',
        RELAY_VAPID_SUBJECT: 'mailto:a@b.c',
      }),
    ).toEqual({ publicKey: 'pub', privateKey: 'priv', subject: 'mailto:a@b.c' })
  })
})

describe('push: subscription persistence', () => {
  let db: PrismaClient
  let accountId: string

  beforeEach(async () => {
    db = await createPgliteDb()
    const account = await db.account.create({ data: { publicKey: 'pk-' + Math.random() } })
    accountId = account.id
  })

  it('subscribe persists a PushSubscription and listPushSubscriptions reads it back', async () => {
    await subscribePush(db, accountId, sub('https://push.example/a'))
    const subs = await listPushSubscriptions(db, accountId)
    expect(subs).toEqual([
      {
        endpoint: 'https://push.example/a',
        keys: { p256dh: 'p-https://push.example/a', auth: 'a-https://push.example/a' },
      },
    ])
  })

  it('subscribe is idempotent by endpoint — re-subscribing the same endpoint upserts (1 row), refreshing keys', async () => {
    await subscribePush(db, accountId, sub('https://push.example/a', 'p1', 'a1'))
    await subscribePush(db, accountId, sub('https://push.example/a', 'p2', 'a2'))

    const rows = await db.pushToken.findMany({ where: { accountId } })
    expect(rows).toHaveLength(1)
    expect(rows[0]?.keysP256dh).toBe('p2')
    expect(rows[0]?.keysAuth).toBe('a2')
    expect(rows[0]?.platform).toBe(WEB_PUSH_PLATFORM)
  })

  it('unsubscribe removes a subscription by endpoint', async () => {
    await subscribePush(db, accountId, sub('https://push.example/a'))
    await subscribePush(db, accountId, sub('https://push.example/b'))

    expect(await unsubscribePush(db, accountId, 'https://push.example/a')).toBe(true)
    const left = await listPushSubscriptions(db, accountId)
    expect(left.map((s) => s.endpoint)).toEqual(['https://push.example/b'])
  })

  it('unsubscribe reports false for an unknown endpoint', async () => {
    expect(await unsubscribePush(db, accountId, 'https://push.example/nope')).toBe(false)
  })

  it('listPushSubscriptions skips rows missing keys (unsendable)', async () => {
    // A legacy/foreign-platform row with no keys must not surface as a subscription.
    await db.pushToken.create({
      data: { accountId, token: 'legacy-token', platform: 'apns' },
    })
    await subscribePush(db, accountId, sub('https://push.example/a'))
    const subs = await listPushSubscriptions(db, accountId)
    expect(subs.map((s) => s.endpoint)).toEqual(['https://push.example/a'])
  })
})

describe('push: delivery (mocked sender)', () => {
  let db: PrismaClient
  let accountId: string

  beforeEach(async () => {
    db = await createPgliteDb()
    const account = await db.account.create({ data: { publicKey: 'pk-' + Math.random() } })
    accountId = account.id
  })

  const payload = (over: Partial<ThinPushPayload> = {}): ThinPushPayload => ({
    type: 'approval',
    runId: 'r1',
    machineId: 'm1',
    agentKind: 'claude',
    ...over,
  })

  it('sends a THIN payload to every one of the account subscriptions', async () => {
    await subscribePush(db, accountId, sub('https://push.example/a'))
    await subscribePush(db, accountId, sub('https://push.example/b'))
    const sender = mockSender()

    const result = await deliverRunNotification(db, sender, accountId, payload())

    expect(result).toEqual({ sent: 2, pruned: 0 })
    expect(sender.calls.map((c) => c.endpoint).sort()).toEqual([
      'https://push.example/a',
      'https://push.example/b',
    ])
  })

  it('ZERO-KNOWLEDGE: the payload carries only {type, runId, machineId, agentKind} and no agent text', async () => {
    await subscribePush(db, accountId, sub('https://push.example/a'))
    const sender = mockSender()

    await deliverRunNotification(db, sender, accountId, payload({ type: 'done' }))

    const body = sender.calls[0]?.payload ?? ''
    expect(body).not.toContain(SECRET_AGENT_TEXT)
    const parsed = JSON.parse(body) as Record<string, unknown>
    // The payload shape is EXACTLY the four cleartext operational fields.
    expect(Object.keys(parsed).sort()).toEqual(['agentKind', 'machineId', 'runId', 'type'])
    expect(parsed).toEqual({ type: 'done', runId: 'r1', machineId: 'm1', agentKind: 'claude' })
  })

  it('cross-account isolation: account A delivery never pushes to account B subscriptions', async () => {
    const accountB = (await db.account.create({ data: { publicKey: 'pk-b-' + Math.random() } })).id
    await subscribePush(db, accountId, sub('https://push.example/a-only'))
    await subscribePush(db, accountB, sub('https://push.example/b-only'))
    const sender = mockSender()

    await deliverRunNotification(db, sender, accountId, payload())

    // Only A's endpoint is ever contacted; B's is untouched.
    expect(sender.calls.map((c) => c.endpoint)).toEqual(['https://push.example/a-only'])
  })

  it('prunes dead subscriptions (410 / 404) and keeps the live + transiently-failed ones', async () => {
    await subscribePush(db, accountId, sub('https://push.example/live'))
    await subscribePush(db, accountId, sub('https://push.example/gone-410'))
    await subscribePush(db, accountId, sub('https://push.example/gone-404'))
    await subscribePush(db, accountId, sub('https://push.example/flaky-500'))
    const sender = mockSender({
      'https://push.example/gone-410': 410,
      'https://push.example/gone-404': 404,
      'https://push.example/flaky-500': 500,
    })

    const result = await deliverRunNotification(db, sender, accountId, payload())

    expect(result.sent).toBe(1)
    expect(result.pruned).toBe(2)
    const left = (await listPushSubscriptions(db, accountId)).map((s) => s.endpoint).sort()
    // 410/404 pruned; the live one and the transient 500 are retained.
    expect(left).toEqual(['https://push.example/flaky-500', 'https://push.example/live'])
  })

  it('is a no-op when the account has no subscriptions', async () => {
    const sender = mockSender()
    const result = await deliverRunNotification(db, sender, accountId, payload())
    expect(result).toEqual({ sent: 0, pruned: 0 })
    expect(sender.calls).toHaveLength(0)
  })
})
