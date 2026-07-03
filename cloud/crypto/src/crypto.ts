import { xchacha20poly1305 } from '@noble/ciphers/chacha'
import { randomBytes } from '@noble/ciphers/webcrypto'
import { base64 } from '@scure/base'
import { AgentEvent, type EncryptedBlob } from '@pactify-apps/wire'

const ALG = 'xchacha20poly1305' as const
const KEY_BYTES = 32
const NONCE_BYTES = 24

/** Fresh 32-byte symmetric run key. */
export function generateRunKey(): Uint8Array {
  return randomBytes(KEY_BYTES)
}

/** Encrypt a normalized AgentEvent into the wire EncryptedBlob envelope. */
export function encryptEvent(key: Uint8Array, event: AgentEvent): EncryptedBlob {
  const nonce = randomBytes(NONCE_BYTES)
  const plaintext = new TextEncoder().encode(JSON.stringify(event))
  const ct = xchacha20poly1305(key, nonce).encrypt(plaintext)
  return { alg: ALG, nonce: base64.encode(nonce), ct: base64.encode(ct) }
}

/** Decrypt + validate an EncryptedBlob back into an AgentEvent. Throws on
 * auth failure (wrong key / tampering) or schema mismatch. */
export function decryptEvent(key: Uint8Array, blob: EncryptedBlob): AgentEvent {
  const nonce = base64.decode(blob.nonce)
  const ct = base64.decode(blob.ct)
  const plaintext = xchacha20poly1305(key, nonce).decrypt(ct)
  const json: unknown = JSON.parse(new TextDecoder().decode(plaintext))
  return AgentEvent.parse(json)
}
