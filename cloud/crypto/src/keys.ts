import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex, utf8ToBytes } from '@noble/hashes/utils.js'
import { randomBytes } from '@noble/ciphers/webcrypto'
import { ed25519 } from '@noble/curves/ed25519.js'

const KEY_BYTES = 32

export interface AccountKeypair {
  publicKeyHex: string
  sign(challenge: string): string
}

/** The account identity, derived from the shared master secret. Both linxd and
 * the web derive the same Ed25519 keypair → same account. */
export function deriveAccountKeypair(masterSecret: Uint8Array): AccountKeypair {
  const seed = hkdf(sha256, masterSecret, undefined, utf8ToBytes('account'), 32)
  const publicKeyHex = bytesToHex(ed25519.getPublicKey(seed))
  return {
    publicKeyHex,
    sign: (challenge: string) => bytesToHex(ed25519.sign(utf8ToBytes(challenge), seed)),
  }
}

/** A fresh 32-byte master secret (held by the user's devices: linxd + web). */
export function generateMasterSecret(): Uint8Array {
  return randomBytes(KEY_BYTES)
}

/** Per-run symmetric key, derived from the shared master secret + the cleartext
 * runId via HKDF-SHA256. linxd and web derive the same key; the relay cannot. */
export function deriveRunKey(masterSecret: Uint8Array, runId: string): Uint8Array {
  return hkdf(sha256, masterSecret, undefined, new TextEncoder().encode(`run:${runId}`), KEY_BYTES)
}
