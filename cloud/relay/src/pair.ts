import { randomBytes } from 'node:crypto'
import type { PairComplete, PairMode, PairStatus } from '@pactify-apps/wire'

const PAIR_TTL_MS = 5 * 60 * 1000

interface PairEntry {
  mode: PairMode
  /** Present in `receive` (the web initiator's ephemeral public key). */
  epkWeb?: string
  /** Present once the `provision` machine publishes its ephemeral key. */
  epkMachine?: string
  /** Set once the secret-holder wraps + posts the encrypted secret. */
  payload?: PairComplete
  expiresAt: number
}

export type PairState = 'unknown' | 'expired' | 'pending' | 'ready' | 'completed'

/**
 * In-memory, per-instance, TTL store for relay-blind pairing. The relay only
 * ever holds ephemeral public keys (`epkWeb`, `epkMachine`) and a ciphertext
 * blob — it NEVER sees the plaintext master secret, in any mode. TODO(T5):
 * Redis-back this.
 */
export class PairStore {
  private codes = new Map<string, PairEntry>()

  constructor(
    private now: () => number = () => Date.now(),
    private ttlMs: number = PAIR_TTL_MS,
  ) {}

  /**
   * Create a new pending pair session and return its short code. `receive`
   * carries the web initiator's `epkWeb`; `provision` carries no key yet (the
   * machine publishes its own via {@link ready}).
   */
  init(mode: PairMode, epkWeb?: string): string {
    this.gc()
    const code = makeCode()
    const entry: PairEntry = { mode, expiresAt: this.now() + this.ttlMs }
    if (epkWeb !== undefined) entry.epkWeb = epkWeb
    this.codes.set(code, entry)
    return code
  }

  /** Look up the session's mode and (in receive) the web's ephemeral key. */
  get(code: string): { mode: PairMode; epkWeb?: string } | null {
    const e = this.codes.get(code)
    if (!e || e.expiresAt < this.now()) return null
    return e.epkWeb !== undefined ? { mode: e.mode, epkWeb: e.epkWeb } : { mode: e.mode }
  }

  /**
   * Provision-only: the secret-less machine publishes its ephemeral public key,
   * advancing the session to `ready`. Returns false if the code is unknown,
   * expired, not a provision session, or already past `pending`.
   */
  ready(code: string, epkMachine: string): boolean {
    const e = this.codes.get(code)
    if (!e) return false
    if (e.expiresAt < this.now()) {
      this.codes.delete(code)
      return false
    }
    if (e.mode !== 'provision') return false
    if (e.epkMachine || e.payload) return false
    e.epkMachine = epkMachine
    return true
  }

  /**
   * Complete a pair session with the responder's ephemeral public key and the
   * encrypted secret. Returns true on success; false if the code is unknown,
   * expired, or already completed (single-use).
   */
  complete(code: string, payload: PairComplete): boolean {
    const e = this.codes.get(code)
    if (!e) return false
    if (e.expiresAt < this.now()) {
      this.codes.delete(code)
      return false
    }
    if (e.payload) return false
    e.payload = payload
    return true
  }

  /** Current pairing status for the counterparty to poll. */
  status(code: string): PairStatus | null {
    const e = this.codes.get(code)
    if (!e || e.expiresAt < this.now()) return null
    if (e.payload) return { state: 'completed', payload: e.payload }
    if (e.epkMachine) return { state: 'ready', epkMachine: e.epkMachine }
    return { state: 'pending' }
  }

  /** Whether a code is known and what state it is in. */
  exists(code: string): PairState {
    const e = this.codes.get(code)
    if (!e) return 'unknown'
    if (e.expiresAt < this.now()) return 'expired'
    if (e.payload) return 'completed'
    if (e.epkMachine) return 'ready'
    return 'pending'
  }

  /** Expire old entries eagerly on init. */
  private gc(): void {
    const now = this.now()
    for (const [code, e] of this.codes) {
      if (e.expiresAt < now) this.codes.delete(code)
    }
  }
}

function makeCode(): string {
  // 6 bytes → 8 url-safe base64 chars.
  return randomBytes(6).toString('base64url').slice(0, 8).padEnd(8, 'A')
}
