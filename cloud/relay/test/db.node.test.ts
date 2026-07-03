import { describe, it, expect } from 'vitest'
import { createPostgresDb } from '../src/db.js'

describe('relay db: prod Postgres factory', () => {
  it('constructs a Prisma client without opening a connection', () => {
    // An invalid URL is fine: the pg Pool is lazy, so construction must not
    // throw or dial out — no real Neon/network in tests. (Prisma 7's client is
    // a Proxy, so `instanceof PrismaClient` is unreliable; assert the contract:
    // it exposes the connection lifecycle + model accessors we rely on.)
    const db = createPostgresDb('postgres://invalid:invalid@127.0.0.1:1/none')
    expect(typeof db.$connect).toBe('function')
    expect(typeof db.$disconnect).toBe('function')
    expect(typeof db.account).toBe('object')
    expect(typeof db.run).toBe('object')
  })
})
