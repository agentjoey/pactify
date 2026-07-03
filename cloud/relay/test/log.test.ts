import { describe, it, expect } from 'vitest'
import { createLogger, type LogRecord } from '../src/log'

function capture() {
  const lines: LogRecord[] = []
  return { sink: (r: LogRecord) => lines.push(r), lines }
}

describe('createLogger (relay)', () => {
  it('suppresses debug at the default info level; emits info/warn/error', () => {
    const { sink, lines } = capture()
    const log = createLogger({ sink, name: 'relay' })
    log.debug('hidden')
    log.info('hello')
    log.warn('careful')
    log.error('boom')
    expect(lines.map((l) => l.level)).toEqual(['info', 'warn', 'error'])
  })

  it('emits debug when the level is debug', () => {
    const { sink, lines } = capture()
    const log = createLogger({ sink, level: 'debug' })
    log.debug('ingest', { runId: 'r1', seq: 3 })
    expect(lines).toHaveLength(1)
    expect(lines[0]?.fields).toEqual({ runId: 'r1', seq: 3 })
  })

  it('always emits error regardless of level', () => {
    const { sink, lines } = capture()
    const log = createLogger({ sink, level: 'error' })
    log.warn('w')
    log.error('e')
    expect(lines.map((l) => l.level)).toEqual(['error'])
  })

  it('serializes a structured line with json fields', () => {
    const { sink, lines } = capture()
    const log = createLogger({ sink })
    log.warn('rate-limited', { account: 'a1', event: 'rpc' })
    const line = lines[0]!.line
    expect(line).toContain('warn')
    expect(line).toContain('rate-limited')
    expect(JSON.parse(line.slice(line.indexOf('{')))).toMatchObject({
      account: 'a1',
      event: 'rpc',
    })
  })

  it('reads the level from RELAY_LOG', () => {
    const { sink, lines } = capture()
    const log = createLogger({ sink, envVar: 'RELAY_LOG', env: { RELAY_LOG: 'debug' } })
    log.debug('d')
    expect(lines).toHaveLength(1)
  })
})
