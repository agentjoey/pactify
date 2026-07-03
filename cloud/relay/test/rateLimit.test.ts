import { describe, it, expect } from 'vitest'
import { TokenBucketLimiter } from '../src/rateLimit'

describe('TokenBucketLimiter', () => {
  it('allows up to capacity then blocks over', () => {
    const t = 0
    const limiter = new TokenBucketLimiter({ capacity: 3, refillPerMs: 0, now: () => t })
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(false)
  })

  it('keys are independent', () => {
    const t = 0
    const limiter = new TokenBucketLimiter({ capacity: 1, refillPerMs: 0, now: () => t })
    expect(limiter.take('a').allowed).toBe(true)
    expect(limiter.take('a').allowed).toBe(false)
    expect(limiter.take('b').allowed).toBe(true)
  })

  it('refills over time using the injected clock', () => {
    let t = 0
    // capacity 2, refill 1 token per 1000ms
    const limiter = new TokenBucketLimiter({ capacity: 2, refillPerMs: 1 / 1000, now: () => t })
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(false)
    // not enough time for a full token
    t = 500
    expect(limiter.take('k').allowed).toBe(false)
    // one token refilled
    t = 1000
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(false)
  })

  it('does not refill beyond capacity', () => {
    let t = 0
    const limiter = new TokenBucketLimiter({ capacity: 2, refillPerMs: 1 / 1000, now: () => t })
    // idle for a long time
    t = 1_000_000
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(true)
    expect(limiter.take('k').allowed).toBe(false)
  })

  it('reports a Retry-After (seconds, rounded up) when blocked', () => {
    const t = 0
    const limiter = new TokenBucketLimiter({ capacity: 1, refillPerMs: 1 / 1000, now: () => t })
    expect(limiter.take('k').allowed).toBe(true)
    const blocked = limiter.take('k')
    expect(blocked.allowed).toBe(false)
    // one token = 1000ms = 1s
    expect(blocked.retryAfterSec).toBe(1)
  })

  it('fromPerMinute builds a bucket whose capacity is the per-minute limit', () => {
    let t = 0
    const limiter = TokenBucketLimiter.fromPerMinute(60, () => t)
    let allowed = 0
    for (let i = 0; i < 70; i++) if (limiter.take('k').allowed) allowed++
    expect(allowed).toBe(60)
    // a minute later, fully refilled
    t = 60_000
    expect(limiter.take('k').allowed).toBe(true)
  })
})
