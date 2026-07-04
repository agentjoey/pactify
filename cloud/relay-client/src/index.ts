import { io, type Socket } from 'socket.io-client'
import { deriveAccountKeypair, deriveProjectKey, decryptEvent } from '@pactify-apps/crypto'
import type { EncryptedBlob } from '@pactify-apps/wire'

/** The cleartext header of one pact event (as served by /v1/pact/projects/:id/events). */
export interface PactEventHeader {
  seq: number
  eventType: string
  task?: string | null
  feature?: string | null
  ts: number
}

/** A project as served by GET /v1/pact/projects. */
export interface Project {
  id: string
  name: string
  feature?: string | null
  seq: number
  lastEventAt: number
}

/** A stored pact event: cleartext header + opaque encrypted body. */
export interface PactEvent extends PactEventHeader {
  projectId: string
  bodyEnc: string
}

/** The relay's realtime `pact-event` broadcast payload. */
export interface PactEventBroadcast {
  projectId: string
  seq: number
  eventType: string
  task?: string | null
  feature?: string | null
  ts: number
  bodyEnc: string
}

/**
 * Framework-agnostic read-client for the zero-knowledge pact relay: challenge
 * auth, HTTP reads (projects + events), realtime socket subscription, and
 * client-side decrypt. No UI-framework dependency — the project key is derived
 * locally from the master secret; the relay never holds it. Shared by Mission
 * Control (Svelte) and the dashboard's RelaySource (React).
 */
export class RelayClient {
  private url: string
  private master: Uint8Array
  private token = ''
  private socket: Socket | null = null

  constructor(url: string, master: Uint8Array) {
    this.url = url.replace(/\/+$/, '')
    this.master = master
  }

  /** Challenge-sign with the account key and exchange for a bearer token. */
  async login(): Promise<void> {
    const kp = deriveAccountKeypair(this.master)
    const challenge = crypto.randomUUID()
    const res = await fetch(`${this.url}/v1/auth`, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify({ publicKey: kp.publicKeyHex, challenge, signature: kp.sign(challenge) }),
    })
    if (!res.ok) throw new Error(`auth failed: ${res.status}`)
    this.token = ((await res.json()) as { token: string }).token
  }

  private auth() {
    return { authorization: `Bearer ${this.token}` }
  }

  private async getJSON<T>(path: string): Promise<T> {
    const res = await fetch(`${this.url}${path}`, { headers: this.auth() })
    if (res.status === 401) {
      await this.login()
      const retry = await fetch(`${this.url}${path}`, { headers: this.auth() })
      if (!retry.ok) throw new Error(`${path}: ${retry.status}`)
      return retry.json() as Promise<T>
    }
    if (!res.ok) throw new Error(`${path}: ${res.status}`)
    return res.json() as Promise<T>
  }

  listProjects(): Promise<Project[]> {
    return this.getJSON<Project[]>('/v1/pact/projects')
  }

  /** A project's stored events in seq order (optionally only seq > afterSeq). */
  getProjectEvents(projectId: string, afterSeq?: number): Promise<PactEvent[]> {
    const q = afterSeq !== undefined ? `?after_seq=${afterSeq}` : ''
    return this.getJSON<PactEvent[]>(`/v1/pact/projects/${encodeURIComponent(projectId)}/events${q}`)
  }

  /** Back-compat alias for {@link getProjectEvents} (Mission Control's name). */
  projectEvents(projectId: string, afterSeq?: number): Promise<PactEvent[]> {
    return this.getProjectEvents(projectId, afterSeq)
  }

  /** Decrypt a stored/broadcast event body into its original pact event JSON.
   * The project key is derived locally; the relay never held it. */
  decrypt(projectId: string, bodyEnc: string): unknown {
    const key = deriveProjectKey(this.master, projectId)
    const blob = JSON.parse(bodyEnc) as EncryptedBlob
    return decryptEvent(key, blob)
  }

  /**
   * Subscribe to realtime pact events for one project. Opens a dedicated socket,
   * filters the account-wide `pact-event` broadcast down to `projectId`, and
   * returns an unsubscribe function that removes the handler and closes the socket.
   */
  subscribe(projectId: string, onEvent: (e: PactEventBroadcast) => void): () => void {
    const socket = io(this.url, { auth: { token: this.token, role: 'client' } })
    const handler = (e: PactEventBroadcast) => {
      if (e.projectId === projectId) onEvent(e)
    }
    socket.on('pact-event', handler)
    return () => {
      socket.off('pact-event', handler)
      socket.disconnect()
    }
  }

  /** Back-compat: subscribe to ALL account events on the instance socket (Mission Control). */
  connect(onEvent: (e: PactEventBroadcast) => void): void {
    this.socket = io(this.url, { auth: { token: this.token, role: 'client' } })
    this.socket.on('pact-event', onEvent)
  }

  disconnect(): void {
    this.socket?.disconnect()
    this.socket = null
  }
}

/** Legacy name retained for Mission Control (Svelte) consumers. */
export { RelayClient as MissionControlRelay }
