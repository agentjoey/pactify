import { describe, it, expect, beforeEach } from 'vitest'
import { ed25519 } from '@noble/curves/ed25519.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import type { PrismaClient } from '@prisma/client'
import { MachineInfo, PairStatus, RunSummary } from '@pactify/wire'
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
    status: res.statusCode,
  }
}

describe('relay http', () => {
  it('GET /healthz returns 200 { ok: true } without auth', async () => {
    const app = createServer({ db, secret: SECRET })
    const res = await app.inject({ method: 'GET', url: '/healthz' })
    expect(res.statusCode).toBe(200)
    expect(res.json()).toEqual({ ok: true })
  })

  it('answers the CORS preflight + reflects the origin so the web can call cross-origin', async () => {
    const app = createServer({ db, secret: SECRET })
    const origin = 'https://pactify-linx-linx-web.vercel.app'
    const pre = await app.inject({
      method: 'OPTIONS',
      url: '/v1/auth',
      headers: {
        origin,
        'access-control-request-method': 'POST',
        'access-control-request-headers': 'content-type,authorization',
      },
    })
    expect(pre.statusCode).toBeLessThan(300)
    expect(pre.headers['access-control-allow-origin']).toBe(origin)
    const get = await app.inject({ method: 'GET', url: '/healthz', headers: { origin } })
    expect(get.headers['access-control-allow-origin']).toBe(origin)
  })

  it('POST /v1/auth issues a token for a valid challenge, 401 for a bad one', async () => {
    const app = createServer({ db, secret: SECRET })
    const { status, token } = await login(app)
    expect(status).toBe(200)
    expect(typeof token).toBe('string')
    const kp = keypair()
    const bad = await app.inject({
      method: 'POST',
      url: '/v1/auth',
      payload: { publicKey: kp.pubHex, challenge: 'c1', signature: 'deadbeef' },
    })
    expect(bad.statusCode).toBe(401)
  })

  it("GET /v1/runs requires a token and returns only the account's runs", async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })

    const noAuth = await app.inject({ method: 'GET', url: '/v1/runs' })
    expect(noAuth.statusCode).toBe(401)

    const res = await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(res.statusCode).toBe(200)
    const runs = res.json() as Array<{ id: string }>
    expect(runs.map((r) => r.id)).toEqual(['r1'])
  })

  it('GET /v1/runs returns a full RunSummary (tokens/pendingApprovals/costMicros) that parses', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: {
        id: 'r1',
        accountId,
        machineId: 'm1',
        agentKind: 'claude',
        state: 'thinking',
        pendingApprovals: 2,
        tokensIn: 11,
        tokensOut: 22,
        costMicros: 333,
        seq: 5,
      },
    })
    const res = await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(res.statusCode).toBe(200)
    const parsed = RunSummary.array().parse(res.json())
    expect(parsed).toHaveLength(1)
    const s = parsed[0]!
    expect(s.id).toBe('r1')
    expect(s.agentKind).toBe('claude')
    expect(s.state).toBe('thinking')
    expect(s.pendingApprovals).toBe(2)
    expect(s.tokensIn).toBe(11)
    expect(s.tokensOut).toBe(22)
    expect(s.costMicros).toBe(333)
    expect(s.machineId).toBe('m1')
    expect(s.seq).toBe(5)
    expect(s.model).toBeUndefined()
    expect(typeof s.updatedAt).toBe('number')
  })

  it('GET /v1/inbox returns runs awaiting approval', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: {
        id: 'r1',
        accountId,
        machineId: 'm1',
        agentKind: 'claude',
        state: 'thinking',
        pendingApprovals: 0,
      },
    })
    await db.run.create({
      data: {
        id: 'r2',
        accountId,
        machineId: 'm1',
        agentKind: 'claude',
        state: 'awaiting-approval',
        pendingApprovals: 1,
      },
    })
    const res = await app.inject({
      method: 'GET',
      url: '/v1/inbox',
      headers: { authorization: `Bearer ${token}` },
    })
    expect((res.json() as Array<{ id: string }>).map((r) => r.id)).toEqual(['r2'])
  })

  it('GET /v1/runs/:id/events?after_seq= returns ciphertext events after seq, only for the owner', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })
    for (const seq of [0, 1, 2]) {
      await db.runEvent.create({
        data: {
          runId: 'r1',
          seq,
          state: 'thinking',
          eventKind: 'message',
          ts: BigInt(1000 + seq),
          bodyEnc: `b${seq}`,
        },
      })
    }
    const res = await app.inject({
      method: 'GET',
      url: '/v1/runs/r1/events?after_seq=0',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(res.statusCode).toBe(200)
    const evs = res.json() as Array<{ seq: number; ts: number; bodyEnc: string }>
    expect(evs.map((e) => e.seq)).toEqual([1, 2])
    expect(typeof evs[0]!.ts).toBe('number') // BigInt serialized to number
    // another account cannot read r1
    const other = await login(app)
    const denied = await app.inject({
      method: 'GET',
      url: '/v1/runs/r1/events?after_seq=0',
      headers: { authorization: `Bearer ${other.token}` },
    })
    expect(denied.statusCode).toBe(404)
  })

  it('DELETE /v1/runs/:id deletes the run + events for the owner; 404 foreign; 401 unauth', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'done' },
    })
    for (const seq of [0, 1]) {
      await db.runEvent.create({
        data: {
          runId: 'r1',
          seq,
          state: 'done',
          eventKind: 'message',
          ts: BigInt(seq),
          bodyEnc: 'b',
        },
      })
    }

    // unauthenticated → 401, run untouched
    const noAuth = await app.inject({ method: 'DELETE', url: '/v1/runs/r1' })
    expect(noAuth.statusCode).toBe(401)
    expect(await db.run.findUnique({ where: { id: 'r1' } })).not.toBeNull()

    // a foreign account → 404, run NOT deleted
    const other = await login(app)
    const denied = await app.inject({
      method: 'DELETE',
      url: '/v1/runs/r1',
      headers: { authorization: `Bearer ${other.token}` },
    })
    expect(denied.statusCode).toBe(404)
    expect(await db.run.findUnique({ where: { id: 'r1' } })).not.toBeNull()
    expect(await db.runEvent.count({ where: { runId: 'r1' } })).toBe(2)

    // the owner → 204, run + events gone, absent from listings
    const ok = await app.inject({
      method: 'DELETE',
      url: '/v1/runs/r1',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(ok.statusCode).toBe(204)
    expect(await db.run.findUnique({ where: { id: 'r1' } })).toBeNull()
    expect(await db.runEvent.count({ where: { runId: 'r1' } })).toBe(0)

    const list = await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: { authorization: `Bearer ${token}` },
    })
    expect((list.json() as Array<{ id: string }>).some((r) => r.id === 'r1')).toBe(false)

    const events = await app.inject({
      method: 'GET',
      url: '/v1/runs/r1/events',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(events.statusCode).toBe(404)
  })

  it('GET /v1/machines requires auth and returns only the account machines', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    const other = await login(app)
    await db.machine.create({
      data: {
        id: 'm1',
        accountId,
        metadataEnc: 'x',
        host: 'host1',
        agentKinds: ['claude'],
        workdirs: ['/tmp'],
        online: true,
        lastSeenAt: 1000n,
      },
    })
    await db.machine.create({
      data: {
        id: 'm2',
        accountId: other.accountId,
        metadataEnc: 'x',
        host: 'host2',
        agentKinds: ['opencode'],
        online: false,
        lastSeenAt: 2000n,
      },
    })

    const noAuth = await app.inject({ method: 'GET', url: '/v1/machines' })
    expect(noAuth.statusCode).toBe(401)

    const res = await app.inject({
      method: 'GET',
      url: '/v1/machines',
      headers: { authorization: `Bearer ${token}` },
    })
    expect(res.statusCode).toBe(200)
    const parsed = MachineInfo.array().parse(res.json())
    expect(parsed).toHaveLength(1)
    expect(parsed[0]).toMatchObject({
      machineId: 'm1',
      host: 'host1',
      agentKinds: ['claude'],
      workdirs: ['/tmp'],
      online: true,
      lastSeenAt: 1000,
    })
  })

  it('POST /v1/pair/init returns an 8-char code and GET exposes epkWeb', async () => {
    const app = createServer({ db, secret: SECRET })
    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { epkWeb: 'web-pub' },
    })
    expect(init.statusCode).toBe(200)
    const { code } = init.json() as { code: string }
    expect(typeof code).toBe('string')
    expect(code).toHaveLength(8)

    const get = await app.inject({ method: 'GET', url: `/v1/pair/${code}` })
    expect(get.statusCode).toBe(200)
    expect(get.json()).toEqual({ mode: 'receive', epkWeb: 'web-pub' })

    const status = await app.inject({ method: 'GET', url: `/v1/pair/${code}/status` })
    expect(status.statusCode).toBe(200)
    const parsed = PairStatus.parse(status.json())
    expect(parsed.state).toBe('pending')
  })

  it('pairing can be completed by an authed machine and polled by the web', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })

    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { epkWeb: 'web-pub' },
    })
    const { code } = init.json() as { code: string }

    const complete = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code}/complete`,
      headers: { authorization: `Bearer ${token}` },
      payload: { epkMachine: 'machine-pub', ciphertext: 'secret-ct' },
    })
    expect(complete.statusCode).toBe(200)

    const status = await app.inject({ method: 'GET', url: `/v1/pair/${code}/status` })
    expect(status.statusCode).toBe(200)
    const parsed = PairStatus.parse(status.json())
    expect(parsed.state).toBe('completed')
    expect(parsed.payload).toEqual({ epkMachine: 'machine-pub', ciphertext: 'secret-ct' })
  })

  it('pair complete rejects expired, used, and unknown codes', async () => {
    let t = 0
    const app = createServer({ db, secret: SECRET, now: () => t })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })

    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { epkWeb: 'web-pub' },
    })
    const { code } = init.json() as { code: string }

    // Unknown code.
    const unknown = await app.inject({
      method: 'POST',
      url: '/v1/pair/nosuchcode/complete',
      headers: { authorization: `Bearer ${token}` },
      payload: { epkMachine: 'm', ciphertext: 'c' },
    })
    expect(unknown.statusCode).toBe(404)

    // Expired code (TTL elapsed).
    t += 6 * 60 * 1000
    const expired = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code}/complete`,
      headers: { authorization: `Bearer ${token}` },
      payload: { epkMachine: 'm', ciphertext: 'c' },
    })
    expect(expired.statusCode).toBe(410)

    // Used code.
    const fresh = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { epkWeb: 'web-pub-2' },
    })
    const code2 = (fresh.json() as { code: string }).code
    const first = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code2}/complete`,
      headers: { authorization: `Bearer ${token}` },
      payload: { epkMachine: 'm', ciphertext: 'c' },
    })
    expect(first.statusCode).toBe(200)
    const second = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code2}/complete`,
      headers: { authorization: `Bearer ${token}` },
      payload: { epkMachine: 'm', ciphertext: 'c' },
    })
    expect(second.statusCode).toBe(410)
  })

  it('POST /v1/pair/init is rate-limited per IP (under the /v1/* reads bucket)', async () => {
    // pair/init is part of the per-IP /v1/* reads surface; drive the limit low.
    const app = createServer({ db, secret: SECRET, rateLimits: { readsPerMin: 10 } })
    const results: number[] = []
    for (let i = 0; i < 12; i++) {
      const res = await app.inject({
        method: 'POST',
        url: '/v1/pair/init',
        payload: { epkWeb: `epk-${i}` },
      })
      results.push(res.statusCode)
    }
    expect(results.filter((s) => s === 200).length).toBeLessThanOrEqual(10)
    expect(results.some((s) => s === 429)).toBe(true)
  })

  it('provision: init→ready→complete drives the status state machine', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })

    // 1. The (authenticated) web opens a provision session — no epk yet.
    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { mode: 'provision' },
    })
    expect(init.statusCode).toBe(200)
    const { code } = init.json() as { code: string }

    // GET reflects the mode and exposes no epkWeb in provision.
    const got = await app.inject({ method: 'GET', url: `/v1/pair/${code}` })
    expect(got.json()).toEqual({ mode: 'provision' })

    // Initially pending.
    let status = PairStatus.parse(
      (await app.inject({ method: 'GET', url: `/v1/pair/${code}/status` })).json(),
    )
    expect(status.state).toBe('pending')

    // 2. The secret-less machine publishes its ephemeral key (UNAUTH).
    const ready = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code}/ready`,
      payload: { epkMachine: 'machine-pub' },
    })
    expect(ready.statusCode).toBe(200)

    status = PairStatus.parse(
      (await app.inject({ method: 'GET', url: `/v1/pair/${code}/status` })).json(),
    )
    expect(status.state).toBe('ready')
    expect(status.epkMachine).toBe('machine-pub')

    // 3. The authenticated web wraps the secret and completes.
    const complete = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code}/complete`,
      headers: { authorization: `Bearer ${token}` },
      payload: { epkMachine: 'web-responder-pub', ciphertext: 'wrapped-secret' },
    })
    expect(complete.statusCode).toBe(200)

    status = PairStatus.parse(
      (await app.inject({ method: 'GET', url: `/v1/pair/${code}/status` })).json(),
    )
    expect(status.state).toBe('completed')
    expect(status.payload).toEqual({
      epkMachine: 'web-responder-pub',
      ciphertext: 'wrapped-secret',
    })
  })

  it('provision: ready requires auth-free access but complete stays authenticated', async () => {
    const app = createServer({ db, secret: SECRET })
    await login(app)
    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { mode: 'provision' },
    })
    const { code } = init.json() as { code: string }

    // ready needs NO bearer token (the machine has no secret yet).
    const ready = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code}/ready`,
      payload: { epkMachine: 'machine-pub' },
    })
    expect(ready.statusCode).toBe(200)

    // complete WITHOUT a token is rejected — only the secret-holder may send it.
    const unauth = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code}/complete`,
      payload: { epkMachine: 'web-pub', ciphertext: 'ct' },
    })
    expect(unauth.statusCode).toBe(401)
  })

  it('provision: ready rejects unknown, expired, and non-pending codes', async () => {
    let t = 0
    const app = createServer({ db, secret: SECRET, now: () => t })

    // Unknown code.
    const unknown = await app.inject({
      method: 'POST',
      url: '/v1/pair/nope/ready',
      payload: { epkMachine: 'm' },
    })
    expect(unknown.statusCode).toBe(404)

    // Open a provision session.
    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { mode: 'provision' },
    })
    const { code } = init.json() as { code: string }

    // First ready succeeds; a second is single-use → gone.
    expect(
      (
        await app.inject({
          method: 'POST',
          url: `/v1/pair/${code}/ready`,
          payload: { epkMachine: 'm1' },
        })
      ).statusCode,
    ).toBe(200)
    expect(
      (
        await app.inject({
          method: 'POST',
          url: `/v1/pair/${code}/ready`,
          payload: { epkMachine: 'm2' },
        })
      ).statusCode,
    ).toBe(410)

    // A fresh provision session, then let it expire → gone.
    const init2 = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { mode: 'provision' },
    })
    const code2 = (init2.json() as { code: string }).code
    t += 6 * 60 * 1000
    expect(
      (
        await app.inject({
          method: 'POST',
          url: `/v1/pair/${code2}/ready`,
          payload: { epkMachine: 'm' },
        })
      ).statusCode,
    ).toBe(410)
  })

  it('ready refuses receive-mode sessions (provision-only)', async () => {
    const app = createServer({ db, secret: SECRET })
    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { epkWeb: 'web-pub' },
    })
    const { code } = init.json() as { code: string }
    const ready = await app.inject({
      method: 'POST',
      url: `/v1/pair/${code}/ready`,
      payload: { epkMachine: 'machine-pub' },
    })
    // receive sessions never advance to ready.
    expect(ready.statusCode).toBe(410)
  })

  it('POST /v1/runs/:id/rename sets Run.title and RunSummary carries it', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })

    const rename = await app.inject({
      method: 'POST',
      url: '/v1/runs/r1/rename',
      headers: { authorization: `Bearer ${token}`, 'content-type': 'application/json' },
      payload: { title: 'My Run' },
    })
    expect(rename.statusCode).toBe(200)
    expect(rename.json()).toEqual({ ok: true })

    const list = await app.inject({
      method: 'GET',
      url: '/v1/runs',
      headers: { authorization: `Bearer ${token}` },
    })
    const parsed = RunSummary.array().parse(list.json())
    expect(parsed).toHaveLength(1)
    expect(parsed[0]!.title).toBe('My Run')
  })

  it('POST /v1/runs/:id/rename requires auth and rejects foreign run', async () => {
    const app = createServer({ db, secret: SECRET })
    const { accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })

    const noAuth = await app.inject({
      method: 'POST',
      url: '/v1/runs/r1/rename',
      payload: { title: 'No' },
    })
    expect(noAuth.statusCode).toBe(401)

    const other = await login(app)
    const foreign = await app.inject({
      method: 'POST',
      url: '/v1/runs/r1/rename',
      headers: { authorization: `Bearer ${other.token}`, 'content-type': 'application/json' },
      payload: { title: 'Intrusion' },
    })
    expect(foreign.statusCode).toBe(404)
  })

  it('POST /v1/runs/:id/rename rejects empty or over-200-char titles', async () => {
    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })
    await db.run.create({
      data: { id: 'r1', accountId, machineId: 'm1', agentKind: 'claude', state: 'thinking' },
    })

    const auth = () => ({
      authorization: `Bearer ${token}`,
      'content-type': 'application/json',
    })

    const empty = await app.inject({
      method: 'POST',
      url: '/v1/runs/r1/rename',
      headers: auth(),
      payload: { title: '' },
    })
    expect(empty.statusCode).toBe(400)

    const long = await app.inject({
      method: 'POST',
      url: '/v1/runs/r1/rename',
      headers: auth(),
      payload: { title: 'x'.repeat(201) },
    })
    expect(long.statusCode).toBe(400)
  })

  it('zero-knowledge: the relay surface only ever carries public keys + ciphertext', async () => {
    // The relay is a blind courier: across the whole provision flow it sees only
    // public keys (epkWeb/epkMachine) and an opaque ciphertext blob. A sentinel
    // "plaintext" is never handed to it, so it can appear in NO response — and
    // the relay has no field in which to lodge one. (The real ECDH round-trip is
    // proven in @pactify/core and @pactify/linx-cli, which depend on the crypto.)
    const PLAINTEXT = 'PLAINTEXT-MASTER-SECRET-MUST-NEVER-LEAK'
    const epkMachine = 'machine-ephemeral-pubkey'
    const epkWebResponder = 'web-responder-ephemeral-pubkey'
    const ciphertext = 'opaque-xchacha20poly1305-blob'

    const app = createServer({ db, secret: SECRET })
    const { token, accountId } = await login(app)
    await db.machine.create({ data: { id: 'm1', accountId, metadataEnc: 'x' } })

    const responses: string[] = []
    const record = (r: { body: string }) => responses.push(r.body)

    const init = await app.inject({
      method: 'POST',
      url: '/v1/pair/init',
      payload: { mode: 'provision' },
    })
    record(init)
    const { code } = init.json() as { code: string }

    record(await app.inject({ method: 'GET', url: `/v1/pair/${code}` }))
    record(
      await app.inject({
        method: 'POST',
        url: `/v1/pair/${code}/ready`,
        payload: { epkMachine },
      }),
    )
    record(await app.inject({ method: 'GET', url: `/v1/pair/${code}/status` }))
    record(
      await app.inject({
        method: 'POST',
        url: `/v1/pair/${code}/complete`,
        headers: { authorization: `Bearer ${token}` },
        payload: { epkMachine: epkWebResponder, ciphertext },
      }),
    )

    const finalStatus = await app.inject({ method: 'GET', url: `/v1/pair/${code}/status` })
    record(finalStatus)

    // Only public keys + ciphertext are couriered back...
    const parsed = PairStatus.parse(finalStatus.json())
    expect(parsed.payload).toEqual({ epkMachine: epkWebResponder, ciphertext })

    // ...and no relay response carries the plaintext (there is no field for it).
    for (const body of responses) {
      expect(body).not.toContain(PLAINTEXT)
    }
  })
})
