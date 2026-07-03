import { describe, it, expect } from 'vitest'
import { PairStore } from '../src/pair'

describe('PairStore provision state machine', () => {
  it('walks pending → ready → completed and returns the wrapped payload', () => {
    const store = new PairStore()
    const code = store.init('provision')
    expect(store.get(code)).toEqual({ mode: 'provision' })
    expect(store.exists(code)).toBe('pending')
    expect(store.status(code)).toEqual({ state: 'pending' })

    expect(store.ready(code, 'epk-machine')).toBe(true)
    expect(store.exists(code)).toBe('ready')
    expect(store.status(code)).toEqual({ state: 'ready', epkMachine: 'epk-machine' })

    expect(store.complete(code, { epkMachine: 'epk-web', ciphertext: 'ct' })).toBe(true)
    expect(store.exists(code)).toBe('completed')
    expect(store.status(code)).toEqual({
      state: 'completed',
      payload: { epkMachine: 'epk-web', ciphertext: 'ct' },
    })
  })

  it('ready is single-use and provision-only', () => {
    const store = new PairStore()
    const provision = store.init('provision')
    expect(store.ready(provision, 'a')).toBe(true)
    expect(store.ready(provision, 'b')).toBe(false)

    const receive = store.init('receive', 'epk-web')
    expect(store.ready(receive, 'a')).toBe(false)
    expect(store.get(receive)).toEqual({ mode: 'receive', epkWeb: 'epk-web' })
  })

  it('zero-knowledge: a stored entry holds only public keys + ciphertext, no secret field', () => {
    // A sentinel plaintext is never handed to the store — and there is no field
    // in which to lodge one. Only public keys + an opaque ciphertext are stored.
    const PLAINTEXT = 'PLAINTEXT-MASTER-SECRET-MUST-NEVER-LEAK'
    const store = new PairStore()
    const code = store.init('provision')
    store.ready(code, 'machine-ephemeral-pubkey')
    store.complete(code, {
      epkMachine: 'web-responder-ephemeral-pubkey',
      ciphertext: 'opaque-xchacha20poly1305-blob',
    })

    const entry = (store as unknown as { codes: Map<string, Record<string, unknown>> }).codes.get(
      code,
    )!
    // The only keys present are the courier fields — no plaintext channel exists.
    expect(new Set(Object.keys(entry))).toEqual(
      new Set(['mode', 'epkMachine', 'payload', 'expiresAt']),
    )
    expect(JSON.stringify(entry)).not.toContain(PLAINTEXT)
  })

  it('expires entries past the TTL', () => {
    let t = 0
    const store = new PairStore(() => t, 1000)
    const code = store.init('provision')
    expect(store.exists(code)).toBe('pending')
    t += 1001
    expect(store.exists(code)).toBe('expired')
    expect(store.get(code)).toBeNull()
    expect(store.status(code)).toBeNull()
    expect(store.ready(code, 'm')).toBe(false)
  })
})
