/**
 * @pactify-apps/relay — zero-knowledge relay.
 *
 * M1 data layer: a Prisma store (Neon in prod, PGlite in tests/local) that
 * indexes the cleartext operational header and persists the encrypted body as
 * an opaque blob. HTTP/socket.io/auth land in later slices.
 *   - `createPgliteDb` — a PrismaClient over in-memory PGlite with the schema
 *     applied (tests/local).
 *   - `ingestWireMessage` / `getRun` / `getRunEvents` — the run/event store.
 */

export * from './db.js'
export * from './log.js'
export * from './metrics.js'
export * from './repo.js'
export * from './queries.js'
export * from './auth.js'
export * from './health.js'
export * from './server.js'
export * from './sockets.js'
export * from './server-main.js'
export * from './machines.js'
export * from './pair.js'
export * from './retention.js'
export * from './push.js'
