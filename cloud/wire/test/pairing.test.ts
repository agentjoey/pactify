import { describe, it, expect } from 'vitest'
import { PairInit, PairReady, PairComplete, PairStatus } from '../src/pairing'

describe('PairInit', () => {
  it('parses a receive init (epkWeb required) and defaults the mode', () => {
    expect(PairInit.parse({ epkWeb: 'AAAA' })).toEqual({ mode: 'receive', epkWeb: 'AAAA' })
  })

  it('parses a provision init with no epkWeb', () => {
    expect(PairInit.parse({ mode: 'provision' })).toEqual({ mode: 'provision' })
  })

  it('rejects a receive init missing epkWeb', () => {
    // mode defaults to receive, which still requires the initiator key.
    expect(PairInit.safeParse({}).success).toBe(false)
    expect(PairInit.safeParse({ mode: 'receive' }).success).toBe(false)
  })

  it('rejects a non-string epkWeb', () => {
    expect(PairInit.safeParse({ epkWeb: 123 }).success).toBe(false)
  })

  it('rejects an unknown mode', () => {
    expect(PairInit.safeParse({ mode: 'bogus', epkWeb: 'AAAA' }).success).toBe(false)
  })
})

describe('PairReady', () => {
  it('parses and round-trips', () => {
    expect(PairReady.parse({ epkMachine: 'MMMM' })).toEqual({ epkMachine: 'MMMM' })
  })

  it('rejects a missing epkMachine', () => {
    expect(PairReady.safeParse({}).success).toBe(false)
  })
})

describe('PairComplete', () => {
  it('parses and round-trips', () => {
    const msg = { epkMachine: 'BBBB', ciphertext: 'CCCC' }
    expect(PairComplete.parse(msg)).toEqual(msg)
  })

  it('rejects a missing ciphertext', () => {
    expect(PairComplete.safeParse({ epkMachine: 'BBBB' }).success).toBe(false)
  })
})

describe('PairStatus', () => {
  it('parses a pending status', () => {
    const msg = { state: 'pending' }
    expect(PairStatus.parse(msg)).toEqual(msg)
  })

  it('parses a ready status carrying the machine ephemeral key', () => {
    const msg = { state: 'ready', epkMachine: 'MMMM' }
    expect(PairStatus.parse(msg)).toEqual(msg)
  })

  it('parses a completed status with payload and round-trips', () => {
    const msg = {
      state: 'completed',
      payload: { epkMachine: 'BBBB', ciphertext: 'CCCC' },
    }
    expect(PairStatus.parse(msg)).toEqual(msg)
  })

  it('rejects an unknown state', () => {
    expect(PairStatus.safeParse({ state: 'expired' }).success).toBe(false)
  })

  it('rejects a malformed payload', () => {
    expect(
      PairStatus.safeParse({ state: 'completed', payload: { epkMachine: 'BBBB' } }).success,
    ).toBe(false)
  })
})
