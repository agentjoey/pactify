import { describe, it, expect } from 'vitest'
import { parseServerEnv, installProcessGuards } from '../src/server-main.js'
import { DEFAULT_RUN_TTL_MS } from '../src/retention.js'
import { DEFAULT_MACHINE_TTL_MS } from '../src/machines.js'

describe('parseServerEnv', () => {
  it('errors when DATABASE_URL is missing', () => {
    const res = parseServerEnv({ RELAY_SECRET: 's' })
    expect(res).toEqual({ error: expect.stringContaining('DATABASE_URL') })
  })

  it('errors when RELAY_SECRET is missing', () => {
    const res = parseServerEnv({ DATABASE_URL: 'postgres://x' })
    expect(res).toEqual({ error: expect.stringContaining('RELAY_SECRET') })
  })

  it('parses a full env and defaults PORT to 4310 and runTtlMs to the 7-day default', () => {
    const res = parseServerEnv({ DATABASE_URL: 'postgres://x', RELAY_SECRET: 's' })
    expect(res).toEqual({
      databaseUrl: 'postgres://x',
      secret: 's',
      port: 4310,
      runTtlMs: DEFAULT_RUN_TTL_MS,
      machineTtlMs: DEFAULT_MACHINE_TTL_MS,
    })
  })

  it('honors an explicit PORT', () => {
    const res = parseServerEnv({
      DATABASE_URL: 'postgres://x',
      RELAY_SECRET: 's',
      PORT: '8080',
    })
    expect(res).toEqual({
      databaseUrl: 'postgres://x',
      secret: 's',
      port: 8080,
      runTtlMs: DEFAULT_RUN_TTL_MS,
      machineTtlMs: DEFAULT_MACHINE_TTL_MS,
    })
  })

  it('honors an explicit RELAY_RUN_TTL_MS (including 0 to disable retention)', () => {
    const res = parseServerEnv({
      DATABASE_URL: 'postgres://x',
      RELAY_SECRET: 's',
      RELAY_RUN_TTL_MS: '0',
    })
    expect(res).toEqual({
      databaseUrl: 'postgres://x',
      secret: 's',
      port: 4310,
      runTtlMs: 0,
      machineTtlMs: DEFAULT_MACHINE_TTL_MS,
    })
  })

  it('errors on a non-numeric RELAY_RUN_TTL_MS', () => {
    const res = parseServerEnv({
      DATABASE_URL: 'postgres://x',
      RELAY_SECRET: 's',
      RELAY_RUN_TTL_MS: 'soon',
    })
    expect(res).toEqual({ error: expect.stringContaining('RELAY_RUN_TTL_MS') })
  })

  it('parses REDIS_URL when present', () => {
    const res = parseServerEnv({
      DATABASE_URL: 'postgres://x',
      RELAY_SECRET: 's',
      REDIS_URL: 'redis://localhost:6379',
    })
    expect(res).toEqual({
      databaseUrl: 'postgres://x',
      secret: 's',
      port: 4310,
      redisUrl: 'redis://localhost:6379',
      runTtlMs: DEFAULT_RUN_TTL_MS,
      machineTtlMs: DEFAULT_MACHINE_TTL_MS,
    })
  })

  it('omits redisUrl when REDIS_URL is absent', () => {
    const res = parseServerEnv({
      DATABASE_URL: 'postgres://x',
      RELAY_SECRET: 's',
    })
    expect(res).toEqual({
      databaseUrl: 'postgres://x',
      secret: 's',
      port: 4310,
      runTtlMs: DEFAULT_RUN_TTL_MS,
      machineTtlMs: DEFAULT_MACHINE_TTL_MS,
    })
    expect('redisUrl' in res).toBe(false)
  })
})

describe('installProcessGuards (RELAY-1)', () => {
  it('logs an unhandledRejection and keeps serving (no exit)', () => {
    const errors: Array<{ msg: string; meta?: unknown }> = []
    const before = process.exitCode
    const { onRejection } = installProcessGuards({ error: (msg, meta) => errors.push({ msg, meta }) }, () => {})
    onRejection(new Error('boom'))
    expect(errors).toHaveLength(1)
    expect(errors[0].msg).toContain('unhandledRejection')
    expect(errors[0].meta).toMatchObject({ reason: { message: 'boom' } })
    expect(process.exitCode).toBe(before) // must NOT flag exit — one dropped promise can't kill the relay
    process.off('unhandledRejection', onRejection)
  })

  it('logs an uncaughtException and exits non-zero for a clean restart', () => {
    const errors: Array<{ msg: string; meta?: unknown }> = []
    const exits: number[] = []
    const { onRejection, onException } = installProcessGuards(
      { error: (msg, meta) => errors.push({ msg, meta }) },
      (code) => exits.push(code),
    )
    onException(new Error('fatal'))
    expect(errors[0].msg).toContain('uncaughtException')
    expect(errors[0].meta).toMatchObject({ message: 'fatal' })
    expect(exits).toEqual([1]) // supervisor-driven restart, not a silent crash
    process.off('unhandledRejection', onRejection)
    process.off('uncaughtException', onException)
  })

  it('registers real process listeners', () => {
    const rBefore = process.listenerCount('unhandledRejection')
    const eBefore = process.listenerCount('uncaughtException')
    const { onRejection, onException } = installProcessGuards({ error: () => {} }, () => {})
    expect(process.listenerCount('unhandledRejection')).toBe(rBefore + 1)
    expect(process.listenerCount('uncaughtException')).toBe(eBefore + 1)
    process.off('unhandledRejection', onRejection)
    process.off('uncaughtException', onException)
  })
})
