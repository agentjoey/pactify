import { describe, it, expect } from 'vitest'
import { createMetrics, isValidMetricName } from '../src/metrics'

describe('MetricsRegistry', () => {
  it('counts a label-less counter', () => {
    const m = createMetrics()
    m.inc('ingest_events_total')
    m.inc('ingest_events_total')
    expect(m.get('ingest_events_total')).toBe(2)
  })

  it('keeps separate series per label value', () => {
    const m = createMetrics()
    m.inc('auth_attempts_total', { result: 'success' })
    m.inc('auth_attempts_total', { result: 'failure' })
    m.inc('auth_attempts_total', { result: 'failure' })
    expect(m.get('auth_attempts_total', { result: 'success' })).toBe(1)
    expect(m.get('auth_attempts_total', { result: 'failure' })).toBe(2)
  })

  it('composes multi-label series independently', () => {
    const m = createMetrics()
    m.inc('http_requests_total', { route: '/v1/runs', status: '200' })
    m.inc('http_requests_total', { route: '/v1/runs', status: '200' })
    m.inc('http_requests_total', { route: '/v1/runs', status: '401' })
    expect(m.get('http_requests_total', { route: '/v1/runs', status: '200' })).toBe(2)
    expect(m.get('http_requests_total', { route: '/v1/runs', status: '401' })).toBe(1)
  })

  it('tracks gauges with set/add and never goes negative', () => {
    const m = createMetrics()
    m.addGauge('connected_sockets', 3)
    m.addGauge('connected_sockets', -1)
    expect(m.getGauge('connected_sockets')).toBe(2)
    m.addGauge('connected_sockets', -5)
    expect(m.getGauge('connected_sockets')).toBe(0)
    m.setGauge('connected_sockets', 7)
    expect(m.getGauge('connected_sockets')).toBe(7)
  })

  it('accumulates a summary as count + sum', () => {
    const m = createMetrics()
    m.observe('http_request_duration_ms', 10)
    m.observe('http_request_duration_ms', 30)
    expect(m.getSummary('http_request_duration_ms')).toEqual({ count: 2, sum: 40 })
  })

  describe('render() — Prometheus text', () => {
    it('emits # HELP and # TYPE for every counter', () => {
      const text = createMetrics().render()
      expect(text).toContain('# HELP http_requests_total')
      expect(text).toContain('# TYPE http_requests_total counter')
      expect(text).toContain('# TYPE connected_sockets gauge')
      expect(text).toContain('# TYPE http_request_duration_ms summary')
    })

    it('renders a labelled series as name{labels} value', () => {
      const m = createMetrics()
      m.inc('http_requests_total', { route: '/v1/runs', status: '200' })
      const text = m.render()
      expect(text).toContain('http_requests_total{route="/v1/runs",status="200"} 1')
    })

    it('renders summary as _count and _sum', () => {
      const m = createMetrics()
      m.observe('http_request_duration_ms', 5)
      const text = m.render()
      expect(text).toContain('http_request_duration_ms_count 1')
      expect(text).toContain('http_request_duration_ms_sum 5')
    })

    it('every series line has a valid metric name and a numeric value', () => {
      const m = createMetrics()
      m.inc('http_requests_total', { route: '/v1/runs', status: '200' })
      m.inc('rpc_total', { type: 'spawn' })
      m.setGauge('connected_sockets', 4)
      m.observe('http_request_duration_ms', 12)
      for (const line of m.render().split('\n')) {
        if (line === '' || line.startsWith('#')) continue
        const space = line.lastIndexOf(' ')
        const series = line.slice(0, space)
        const value = line.slice(space + 1)
        const baseName = series.replace(/\{.*\}$/, '')
        expect(isValidMetricName(baseName)).toBe(true)
        expect(Number.isNaN(Number(value))).toBe(false)
      }
    })

    it('escapes special chars in label values', () => {
      const m = createMetrics()
      m.inc('errors_total', { where: 'a"b\\c' })
      expect(m.render()).toContain('errors_total{where="a\\"b\\\\c"} 1')
    })

    it('does not leak secret-shaped label values it was never given', () => {
      const m = createMetrics()
      // Only aggregate dimensions go in. The render must not contain anything
      // resembling a token or account id we never passed.
      m.inc('auth_attempts_total', { result: 'failure' })
      m.inc('http_requests_total', { route: '/v1/auth', status: '401' })
      const text = m.render()
      expect(text).not.toMatch(/Bearer/i)
      expect(text).not.toContain('account=')
      // route + result are the only label keys present
      const labelKeys = [...text.matchAll(/\{([^}]*)\}/g)]
        .flatMap((mm) => mm[1]!.split(','))
        .map((kv) => kv.split('=')[0])
      expect(new Set(labelKeys)).toEqual(new Set(['result', 'route', 'status']))
    })
  })
})
