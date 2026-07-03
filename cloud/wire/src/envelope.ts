import { z } from 'zod'
import { OperationalHeader } from './operational.js'

/**
 * Opaque E2E ciphertext. wire defines the SHAPE only; the crypto lives in
 * @pactify-apps/crypto / linxd. nonce and ct are base64.
 */
// Bounds are generous DoS caps (a conforming peer stays far under), not tight
// limits — see docs/specs/agentworks-wire.md §2.5. A 24-byte nonce is ~32 b64
// chars; the ct is an encrypted AgentEvent that can be large (diffs, tool output).
export const EncryptedBlob = z
  .object({
    alg: z.literal('xchacha20poly1305'),
    nonce: z.string().min(1).max(64),
    ct: z.string().min(1).max(8_000_000),
  })
  .strict()
export type EncryptedBlob = z.infer<typeof EncryptedBlob>

/** One relay message: cleartext header + encrypted body. */
export const WireMessage = z
  .object({
    header: OperationalHeader,
    body: EncryptedBlob,
  })
  .strict()
export type WireMessage = z.infer<typeof WireMessage>
