import { describe, it, expect } from 'vitest'
import { health } from '../src/index.js'

describe('health()', () => {
  it('returns ok: true', () => {
    expect(health()).toEqual({ ok: true })
  })
})
