import { z } from 'zod'
import { OperationalHeader } from './operational.js'

/**
 * Opaque E2E ciphertext. wire defines the SHAPE only; the crypto lives in
 * @pactify/core / linxd. nonce and ct are base64.
 */
export const EncryptedBlob = z.object({
  alg: z.literal('xchacha20poly1305'),
  nonce: z.string().min(1),
  ct: z.string().min(1),
})
export type EncryptedBlob = z.infer<typeof EncryptedBlob>

/** One relay message: cleartext header + encrypted body. */
export const WireMessage = z.object({
  header: OperationalHeader,
  body: EncryptedBlob,
})
export type WireMessage = z.infer<typeof WireMessage>
