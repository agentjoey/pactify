import { z } from 'zod'

/**
 * Relay-blind pairing payloads (ephemeral X25519 ECDH). The relay is a blind
 * courier: it only ever stores public keys + ciphertext, never plaintext.
 *
 * Two directions share the same primitives:
 *  - `receive`  (first-pair): the web is the INITIATOR — it publishes `epkWeb`
 *    and the secret-holding machine wraps the secret back via `complete`.
 *  - `provision` (add a machine): the secret-less machine is the INITIATOR — it
 *    publishes `epkMachine` via `ready`, and the secret-holding (authenticated)
 *    web wraps the secret back via `complete` (here `epkMachine` carries the
 *    web responder's ephemeral key — same wire shape).
 */

/** Pairing direction. `receive` = first-pair (web initiates, machine sends the
 *  secret); `provision` = add-a-machine (machine initiates, web sends it). */
export const PairMode = z.enum(['receive', 'provision'])
export type PairMode = z.infer<typeof PairMode>

/**
 * web → relay: open a pairing session. In `receive` mode the web is the
 * initiator and MUST include its ephemeral public key `epkWeb`. In `provision`
 * mode the web is the sender (not the initiator), so `epkWeb` is absent — the
 * machine publishes its key later via `ready`.
 */
export const PairInit = z
  .object({
    mode: PairMode.default('receive'),
    epkWeb: z.string().optional(),
  })
  .refine((v) => v.mode === 'provision' || v.epkWeb !== undefined, {
    message: 'epkWeb is required in receive mode',
    path: ['epkWeb'],
  })
export type PairInit = z.infer<typeof PairInit>

/** machine → relay (provision only): the secret-less machine's ephemeral public
 *  key, published after it fetches a `provision` code. UNAUTHENTICATED. */
export const PairReady = z.object({
  epkMachine: z.string(),
})
export type PairReady = z.infer<typeof PairReady>

/**
 * sender → relay: the responder's ephemeral public key + the encrypted secret.
 * `receive`: the machine posts (its `epkMachine` + ciphertext). `provision`:
 * the web posts (its ephemeral responder key in the `epkMachine` field +
 * ciphertext). AUTHENTICATED in both modes — only the secret-holder may post.
 */
export const PairComplete = z.object({
  epkMachine: z.string(),
  ciphertext: z.string(),
})
export type PairComplete = z.infer<typeof PairComplete>

/**
 * relay → poll: pairing progress.
 *  - `pending`   — opened, awaiting the next step.
 *  - `ready`     — (provision) the machine published `epkMachine`; the web can
 *                  now wrap the secret to it.
 *  - `completed` — the wrapped secret (`payload`) is available to the receiver.
 */
export const PairStatus = z.object({
  state: z.enum(['pending', 'ready', 'completed']),
  epkMachine: z.string().optional(),
  payload: PairComplete.optional(),
})
export type PairStatus = z.infer<typeof PairStatus>
