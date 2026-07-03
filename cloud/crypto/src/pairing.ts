/**
 * Relay-blind pairing via ephemeral X25519 ECDH.
 *
 * The web and the machine (linxd) each generate an ephemeral X25519 keypair and
 * exchange only public keys + ciphertext through the relay. They derive the same
 * symmetric key from the ECDH shared secret (HKDF-SHA256, info `linx-pair-v1`)
 * and use it to wrap the account master secret with xchacha20poly1305. The relay
 * couriers public keys and ciphertext but never learns the master secret.
 */
import { x25519 } from '@noble/curves/ed25519.js'
import { xchacha20poly1305 } from '@noble/ciphers/chacha'
import { randomBytes } from '@noble/ciphers/webcrypto'
import { hkdf } from '@noble/hashes/hkdf.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { bytesToHex, hexToBytes, utf8ToBytes } from '@noble/hashes/utils.js'
import { base64url } from '@scure/base'

const KEY_BYTES = 32
const NONCE_BYTES = 24
const PAIR_INFO = utf8ToBytes('linx-pair-v1')

/** Derive the symmetric pairing key from an X25519 shared secret. */
function pairingKey(shared: Uint8Array): Uint8Array {
  return hkdf(sha256, shared, undefined, PAIR_INFO, KEY_BYTES)
}

export interface PairingInitiator {
  /** Hex of the web side's ephemeral X25519 public key. */
  epkWebPub: string
  /**
   * Finish pairing on the web side: derive the shared key from the machine's
   * ephemeral public key, then decrypt `base64url(nonce(24) || ct)` into the
   * master secret. Throws on auth failure (tampering / wrong key).
   */
  complete(epkMachinePub: string, ciphertext: string): Uint8Array
}

/**
 * WEB side. Generate an ephemeral X25519 keypair and return its public key plus
 * a `complete()` that decrypts the master secret the machine wrapped for us.
 */
export function createPairingInitiator(): PairingInitiator {
  const webPriv = x25519.utils.randomSecretKey()
  const epkWebPub = bytesToHex(x25519.getPublicKey(webPriv))

  return {
    epkWebPub,
    complete(epkMachinePub: string, ciphertext: string): Uint8Array {
      const shared = x25519.getSharedSecret(webPriv, hexToBytes(epkMachinePub))
      const key = pairingKey(shared)
      const raw = base64url.decode(ciphertext)
      const nonce = raw.slice(0, NONCE_BYTES)
      const ct = raw.slice(NONCE_BYTES)
      return xchacha20poly1305(key, nonce).decrypt(ct)
    },
  }
}

export interface PairingResponse {
  /** Hex of the machine side's ephemeral X25519 public key. */
  epkMachinePub: string
  /** `base64url(nonce(24) || ct)` of the wrapped master secret. */
  ciphertext: string
}

/**
 * MACHINE / linxd side. Generate an ephemeral X25519 keypair, derive the shared
 * key from the web's ephemeral public key, and wrap `masterSecret` for the web.
 */
export function pairingRespond(masterSecret: Uint8Array, epkWebPub: string): PairingResponse {
  const machinePriv = x25519.utils.randomSecretKey()
  const epkMachinePub = bytesToHex(x25519.getPublicKey(machinePriv))
  const shared = x25519.getSharedSecret(machinePriv, hexToBytes(epkWebPub))
  const key = pairingKey(shared)
  const nonce = randomBytes(NONCE_BYTES)
  const ct = xchacha20poly1305(key, nonce).encrypt(masterSecret)
  const raw = new Uint8Array(nonce.length + ct.length)
  raw.set(nonce, 0)
  raw.set(ct, nonce.length)
  return { epkMachinePub, ciphertext: base64url.encode(raw) }
}
