import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import type { AgentEvent } from '@pactify-apps/wire'
import { deriveProjectKey, encryptEvent } from '@pactify-apps/crypto'
import type { PactEventBroadcast } from '../src/index'

// A controllable fake socket so subscribe() can be exercised without a real relay.
let lastFakeSocket: {
  handlers: Record<string, (arg: unknown) => void>
  emit: (event: string, arg: unknown) => void
  disconnected: boolean
}
vi.mock('socket.io-client', () => ({
  io: () => {
    const handlers: Record<string, (arg: unknown) => void> = {}
    const sock = {
      handlers,
      disconnected: false,
      on(ev: string, fn: (arg: unknown) => void) {
        handlers[ev] = fn
      },
      off(ev: string) {
        delete handlers[ev]
      },
      disconnect() {
        this.disconnected = true
      },
      emit(ev: string, arg: unknown) {
        handlers[ev]?.(arg)
      },
    }
    lastFakeSocket = sock
    return sock
  },
}))

// Import AFTER the mock is registered.
const { RelayClient } = await import('../src/index')

const MASTER = Uint8Array.from({ length: 32 }, (_, i) => i)
const URL = 'https://relay.test'

function jsonResponse(body: unknown, status = 200): Response {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => body,
  } as Response
}

describe('RelayClient', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn())
  })
  afterEach(() => {
    vi.unstubAllGlobals()
    vi.restoreAllMocks()
  })

  it('login exchanges a challenge signature for a bearer token', async () => {
    const fetchMock = fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValueOnce(jsonResponse({ token: 'tok123' }))
    const c = new RelayClient(URL, MASTER)
    await c.login()
    const [url, init] = fetchMock.mock.calls[0]
    expect(url).toBe(`${URL}/v1/auth`)
    const sent = JSON.parse((init as RequestInit).body as string)
    expect(typeof sent.publicKey).toBe('string')
    expect(typeof sent.signature).toBe('string')
    expect(typeof sent.challenge).toBe('string')
  })

  it('listProjects and getProjectEvents send the bearer token and parse the body', async () => {
    const fetchMock = fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValueOnce(jsonResponse({ token: 'tok123' }))
    const c = new RelayClient(URL, MASTER)
    await c.login()

    fetchMock.mockResolvedValueOnce(jsonResponse([{ id: 'p1', name: 'demo', seq: 3, lastEventAt: 9 }]))
    const projects = await c.listProjects()
    expect(projects[0]!.id).toBe('p1')
    const [pUrl, pInit] = fetchMock.mock.calls[1]
    expect(pUrl).toBe(`${URL}/v1/pact/projects`)
    expect((pInit as RequestInit).headers).toMatchObject({ authorization: 'Bearer tok123' })

    fetchMock.mockResolvedValueOnce(jsonResponse([{ projectId: 'p1', seq: 1, eventType: 'assign', ts: 1, bodyEnc: 'x' }]))
    const events = await c.getProjectEvents('p1', 0)
    expect(events[0]!.eventType).toBe('assign')
    expect(fetchMock.mock.calls[2][0]).toBe(`${URL}/v1/pact/projects/p1/events?after_seq=0`)
  })

  it('getJSON re-logs in and retries once on 401', async () => {
    const fetchMock = fetch as unknown as ReturnType<typeof vi.fn>
    fetchMock.mockResolvedValueOnce(jsonResponse({ token: 't1' })) // initial login
    const c = new RelayClient(URL, MASTER)
    await c.login()
    fetchMock
      .mockResolvedValueOnce(jsonResponse({}, 401)) // first read → 401
      .mockResolvedValueOnce(jsonResponse({ token: 't2' })) // re-login
      .mockResolvedValueOnce(jsonResponse([])) // retry read → 200
    const projects = await c.listProjects()
    expect(projects).toEqual([])
    expect(fetchMock).toHaveBeenCalledTimes(4)
  })

  it('decrypt derives the project key locally and recovers the plaintext event', () => {
    const projectId = 'acct1:pactify'
    const event: AgentEvent = { kind: 'message', role: 'assistant', text: 'hello 世界' }
    const key = deriveProjectKey(MASTER, projectId)
    const bodyEnc = JSON.stringify(encryptEvent(key, event))

    const c = new RelayClient(URL, MASTER)
    expect(c.decrypt(projectId, bodyEnc)).toEqual(event)
  })

  it('subscribe filters the account broadcast to one project and unsubscribes cleanly', () => {
    const c = new RelayClient(URL, MASTER)
    const seen: PactEventBroadcast[] = []
    const off = c.subscribe('p1', (e) => seen.push(e))

    const mk = (projectId: string): PactEventBroadcast => ({ projectId, seq: 1, eventType: 'assign', ts: 1, bodyEnc: '' })
    lastFakeSocket.emit('pact-event', mk('p2')) // other project → ignored
    lastFakeSocket.emit('pact-event', mk('p1')) // ours → delivered
    expect(seen).toHaveLength(1)
    expect(seen[0]!.projectId).toBe('p1')

    off()
    expect(lastFakeSocket.disconnected).toBe(true)
    expect(lastFakeSocket.handlers['pact-event']).toBeUndefined()
  })
})
