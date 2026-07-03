import { io, type Socket } from 'socket.io-client'
import { deriveAccountKeypair, deriveProjectKey, decryptEvent } from '@pactify-apps/crypto'
import type { EncryptedBlob } from '@pactify-apps/wire'
import type { PactEventHeader } from './board'

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

/** Mission Control's connection to the relay: auth, HTTP reads, realtime socket. */
export class MissionControlRelay {
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

  projectEvents(projectId: string, afterSeq?: number): Promise<PactEvent[]> {
    const q = afterSeq !== undefined ? `?after_seq=${afterSeq}` : ''
    return this.getJSON<PactEvent[]>(`/v1/pact/projects/${encodeURIComponent(projectId)}/events${q}`)
  }

  /** Decrypt a stored/broadcast event body into its original pact event JSON.
   * The project key is derived locally; the relay never held it. */
  decrypt(projectId: string, bodyEnc: string): unknown {
    const key = deriveProjectKey(this.master, projectId)
    const blob = JSON.parse(bodyEnc) as EncryptedBlob
    return decryptEvent(key, blob)
  }

  /** Subscribe to realtime pact events for this account. */
  connect(onEvent: (e: PactEventBroadcast) => void): void {
    this.socket = io(this.url, { auth: { token: this.token, role: 'client' } })
    this.socket.on('pact-event', onEvent)
  }

  disconnect(): void {
    this.socket?.disconnect()
    this.socket = null
  }
}
