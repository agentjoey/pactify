import type { Server } from 'socket.io'

/**
 * A late-bound bridge from the HTTP layer to socket.io broadcasts. pact events
 * arrive over HTTP (POST /v1/pact/ingest — pactify machines post; Go socket.io
 * support is poor), but web board clients watch over sockets, so the HTTP handler
 * needs to fan out to the account room. `io` only exists after attachSockets, so
 * createServer gets this bridge, its routes call `emit`, and server-main binds
 * the real `io` in once it's created. Unbound, `emit` is a safe no-op (tests).
 */
export interface Broadcaster {
  emit(accountId: string, event: string, payload: unknown): void
  bind(io: Server): void
}

export function createBroadcaster(): Broadcaster {
  let io: Server | null = null
  return {
    emit(accountId, event, payload) {
      io?.to(`acct:${accountId}`).emit(event, payload)
    },
    bind(server) {
      io = server
    },
  }
}
