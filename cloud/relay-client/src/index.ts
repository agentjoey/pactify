import { io, type Socket } from 'socket.io-client'
import { deriveAccountKeypair, deriveProjectKey, decryptEvent, decryptEventRaw } from '@pactify-apps/crypto'
import type { EncryptedBlob, RpcRequest, MachineInfo } from '@pactify-apps/wire'

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

/** An ephemeral courier event broadcast to the account room (e.g. cockpit mirror). */
export interface EphemeralMessage {
  runId: string
  seq: number
  body: unknown
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
    const body = (await res.json()) as { token: string; accountId?: string }
    this.token = body.token
    if (body.accountId) this.accountId = body.accountId
  }

  private accountId = ''

  /** This connection's account id (available after login). The U3 down-channel
   * uses it as the target machineId (MVP: machineId == accountId). */
  account(): string {
    return this.accountId
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

  /** The account's machines (presence + agent kinds) — the multi-machine roster
   * the dashboard shows and targets rpc at. Offline machines past the board TTL
   * are hidden by the relay. */
  listMachines(): Promise<MachineInfo[]> {
    return this.getJSON<MachineInfo[]>('/v1/machines')
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

  /** Decrypt without validating against linx's AgentEvent schema — for foreign
   * payloads like pactify's raw pact ledger lines, which decryptEvent would
   * reject with `invalid_union_discriminator`. The caller validates the shape.
   * Accepts either the JSON string or the already-parsed EncryptedBlob object. */
  decryptRaw(projectId: string, bodyEnc: string | unknown): unknown {
    const key = deriveProjectKey(this.master, projectId)
    const blob = (
      typeof bodyEnc === 'string' ? JSON.parse(bodyEnc) : JSON.parse(JSON.stringify(bodyEnc))
    ) as EncryptedBlob
    return decryptEventRaw(key, blob)
  }

  /**
   * Subscribe to realtime pact events for one project. Opens a dedicated socket,
   * filters the account-wide `pact-event` broadcast down to `projectId`, and
   * returns an unsubscribe function that removes the handler and closes the socket.
   */
  subscribe(
    projectId: string,
    onEvent: (e: PactEventBroadcast) => void,
    onConn?: (live: boolean) => void,
  ): () => void {
    const socket = io(this.url, { auth: { token: this.token, role: 'client' } })
    const handler = (e: PactEventBroadcast) => {
      if (e.projectId === projectId) onEvent(e)
    }
    socket.on('pact-event', handler)
    // Surface socket liveness so a hosted dashboard can show online/offline
    // (previously the connection state was never exposed to callers).
    if (onConn) {
      socket.on('connect', () => onConn(true))
      socket.on('disconnect', () => onConn(false))
      socket.on('connect_error', () => onConn(false))
    }
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
    this.control?.disconnect()
    this.control = null
  }

  // A persistent control socket for outbound rpc (distinct from the per-project
  // subscribe sockets). Opened lazily on first sendRpc.
  private control: Socket | null = null

  private controlSocket(): Socket {
    if (!this.control) {
      this.control = io(this.url, { auth: { token: this.token, role: 'client' } })
    }
    return this.control
  }

  /**
   * Send a pact command (U3 P-b rpc type) to the target machine. Fire-and-forget:
   * the relay routes it by `machineId` and the machine executes it locally; the
   * EFFECT comes back through the event stream (the executed verb appends to the
   * ledger, which the machine uploads and the board re-projects), not a direct
   * ack — so there is no reply to await here. Requires a prior login() for the token.
   */
  sendRpc(rpc: RpcRequest): void {
    this.controlSocket().emit('rpc', rpc)
  }

  private ephemeralHandlers = new Set<(e: EphemeralMessage) => void>()
  private ephemeralListenerAttached = false

  /**
   * Subscribe to ephemeral account-room events couriered by the relay. The relay
   * forwards events with a `cockpit:` runId prefix from machine ingest; the
   * caller filters and decrypts the opaque body. Returns an unsubscribe function.
   */
  onEphemeral(cb: (e: EphemeralMessage) => void): () => void {
    this.ephemeralHandlers.add(cb)
    if (!this.ephemeralListenerAttached) {
      this.ephemeralListenerAttached = true
      this.controlSocket().on('event', (e: EphemeralMessage) => {
        if (typeof e?.runId === 'string' && e.runId.startsWith('cockpit:')) {
          this.ephemeralHandlers.forEach((h) => h(e))
        }
      })
    }
    return () => {
      this.ephemeralHandlers.delete(cb)
    }
  }
}

/** Legacy name retained for Mission Control (Svelte) consumers. */
export { RelayClient as MissionControlRelay }
