import webpush from 'web-push'
import type { PrismaClient } from '@prisma/client'
import type { Logger } from './log.js'

/**
 * Web-push notification delivery for the relay (i-notif-be).
 *
 * ZERO-KNOWLEDGE: a push payload carries ONLY the cleartext operational fields
 * `{ type, runId, machineId, agentKind }` — NEVER decrypted agent text. The web
 * service worker shows a generic, localized notification and deep-links to the
 * run by id; the actual content stays E2E-encrypted and never reaches here.
 */

/** Platform tag stored on a {@link PushToken} row for a browser subscription. */
export const WEB_PUSH_PLATFORM = 'web-push'

/** VAPID config the relay signs web-push requests with, read from env. */
export interface VapidConfig {
  publicKey: string
  privateKey: string
  /** A `mailto:` or URL contact, required by the push services. */
  subject: string
}

/**
 * A standard browser PushSubscription JSON (the subset web-push needs): the
 * `endpoint` plus the `p256dh`/`auth` keys used to encrypt the payload.
 */
export interface PushSubscriptionJson {
  endpoint: string
  keys: { p256dh: string; auth: string }
}

/** The notify-worthy event types carried in a push payload's `type` field. */
export type NotifyType = 'approval' | 'done' | 'error'

/**
 * The THIN push payload — cleartext operational fields only. This is the entire
 * contract the service worker receives; there is deliberately no place for
 * message text, titles, or any decrypted content.
 */
export interface ThinPushPayload {
  type: NotifyType
  runId: string
  machineId: string
  agentKind: string
}

/**
 * Decide the notify-worthy push type for one ingested header, or `null` when it
 * is not worth a notification. Two independent, race-free signals:
 *
 *   - **approval (needs you):** the header's own `eventKind === 'approval-request'`.
 *     This is a per-event signal carried by the header itself, so it is immune to
 *     concurrent re-reads of the run row and fires once per approval request.
 *   - **done / error:** a *transition* of the run's cleartext state into the
 *     terminal `done`/`error` — gated on `nextState !== prevState` (the state
 *     stored BEFORE this header's upsert) so a repeated/duplicate terminal header
 *     does not re-notify. `nextState` is the header's OWN state, never a
 *     re-fetched row, so two concurrent handlers each judge their own header.
 *
 * `prevState` is the run's stored state before this header was applied (`null`
 * for a brand-new run).
 */
export function notifyTypeForHeader(
  prevState: string | null,
  eventKind: string,
  nextState: string,
): NotifyType | null {
  if (eventKind === 'approval-request') return 'approval'
  if (nextState !== prevState) {
    if (nextState === 'done') return 'done'
    if (nextState === 'error') return 'error'
  }
  return null
}

/**
 * The injectable web-push sender. Tests pass a mock so no network is touched;
 * production passes {@link createWebPushSender}. `send` resolves on success and
 * rejects with a {@link PushSendError} (carrying the push service's HTTP status)
 * on failure, so the caller can prune dead subscriptions on 404/410.
 */
export interface WebPushSender {
  send(subscription: PushSubscriptionJson, payload: string): Promise<void>
}

/** A failed web-push send, carrying the push service's HTTP status code. */
export class PushSendError extends Error {
  constructor(
    readonly statusCode: number,
    message?: string,
  ) {
    super(message ?? `web-push failed (${statusCode})`)
    this.name = 'PushSendError'
  }
}

/** HTTP statuses from a push service that mean the subscription is dead. */
export function isDeadSubscriptionStatus(statusCode: number): boolean {
  return statusCode === 404 || statusCode === 410
}

/**
 * Read the VAPID config from the environment (`RELAY_VAPID_PUBLIC_KEY`,
 * `RELAY_VAPID_PRIVATE_KEY`, `RELAY_VAPID_SUBJECT`). Returns `null` when any of
 * the three is missing — the relay then runs with push delivery disabled (a
 * no-op sender), so an unconfigured deploy still boots.
 */
export function loadVapidConfig(env: NodeJS.ProcessEnv = process.env): VapidConfig | null {
  const publicKey = env.RELAY_VAPID_PUBLIC_KEY
  const privateKey = env.RELAY_VAPID_PRIVATE_KEY
  const subject = env.RELAY_VAPID_SUBJECT
  if (!publicKey || !privateKey || !subject) return null
  return { publicKey, privateKey, subject }
}

/**
 * The production web-push sender, wrapping the `web-push` library. VAPID details
 * are passed per-call (no global `setVapidDetails`) so the sender is a pure
 * value with no process-wide mutation. A `WebPushError` from the library is
 * re-thrown as a {@link PushSendError} carrying its `statusCode`.
 */
export function createWebPushSender(vapid: VapidConfig): WebPushSender {
  return {
    async send(subscription, payload) {
      try {
        await webpush.sendNotification(subscription, payload, {
          vapidDetails: {
            subject: vapid.subject,
            publicKey: vapid.publicKey,
            privateKey: vapid.privateKey,
          },
        })
      } catch (err) {
        const status = (err as { statusCode?: unknown }).statusCode
        const code = typeof status === 'number' ? status : 0
        throw new PushSendError(code, err instanceof Error ? err.message : String(err))
      }
    },
  }
}

/**
 * Persist (or refresh) a browser PushSubscription for an account, idempotent by
 * `endpoint`. The endpoint is stored in `PushToken.token`, so the existing
 * `@@unique([accountId, token])` turns a re-subscribe with the same endpoint
 * into an upsert — the keys are refreshed (they can rotate) without creating a
 * duplicate row.
 */
export async function subscribePush(
  db: PrismaClient,
  accountId: string,
  sub: PushSubscriptionJson,
): Promise<void> {
  await db.pushToken.upsert({
    where: { accountId_token: { accountId, token: sub.endpoint } },
    create: {
      accountId,
      token: sub.endpoint,
      platform: WEB_PUSH_PLATFORM,
      keysP256dh: sub.keys.p256dh,
      keysAuth: sub.keys.auth,
    },
    update: {
      platform: WEB_PUSH_PLATFORM,
      keysP256dh: sub.keys.p256dh,
      keysAuth: sub.keys.auth,
    },
  })
}

/**
 * Remove a subscription by endpoint for an account. Returns `true` if a row was
 * removed, `false` if no matching subscription existed (unknown endpoint, or one
 * belonging to another account — the account scoping makes them indistinct).
 */
export async function unsubscribePush(
  db: PrismaClient,
  accountId: string,
  endpoint: string,
): Promise<boolean> {
  const res = await db.pushToken.deleteMany({ where: { accountId, token: endpoint } })
  return res.count > 0
}

/**
 * List an account's web-push subscriptions as PushSubscription JSON. Rows whose
 * keys are missing (e.g. a legacy/foreign-platform row) are skipped so the
 * sender never receives an unsendable subscription.
 */
export async function listPushSubscriptions(
  db: PrismaClient,
  accountId: string,
): Promise<PushSubscriptionJson[]> {
  const rows = await db.pushToken.findMany({
    where: { accountId, platform: WEB_PUSH_PLATFORM },
  })
  const subs: PushSubscriptionJson[] = []
  for (const r of rows) {
    if (!r.keysP256dh || !r.keysAuth) continue
    subs.push({ endpoint: r.token, keys: { p256dh: r.keysP256dh, auth: r.keysAuth } })
  }
  return subs
}

/** Outcome of a {@link deliverRunNotification} fan-out. */
export interface DeliveryResult {
  /** Subscriptions the push service accepted. */
  sent: number
  /** Dead subscriptions (404/410) pruned during this delivery. */
  pruned: number
}

/**
 * Fan a THIN {@link ThinPushPayload} out to all of an account's web-push
 * subscriptions via the injected {@link WebPushSender}. Account-scoped: only
 * `accountId`'s own subscriptions are loaded, so one account's run never pushes
 * to another's devices. Subscriptions the push service reports as dead (404/410)
 * are pruned. Other send failures are logged and left in place (transient).
 *
 * The serialized payload contains ONLY `{ type, runId, machineId, agentKind }` —
 * the zero-knowledge guarantee.
 */
export async function deliverRunNotification(
  db: PrismaClient,
  sender: WebPushSender,
  accountId: string,
  payload: ThinPushPayload,
  log?: Logger,
): Promise<DeliveryResult> {
  return fanOutPush(db, sender, accountId, JSON.stringify(payload), payload.type, log)
}

/**
 * Thin U2 pact push (zero-knowledge): a task in one of the caller's projects
 * needs a human (awaiting review / changes requested). Contains ONLY the
 * cleartext operational ids — no spec/evidence, no decrypted content.
 */
export interface PactPushPayload {
  type: 'pact-needs-you'
  projectId: string
  eventType: string
  task?: string
}

export async function deliverPactNotification(
  db: PrismaClient,
  sender: WebPushSender,
  accountId: string,
  payload: PactPushPayload,
  log?: Logger,
): Promise<DeliveryResult> {
  return fanOutPush(db, sender, accountId, JSON.stringify(payload), payload.type, log)
}

// fanOutPush sends one serialized body to every push subscription of an account,
// pruning subscriptions the push service reports as dead (404/410). `label` is a
// non-sensitive type tag used only for logging a transient failure.
async function fanOutPush(
  db: PrismaClient,
  sender: WebPushSender,
  accountId: string,
  body: string,
  label: string,
  log?: Logger,
): Promise<DeliveryResult> {
  const subs = await listPushSubscriptions(db, accountId)
  let sent = 0
  let pruned = 0
  for (const sub of subs) {
    try {
      await sender.send(sub, body)
      sent++
    } catch (err) {
      const status = err instanceof PushSendError ? err.statusCode : 0
      if (isDeadSubscriptionStatus(status)) {
        await db.pushToken.deleteMany({ where: { accountId, token: sub.endpoint } })
        pruned++
      } else {
        // Transient/unknown failure: keep the subscription, log the type only.
        log?.warn('web-push send failed', { type: label, status })
      }
    }
  }
  return { sent, pruned }
}
