/**
 * Tiny dependency-free in-process metrics registry for the relay.
 *
 * Three metric kinds:
 *   - counters: monotonically increasing totals, optionally split by a small set
 *     of label values (e.g. `http_requests_total{route,status}`).
 *   - gauges: a single value that can go up or down (e.g. connected sockets).
 *   - a simple summary: count + sum for a value stream (e.g. request duration),
 *     enough to derive an average without a full histogram.
 *
 * The registry renders to Prometheus text format (`# HELP` / `# TYPE` + one
 * `name{labels} value` line per series). It is pure and injectable so the hot
 * paths can take a registry and tests can assert on counts/labels directly.
 *
 * SAFETY: labels are aggregate-only. Never pass tokens, account ids, ciphertext,
 * or any per-account content as a label value — only bounded, low-cardinality
 * dimensions (route templates, result strings, rpc types, error sites). Label
 * values are sanitized on the way out, but the caller is responsible for not
 * feeding secrets in.
 */

/** A bounded set of label name → value pairs for one counter series. */
export type Labels = Record<string, string>

/** Counter names exposed by the relay. */
export type CounterName =
  | 'auth_attempts_total'
  | 'http_requests_total'
  | 'socket_connections_total'
  | 'rpc_total'
  | 'rpc_rejected_total'
  | 'ingest_events_total'
  | 'ingest_rejected_total'
  | 'rate_limited_total'
  | 'runs_deleted_total'
  | 'reconciled_runs_total'
  | 'machines_deleted_total'
  | 'push_sent_total'
  | 'push_pruned_total'
  | 'errors_total'

/** Gauge names exposed by the relay. */
export type GaugeName = 'connected_sockets' | 'connected_machines'

/** Summary names exposed by the relay. */
export type SummaryName = 'http_request_duration_ms'

interface CounterDef {
  help: string
  labelNames: readonly string[]
}

const COUNTERS: Record<CounterName, CounterDef> = {
  auth_attempts_total: {
    help: 'Total authentication attempts by result.',
    labelNames: ['result'],
  },
  http_requests_total: {
    help: 'Total HTTP requests by route and status.',
    labelNames: ['route', 'status'],
  },
  socket_connections_total: {
    help: 'Total socket connections accepted by role.',
    labelNames: ['role'],
  },
  rpc_total: {
    help: 'Total rpc events received by type.',
    labelNames: ['type'],
  },
  rpc_rejected_total: {
    help: 'Total rpc messages rejected by schema validation at the socket boundary.',
    labelNames: [],
  },
  ingest_events_total: {
    help: 'Total wire events ingested.',
    labelNames: [],
  },
  ingest_rejected_total: {
    help: 'Total ingest messages rejected by schema validation at the socket boundary.',
    labelNames: [],
  },
  rate_limited_total: {
    help: 'Total requests rejected by a rate limiter, by path.',
    labelNames: ['path'],
  },
  runs_deleted_total: {
    help: 'Total runs deleted via the run-delete endpoint.',
    labelNames: [],
  },
  reconciled_runs_total: {
    help: 'Total stale runs flipped to idle on a machine reconnect (zombie reconciliation).',
    labelNames: [],
  },
  machines_deleted_total: {
    help: 'Total machines deleted via the machine-delete endpoint.',
    labelNames: [],
  },
  push_sent_total: {
    help: 'Total web-push notifications accepted by the push service, by type.',
    labelNames: ['type'],
  },
  push_pruned_total: {
    help: 'Total dead web-push subscriptions pruned (404/410 from the push service).',
    labelNames: [],
  },
  errors_total: {
    help: 'Total errors by where they occurred.',
    labelNames: ['where'],
  },
}

const GAUGES: Record<GaugeName, string> = {
  connected_sockets: 'Currently connected sockets.',
  connected_machines: 'Currently connected machine sockets.',
}

const SUMMARIES: Record<SummaryName, string> = {
  http_request_duration_ms: 'HTTP request duration in milliseconds.',
}

interface SummaryState {
  count: number
  sum: number
}

const METRIC_NAME = /^[a-zA-Z_][a-zA-Z0-9_]*$/

function escapeLabelValue(v: string): string {
  return v.replace(/\\/g, '\\\\').replace(/\n/g, '\\n').replace(/"/g, '\\"')
}

/**
 * Build the canonical `{label="value",...}` suffix for a series. Label names
 * follow the metric's declared order; values are escaped per the Prometheus
 * exposition format (backslash, double-quote, newline). Missing/unknown labels
 * are dropped rather than rendered empty.
 */
function renderLabels(labelNames: readonly string[], labels: Labels): string {
  const parts: string[] = []
  for (const name of labelNames) {
    const raw = labels[name]
    if (raw === undefined) continue
    parts.push(`${name}="${escapeLabelValue(raw)}"`)
  }
  return parts.length ? `{${parts.join(',')}}` : ''
}

/** Stable key for a labelled series within one counter. */
function seriesKey(labelNames: readonly string[], labels: Labels): string {
  return labelNames.map((n) => `${n}=${labels[n] ?? ''}`).join('\x1f')
}

/**
 * In-process metrics registry. Counters and the summary accumulate; gauges hold
 * the latest value. {@link render} produces Prometheus text.
 */
export class MetricsRegistry {
  private readonly counters = new Map<CounterName, Map<string, { labels: Labels; value: number }>>()
  private readonly gauges = new Map<GaugeName, number>()
  private readonly summaries = new Map<SummaryName, SummaryState>()

  /** Increment a counter series by `amount` (default 1). */
  inc(name: CounterName, labels: Labels = {}, amount = 1): void {
    const def = COUNTERS[name]
    let series = this.counters.get(name)
    if (!series) {
      series = new Map()
      this.counters.set(name, series)
    }
    const key = seriesKey(def.labelNames, labels)
    const entry = series.get(key)
    if (entry) {
      entry.value += amount
    } else {
      // Keep only the declared labels so unknown keys can't sneak into output.
      const kept: Labels = {}
      for (const n of def.labelNames) if (labels[n] !== undefined) kept[n] = labels[n]!
      series.set(key, { labels: kept, value: amount })
    }
  }

  /** Read a counter series value (for tests); `0` if never incremented. */
  get(name: CounterName, labels: Labels = {}): number {
    const series = this.counters.get(name)
    if (!series) return 0
    return series.get(seriesKey(COUNTERS[name].labelNames, labels))?.value ?? 0
  }

  /** Set a gauge to an absolute value. */
  setGauge(name: GaugeName, value: number): void {
    this.gauges.set(name, value)
  }

  /** Adjust a gauge by `delta` (clamped at 0 so it never goes negative). */
  addGauge(name: GaugeName, delta: number): void {
    const next = (this.gauges.get(name) ?? 0) + delta
    this.gauges.set(name, next < 0 ? 0 : next)
  }

  /** Read a gauge value (for tests); `0` if never set. */
  getGauge(name: GaugeName): number {
    return this.gauges.get(name) ?? 0
  }

  /** Observe a value into a summary (count + sum). */
  observe(name: SummaryName, value: number): void {
    if (!Number.isFinite(value)) return
    const s = this.summaries.get(name) ?? { count: 0, sum: 0 }
    s.count += 1
    s.sum += value
    this.summaries.set(name, s)
  }

  /** Read a summary's accumulated state (for tests). */
  getSummary(name: SummaryName): { count: number; sum: number } {
    return { ...(this.summaries.get(name) ?? { count: 0, sum: 0 }) }
  }

  /** Render the whole registry as Prometheus exposition text. */
  render(): string {
    const out: string[] = []

    for (const name of Object.keys(COUNTERS) as CounterName[]) {
      const def = COUNTERS[name]
      out.push(`# HELP ${name} ${def.help}`)
      out.push(`# TYPE ${name} counter`)
      const series = this.counters.get(name)
      if (!series || series.size === 0) {
        // Emit a zero so the series exists even before first increment.
        out.push(`${name} 0`)
        continue
      }
      for (const { labels, value } of series.values()) {
        out.push(`${name}${renderLabels(def.labelNames, labels)} ${value}`)
      }
    }

    for (const name of Object.keys(GAUGES) as GaugeName[]) {
      out.push(`# HELP ${name} ${GAUGES[name]}`)
      out.push(`# TYPE ${name} gauge`)
      out.push(`${name} ${this.gauges.get(name) ?? 0}`)
    }

    for (const name of Object.keys(SUMMARIES) as SummaryName[]) {
      const s = this.summaries.get(name) ?? { count: 0, sum: 0 }
      out.push(`# HELP ${name} ${SUMMARIES[name]}`)
      out.push(`# TYPE ${name} summary`)
      out.push(`${name}_count ${s.count}`)
      out.push(`${name}_sum ${s.sum}`)
    }

    return out.join('\n') + '\n'
  }
}

/** Prometheus exposition content type for the `/metrics` response. */
export const METRICS_CONTENT_TYPE = 'text/plain; version=0.0.4; charset=utf-8'

/** Validate a metric name per the Prometheus naming rules. */
export function isValidMetricName(name: string): boolean {
  return METRIC_NAME.test(name)
}

/** Create a fresh registry. */
export function createMetrics(): MetricsRegistry {
  return new MetricsRegistry()
}
