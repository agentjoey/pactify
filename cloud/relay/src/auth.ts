import { timingSafeEqual } from 'node:crypto'
import type { PrismaClient } from '@prisma/client'
import { ed25519 } from '@noble/curves/ed25519.js'
import { hmac } from '@noble/hashes/hmac.js'
import { sha256 } from '@noble/hashes/sha2.js'
import { hexToBytes, utf8ToBytes } from '@noble/hashes/utils.js'
import { base64urlnopad } from '@scure/base'

/**
 * Constant-time string equality for secrets/tokens/signatures. Returns false on
 * length mismatch (the lengths themselves are not secret) and otherwise defers
 * to {@link timingSafeEqual} so the byte comparison does not short-circuit and
 * leak timing.
 */
export function timingSafeEqualStr(a: string, b: string): boolean {
  const aBuf = Buffer.from(a)
  const bBuf = Buffer.from(b)
  if (aBuf.length !== bBuf.length) return false
  return timingSafeEqual(aBuf, bBuf)
}

/** Verify an Ed25519 signature (hex) over a challenge string. */
export function verifyChallenge(
  publicKeyHex: string,
  challenge: string,
  signatureHex: string,
): boolean {
  try {
    return ed25519.verify(
      hexToBytes(signatureHex),
      utf8ToBytes(challenge),
      hexToBytes(publicKeyHex),
    )
  } catch {
    return false
  }
}

interface TokenPayload {
  accountId: string
  exp: number
}

function macOf(secret: string, body: string): string {
  return base64urlnopad.encode(hmac(sha256, utf8ToBytes(secret), utf8ToBytes(body)))
}

/** Issue a stateless HMAC-SHA256 token: base64url(payload).base64url(mac). */
export function issueToken(secret: string, accountId: string, ttlMs: number, now: number): string {
  const payload: TokenPayload = { accountId, exp: now + ttlMs }
  const body = base64urlnopad.encode(utf8ToBytes(JSON.stringify(payload)))
  return `${body}.${macOf(secret, body)}`
}

/** Verify a token; returns { accountId } or null (bad mac / expired / malformed). */
export function verifyToken(
  secret: string,
  token: string,
  now: number,
): { accountId: string } | null {
  const dot = token.indexOf('.')
  if (dot < 0) return null
  const body = token.slice(0, dot)
  const sig = token.slice(dot + 1)
  const expected = macOf(secret, body)
  // Constant-time compare; rejects length mismatch without leaking via early-out.
  if (!timingSafeEqualStr(sig, expected)) return null
  try {
    const payload = JSON.parse(
      new TextDecoder().decode(base64urlnopad.decode(body)),
    ) as TokenPayload
    if (typeof payload.accountId !== 'string' || typeof payload.exp !== 'number') return null
    if (payload.exp < now) return null
    return { accountId: payload.accountId }
  } catch {
    return null
  }
}

/** Verify the challenge, upsert the Account by public key, return a token. */
export async function authenticate(
  db: PrismaClient,
  secret: string,
  input: { publicKey: string; challenge: string; signature: string },
  ttlMs: number,
  now: number,
): Promise<{ token: string; accountId: string } | null> {
  if (!verifyChallenge(input.publicKey, input.challenge, input.signature)) return null
  const account = await db.account.upsert({
    where: { publicKey: input.publicKey },
    create: { publicKey: input.publicKey },
    update: {},
  })
  return { token: issueToken(secret, account.id, ttlMs, now), accountId: account.id }
}
