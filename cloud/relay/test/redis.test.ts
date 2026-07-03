import { describe, it, expect, vi } from 'vitest'
import { createClient } from 'redis'
import { maybeMakeAdapter } from '../src/redis.js'

vi.mock('redis', () => {
  const makeClient = () => ({
    connect: vi.fn(() => Promise.resolve()),
    disconnect: vi.fn(),
    duplicate: vi.fn(() => makeClient()),
    on: vi.fn(),
    subscribe: vi.fn(),
    unsubscribe: vi.fn(),
    publish: vi.fn(),
  })
  return {
    createClient: vi.fn(() => makeClient()),
  }
})

describe('maybeMakeAdapter', () => {
  it('returns undefined when no redisUrl is provided', async () => {
    expect(await maybeMakeAdapter(undefined)).toBeUndefined()
  })

  it('connects pub/sub clients before returning an adapter constructor', async () => {
    const adapter = await maybeMakeAdapter('redis://localhost:6379')
    expect(adapter).toBeDefined()
    expect(typeof adapter).toBe('function')

    expect(createClient).toHaveBeenCalledTimes(1)
    expect(createClient).toHaveBeenCalledWith({ url: 'redis://localhost:6379' })

    const pub = vi.mocked(createClient).mock.results[0]!.value
    expect(pub.connect).toHaveBeenCalledTimes(1)
    expect(pub.duplicate).toHaveBeenCalledTimes(1)

    const sub = pub.duplicate.mock.results[0]!.value
    expect(sub.connect).toHaveBeenCalledTimes(1)
  })
})
