import { z } from 'zod'
import { EncryptedBlob } from './envelope.js'
import { RunSummary } from './summary.js'
import { MachineInfo } from './machine.js'

/**
 * client → relay WS messages. `subscribe` carries optional per-run resume
 * cursors (`afterSeqByRun`) so the relay can replay only the missed events.
 */
export const ClientWsMessage = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('subscribe'),
    runIds: z.array(z.string()).optional(),
    afterSeqByRun: z.record(z.string(), z.number()).optional(),
  }),
  z.object({
    type: z.literal('unsubscribe'),
    runIds: z.array(z.string()),
  }),
  z.object({
    type: z.literal('ping'),
  }),
])
export type ClientWsMessage = z.infer<typeof ClientWsMessage>

/**
 * relay → client WS messages. `event` is gapless per-run (carries seq +
 * ciphertext); `replay-end` closes a resume burst; `run-updated`/`machines`
 * drive the board; `pong` answers the heartbeat.
 */
export const ServerWsMessage = z.discriminatedUnion('type', [
  z.object({
    type: z.literal('event'),
    runId: z.string(),
    seq: z.number(),
    body: EncryptedBlob,
  }),
  z.object({
    type: z.literal('run-updated'),
    summary: RunSummary,
  }),
  z.object({
    type: z.literal('replay-end'),
    runId: z.string(),
    lastSeq: z.number(),
  }),
  z.object({
    type: z.literal('machines'),
    machines: z.array(MachineInfo),
  }),
  z.object({
    type: z.literal('pong'),
  }),
])
export type ServerWsMessage = z.infer<typeof ServerWsMessage>
