import { describe, it, expect, beforeEach } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import type { PrismaClient } from '@prisma/client'
import { createPgliteDb } from '../src/db'
import { createServer } from '../src/server'
import { subscribePush, type WebPushSender } from '../src/push'
import type { Broadcaster } from '../src/broadcast'

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
  return { token: res.json().token as string, accountId: res.json().accountId as string }
}

/** A Broadcaster that records what would be fanned out to socket clients. */
function recordingBroadcaster(): Broadcaster & { sent: Array<{ accountId: string; event: string; payload: any }> } {
  const sent: Array<{ accountId: string; event: string; payload: any }> = []
  return { sent, emit: (accountId, event, payload) => sent.push({ accountId, event, payload }), bind: () => {} }
}

function ingestBody(over: Record<string, unknown> = {}) {
  return {
    projectId: 'p:pactify',
    name: 'pactify',
    feature: 'cloud-m0',
    eventType: 'assign',
    task: 'm0-wire',
    seq: 0,
    ts: 1000,
    bodyEnc: 'ciphertext',
    ...over,
  }
}

describe('relay pact HTTP (U2 S2)', () => {
  it('POST /v1/pact/ingest persists, returns 202, and broadcasts pact-event', async () => {
    const bc = recordingBroadcaster()
    const app = createServer({ db, secret: SECRET, broadcaster: bc })
    const { token, accountId } = await login(app)

    const res = await app.inject({
      method: 'POST',
      url: '/v1/pact/ingest',
      headers: { authorization: `Bearer ${token}` },
      payload: ingestBody(),
    })
    expect(res.statusCode).toBe(202)

    // Persisted + visible on the board.
    const projects = await app.inject({
      method: 'GET',
      url: '/v1/pact/projects',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(projects.json()).toHaveLength(1)
    expect(projects.json()[0].name).toBe('pactify')

    // Broadcast to the account room.
    expect(bc.sent).toHaveLength(1)
    expect(bc.sent[0]).toMatchObject({ accountId, event: 'pact-event' })
    expect(bc.sent[0].payload).toMatchObject({ projectId: 'p:pactify', eventType: 'assign', seq: 0 })
  })

  it('GET /v1/pact/projects/:id/events returns the event stream; foreign/missing → 404', async () => {
    const app = createServer({ db, secret: SECRET })
    const a = await login(app)
    const b = await login(app) // a second account

    for (const seq of [0, 1, 2]) {
      await app.inject({
        method: 'POST',
        url: '/v1/pact/ingest',
        headers: { authorization: `Bearer ${a.token}` },
        payload: ingestBody({ seq, eventType: seq === 2 ? 'accept' : 'checkpoint', bodyEnc: `c${seq}` }),
      })
    }

    const evs = await app.inject({
      method: 'GET',
      url: '/v1/pact/projects/p:pactify/events',
      headers: { authorization: `Bearer ${a.token}` },
    })
    expect(evs.json().map((e: any) => e.eventType)).toEqual(['checkpoint', 'checkpoint', 'accept'])
    expect(evs.json()[0].ts).toBe(1000) // BigInt serialized to number

    // after_seq catch-up.
    const after = await app.inject({
      method: 'GET',
      url: '/v1/pact/projects/p:pactify/events?after_seq=1',
      headers: { authorization: `Bearer ${a.token}` },
    })
    expect(after.json()).toHaveLength(1)

    // Account b cannot read account a's project events (uniform 404).
    const foreign = await app.inject({
      method: 'GET',
      url: '/v1/pact/projects/p:pactify/events',
      headers: { authorization: `Bearer ${b.token}` },
    })
    expect(foreign.statusCode).toBe(404)
  })

  it('fires a needs-you push on checkpoint/changes_requested, not on other events (S6)', async () => {
    const calls: string[] = []
    const sender: WebPushSender = {
      async send(_sub, payload) {
        calls.push(payload)
      },
    }
    const app = createServer({ db, secret: SECRET, pushSender: sender })
    const { token, accountId } = await login(app)
    await subscribePush(db, accountId, { endpoint: 'https://push.example/a', keys: { p256dh: 'p', auth: 'a' } })

    const post = (over: Record<string, unknown>) =>
      app.inject({
        method: 'POST',
        url: '/v1/pact/ingest',
        headers: { authorization: `Bearer ${token}` },
        payload: ingestBody(over),
      })

    await post({ seq: 0, eventType: 'assign', task: 't1' }) // not a needs-you event
    await post({ seq: 1, eventType: 'checkpoint', task: 't1' }) // needs review → push
    // give the fire-and-forget push a tick
    await new Promise((r) => setTimeout(r, 30))

    expect(calls).toHaveLength(1)
    const p = JSON.parse(calls[0])
    expect(p).toMatchObject({ type: 'pact-needs-you', eventType: 'checkpoint', task: 't1', projectId: 'p:pactify' })
    // zero-knowledge: no decrypted content in the push.
    expect(calls[0]).not.toContain('bodyEnc')

    // Re-uploading the same event (reconnect replay) must NOT re-push (no storm).
    await post({ seq: 1, eventType: 'checkpoint', task: 't1' })
    await new Promise((r) => setTimeout(r, 30))
    expect(calls).toHaveLength(1)
  })

  it('rejects unauthenticated ingest (401) and a malformed body (400)', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token } = await login(app)
    expect((await app.inject({ method: 'POST', url: '/v1/pact/ingest', payload: ingestBody() })).statusCode).toBe(401)
    const bad = await app.inject({
      method: 'POST',
      url: '/v1/pact/ingest',
      headers: { authorization: `Bearer ${token}` },
      payload: { projectId: 'p', seq: -1 }, // missing fields + negative seq
    })
    expect(bad.statusCode).toBe(400)
  })
})

// Security regression — review finding H3 (high).
//
// POST /v1/pact/ingest takes projectId straight from the client body and never
// verifies the caller owns it; PactEvent uniqueness is global per project. So a
// foreign account can (a) inject forged events into a victim's project — surfaced
// on the victim's board because getProjectEvents filters by projectId only — and
// (b) pre-claim a (projectId, seq) so the victim's real event is dropped by
// skipDuplicates. RED until ingest rejects a foreign projectId (and the read is
// scoped by accountId).
describe('relay pact HTTP — cross-tenant ingest isolation (H3)', () => {
  it('a foreign account cannot inject events into another account\'s project', async () => {
    const app = createServer({ db, secret: SECRET })
    const a = await login(app)
    const b = await login(app) // attacker, a different valid account

    const own = await app.inject({
      method: 'POST', url: '/v1/pact/ingest',
      headers: { authorization: `Bearer ${a.token}` },
      payload: ingestBody({ seq: 0, eventType: 'assign', task: 'legit' }),
    })
    expect(own.statusCode).toBe(202) // A creates its project

    // B injects a forged event targeting A's project id.
    const forged = await app.inject({
      method: 'POST', url: '/v1/pact/ingest',
      headers: { authorization: `Bearer ${b.token}` },
      payload: ingestBody({ seq: 1, eventType: 'merge', task: 'FORGED-by-b' }),
    })
    expect(forged.statusCode).not.toBe(202) // cross-tenant ingest must be rejected

    // A's board must never surface B's forged event.
    const evs = await app.inject({
      method: 'GET', url: '/v1/pact/projects/p:pactify/events',
      headers: { authorization: `Bearer ${a.token}` },
    })
    expect(evs.json().map((e: any) => e.task)).not.toContain('FORGED-by-b')
    expect(evs.json()).toHaveLength(1)
  })

  it('a foreign account cannot clobber a seq the owner will use (idempotency)', async () => {
    const app = createServer({ db, secret: SECRET })
    const a = await login(app)
    const b = await login(app)

    await app.inject({
      method: 'POST', url: '/v1/pact/ingest',
      headers: { authorization: `Bearer ${a.token}` },
      payload: ingestBody({ seq: 0 }),
    }) // A creates its project

    // B pre-claims (p:pactify, seq 5) with poison.
    await app.inject({
      method: 'POST', url: '/v1/pact/ingest',
      headers: { authorization: `Bearer ${b.token}` },
      payload: ingestBody({ seq: 5, bodyEnc: 'b-poison' }),
    })

    // A's real event at seq 5 must still land, not be dropped as a duplicate.
    await app.inject({
      method: 'POST', url: '/v1/pact/ingest',
      headers: { authorization: `Bearer ${a.token}` },
      payload: ingestBody({ seq: 5, bodyEnc: 'a-real' }),
    })

    const evs = await app.inject({
      method: 'GET', url: '/v1/pact/projects/p:pactify/events',
      headers: { authorization: `Bearer ${a.token}` },
    })
    const bodies = evs.json().map((e: any) => e.bodyEnc)
    expect(bodies).toContain('a-real') // owner's event survived
    expect(bodies).not.toContain('b-poison') // attacker's poison never on the board
  })
})
