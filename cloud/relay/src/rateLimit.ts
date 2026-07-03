/**
 * In-memory token-bucket rate limiting for the single-instance relay (no Redis).
 *
 * A {@link TokenBucketLimiter} holds one bucket per string key (e.g. an IP, or
 * `ip:account`). Each {@link TokenBucketLimiter.take | take} consumes one token;
 * tokens refill continuously at `refillPerMs` up to `capacity`. Time is injected
 * (`now`) so the limiter is pure and unit-testable. When empty, `take` reports
 * `allowed: false` plus a `retryAfterSec` (seconds, rounded up) telling the
 * caller when the next token is available.
 *
 * This is an MVP abuse/brute-force guard, not a distributed limiter: state lives
 * in this process only. With multiple relay instances each enforces its own
 * limits; that's acceptable for the current single-instance deployment.
 */

/** Result of a {@link TokenBucketLimiter.take} call. */
export interface TakeResult {
  /** Whether a token was available and consumed. */
  allowed: boolean
  /** When blocked, seconds until the next token (rounded up); `0` when allowed. */
  retryAfterSec: number
}

/** Construction options for a {@link TokenBucketLimiter}. */
export interface TokenBucketOptions {
  /** Maximum tokens a bucket holds (also the burst size). */
  capacity: number
  /** Tokens added per millisecond. `0` disables refill (fixed quota until reset). */
  refillPerMs: number
  /** Injected clock; defaults to `Date.now`. */
  now?: () => number
}

interface Bucket {
  tokens: number
  updatedAt: number
}

/**
 * A keyed token-bucket limiter. Buckets are created lazily on first use and
 * kept in a `Map`; idle keys cost one small entry until the process restarts,
 * which is fine for the bounded key space (IPs / accounts) of the relay MVP.
 */
export class TokenBucketLimiter {
  private readonly capacity: number
  private readonly refillPerMs: number
  private readonly now: () => number
  private readonly buckets = new Map<string, Bucket>()

  constructor(opts: TokenBucketOptions) {
    this.capacity = opts.capacity
    this.refillPerMs = opts.refillPerMs
    this.now = opts.now ?? (() => Date.now())
  }

  /**
   * Build a per-minute limiter: `capacity = perMinute` and refill sized so a
   * full bucket is restored over 60s. Falling back to a 1-token bucket if a
   * non-positive limit is passed keeps the relay from accidentally disabling
   * the guard via a bad env value.
   */
  static fromPerMinute(perMinute: number, now?: () => number): TokenBucketLimiter {
    const capacity = perMinute > 0 ? perMinute : 1
    return new TokenBucketLimiter({ capacity, refillPerMs: capacity / 60_000, now })
  }

  /** Consume one token for `key`. See {@link TakeResult}. */
  take(key: string): TakeResult {
    const t = this.now()
    let bucket = this.buckets.get(key)
    if (!bucket) {
      bucket = { tokens: this.capacity, updatedAt: t }
      this.buckets.set(key, bucket)
    }
    // Refill for elapsed time, capped at capacity.
    const elapsed = Math.max(0, t - bucket.updatedAt)
    bucket.tokens = Math.min(this.capacity, bucket.tokens + elapsed * this.refillPerMs)
    bucket.updatedAt = t

    if (bucket.tokens >= 1) {
      bucket.tokens -= 1
      return { allowed: true, retryAfterSec: 0 }
    }
    const deficit = 1 - bucket.tokens
    const waitMs = this.refillPerMs > 0 ? deficit / this.refillPerMs : Number.POSITIVE_INFINITY
    const retryAfterSec = Number.isFinite(waitMs) ? Math.max(1, Math.ceil(waitMs / 1000)) : 1
    return { allowed: false, retryAfterSec }
  }
}
