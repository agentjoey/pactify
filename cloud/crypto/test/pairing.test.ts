import { describe, it, expect } from 'vitest'
import { base64url } from '@scure/base'
import { createPairingInitiator, pairingRespond } from '../src/pairing'

describe('relay-blind pairing', () => {
  it('round-trips the master secret (web ↔ machine)', () => {
    const masterSecret = new Uint8Array(32).map((_, i) => (i * 7 + 3) & 0xff)
    const init = createPairingInitiator()
    const resp = pairingRespond(masterSecret, init.epkWebPub)
    const recovered = init.complete(resp.epkMachinePub, resp.ciphertext)
    expect(recovered).toEqual(masterSecret)
  })

  it('round-trips with the roles reversed (machine-initiator + web-responder)', () => {
    // Add-a-machine: the secret-less machine is the INITIATOR and the
    // secret-holding web is the RESPONDER. The same symmetric ECDH primitives
    // work just by swapping who calls which — this guards that assumption.
    const masterSecret = new Uint8Array(32).map((_, i) => (i * 11 + 5) & 0xff)
    // Machine generates its ephemeral key and publishes it (via `ready`).
    const machine = createPairingInitiator()
    // Web wraps its current secret to the machine's ephemeral key.
    const webResp = pairingRespond(masterSecret, machine.epkWebPub)
    // Machine decrypts with the web responder's ephemeral key + ciphertext.
    const recovered = machine.complete(webResp.epkMachinePub, webResp.ciphertext)
    expect(recovered).toEqual(masterSecret)
  })

  it('emits hex public keys and a base64url(nonce||ct) ciphertext', () => {
    const init = createPairingInitiator()
    expect(init.epkWebPub).toMatch(/^[0-9a-f]{64}$/)
    const resp = pairingRespond(new Uint8Array(32), init.epkWebPub)
    expect(resp.epkMachinePub).toMatch(/^[0-9a-f]{64}$/)
    const raw = base64url.decode(resp.ciphertext)
    // 24-byte nonce + ciphertext (>= secret + 16-byte poly1305 tag)
    expect(raw.length).toBeGreaterThanOrEqual(24 + 32 + 16)
  })

  it('throws on a tampered ciphertext', () => {
    const masterSecret = new Uint8Array(32).fill(9)
    const init = createPairingInitiator()
    const resp = pairingRespond(masterSecret, init.epkWebPub)
    const raw = base64url.decode(resp.ciphertext)
    raw[raw.length - 1] ^= 0xff
    const tampered = base64url.encode(raw)
    expect(() => init.complete(resp.epkMachinePub, tampered)).toThrow()
  })

  it('fails to decrypt with a wrong machine public key', () => {
    const masterSecret = new Uint8Array(32).fill(5)
    const init = createPairingInitiator()
    const resp = pairingRespond(masterSecret, init.epkWebPub)
    // A different machine's ephemeral public key derives a different shared key.
    const otherResp = pairingRespond(masterSecret, init.epkWebPub)
    expect(() => init.complete(otherResp.epkMachinePub, resp.ciphertext)).toThrow()
  })

  it('produces a fresh epkWebPub and nonce per run', () => {
    const a = createPairingInitiator()
    const b = createPairingInitiator()
    expect(a.epkWebPub).not.toEqual(b.epkWebPub)

    const secret = new Uint8Array(32).fill(1)
    const r1 = pairingRespond(secret, a.epkWebPub)
    const r2 = pairingRespond(secret, a.epkWebPub)
    const n1 = base64url.decode(r1.ciphertext).slice(0, 24)
    const n2 = base64url.decode(r2.ciphertext).slice(0, 24)
    expect(n1).not.toEqual(n2)
  })
})
