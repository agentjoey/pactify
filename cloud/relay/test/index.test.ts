import { describe, it, expect } from 'vitest'
import * as relay from '../src/index.js'

describe('@pactify/linx-relay public surface', () => {
  it('exports the data layer', () => {
    expect(typeof relay.ingestWireMessage).toBe('function')
    expect(typeof relay.getRun).toBe('function')
    expect(typeof relay.getRunEvents).toBe('function')
    expect(typeof relay.createPgliteDb).toBe('function')
  })

  it('exports the read-side queries', () => {
    expect(typeof relay.listRuns).toBe('function')
    expect(typeof relay.listPendingApprovals).toBe('function')
    expect(typeof relay.getRunEventsAfter).toBe('function')
  })

  it('exports the auth primitives', () => {
    expect(typeof relay.verifyChallenge).toBe('function')
    expect(typeof relay.issueToken).toBe('function')
    expect(typeof relay.verifyToken).toBe('function')
    expect(typeof relay.authenticate).toBe('function')
  })

  it('exports the http server', () => {
    expect(typeof relay.createServer).toBe('function')
  })

  it('exports the socket layer', () => {
    expect(typeof relay.attachSockets).toBe('function')
  })

  it('exports the server bootstrap', () => {
    expect(typeof relay.parseServerEnv).toBe('function')
    expect(typeof relay.startServer).toBe('function')
    expect(typeof relay.main).toBe('function')
  })
})
