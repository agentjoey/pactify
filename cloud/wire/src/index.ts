/**
 * @pactify-apps/wire — shared protocol contracts for Linx.
 *
 * The C hybrid model: a WireMessage is a cleartext operational header plus an
 * opaque E2E-encrypted body. The plaintext encrypted into the body is an
 * AgentEvent. RPC carries client↔relay↔linxd control messages.
 */
export const PROTOCOL_VERSION = 1 as const

export * from './operational.js'
export * from './envelope.js'
export * from './events.js'
export * from './reply.js'
export * from './rpc.js'
export * from './summary.js'
export * from './machine.js'
export * from './ws.js'
export * from './pairing.js'
