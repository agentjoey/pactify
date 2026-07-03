// Golden vectors — the cross-language compatibility contract.
//
// These pin the EXACT byte outputs of the account/run key derivations and the
// xchacha20poly1305 envelope for fixed inputs. A future Go port (pactify
// internal/cloudauth, per the U0/U1 unified-architecture plan) must reproduce
// these same outputs byte-for-byte, or account identity and E2E decryption
// silently break across languages. The round-trip tests in keys/crypto/pairing
// cannot catch that class of drift — only frozen vectors can.
//
// DO NOT edit an expected value to make a failing test pass. A mismatch means
// the derivation/encryption changed, which is a cross-language BREAK: either
// revert the change, or bump the protocol version and regenerate vectors on
// BOTH sides deliberately.
import { describe, it, expect } from 'vitest'
import { hexToBytes, bytesToHex } from '@noble/hashes/utils.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha'
import { base64 } from '@scure/base'
import { deriveAccountKeypair, deriveRunKey } from '../src/keys.js'
import { decryptEvent } from '../src/crypto.js'

// Fixed master secret: bytes 0x00..0x1f.
const MASTER = hexToBytes('000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f')

describe('golden vectors (cross-language KDF/envelope contract)', () => {
  it('deriveAccountKeypair pins the account public key', () => {
    const kp = deriveAccountKeypair(MASTER)
    expect(kp.publicKeyHex).toBe(
      'ca14d356f48c1391eb7c8b51970f768360e9d25e5d9fded81b55d6aef64d79b7',
    )
  })

  it('account keypair pins a deterministic Ed25519 signature over a fixed challenge', () => {
    const kp = deriveAccountKeypair(MASTER)
    expect(kp.sign('pactify-golden-challenge')).toBe(
      'd8eb8cab4286a9062f09d6c1c9e539de865cf6883e080234545735936375f2d1' +
        '5335a256a5d098bf67fe9aad98c5b1dac2225fcc697c4bfeaac1ed29d06afe08',
    )
  })

  it('deriveRunKey pins the per-run symmetric key', () => {
    expect(bytesToHex(deriveRunKey(MASTER, 'run-0001'))).toBe(
      'a58393253ee3c085d948bb7f28af35887f835d95671f61cbd8aee617f21edac9',
    )
  })

  it('xchacha20poly1305 envelope pins ciphertext for a fixed key+nonce+plaintext', () => {
    // The primitive is deterministic given (key, nonce); encryptEvent picks a
    // random nonce, so we pin the underlying AEAD directly. Go's port must
    // produce this exact ciphertext for the same inputs.
    const key = hexToBytes('01080f161d242b323940474e555c636a71787f868d949ba2a9b0b7bec5ccd3da')
    const nonce = base64.decode('AgUICw4RFBcaHSAjJiksLzI1ODs+QURH')
    const plaintext = new TextEncoder().encode(
      JSON.stringify({ kind: 'message', role: 'assistant', text: 'golden' }),
    )
    const ct = xchacha20poly1305(key, nonce).encrypt(plaintext)
    expect(base64.encode(ct)).toBe(
      '8fzdY0Aj4+cQ775q7fHllfmR+blpRft8Dg6V/XY6WqhTq2Px0H/V/ToElSTRrZxL3dWTUs+vkLP9SDD06QA1YCl21UnF',
    )
  })

  it('decryptEvent recovers the AgentEvent from the pinned envelope', () => {
    const key = hexToBytes('01080f161d242b323940474e555c636a71787f868d949ba2a9b0b7bec5ccd3da')
    const blob = {
      alg: 'xchacha20poly1305' as const,
      nonce: 'AgUICw4RFBcaHSAjJiksLzI1ODs+QURH',
      ct: '8fzdY0Aj4+cQ775q7fHllfmR+blpRft8Dg6V/XY6WqhTq2Px0H/V/ToElSTRrZxL3dWTUs+vkLP9SDD06QA1YCl21UnF',
    }
    expect(decryptEvent(key, blob)).toEqual({ kind: 'message', role: 'assistant', text: 'golden' })
  })
})
