/**
 * Tiny dependency-free leveled structured logger for the relay.
 *
 * Lines go to stderr as `level ts msg {json-fields}` (a grep-able, mostly-JSON
 * shape). The level is read from `RELAY_LOG` (default `info`); `debug` is off
 * unless explicitly enabled. A sink is injectable so tests can capture records
 * instead of touching stderr.
 *
 * Never pass tokens/secrets/ciphertext as fields — callers are responsible for
 * keeping sensitive material out of the `fields` object.
 */

export type LogLevel = 'debug' | 'info' | 'warn' | 'error'

/** Structured, JSON-serializable log fields. */
export type LogFields = Record<string, unknown>

/** A single emitted record, handed to the sink. */
export interface LogRecord {
  level: LogLevel
  /** Epoch ms. */
  ts: number
  /** Optional logger name (component), e.g. `relay`. */
  name?: string
  msg: string
  fields?: LogFields
  /** Pre-rendered `level ts [name] msg {json}` line (what the default sink writes). */
  line: string
}

/** A sink consumes rendered records; the default writes to stderr. */
export type LogSink = (record: LogRecord) => void

export interface LoggerOptions {
  /** Explicit level; overrides any env-derived level. */
  level?: LogLevel
  /** Env var to read the level from (e.g. `RELAY_LOG`). */
  envVar?: string
  /** Env map to read `envVar` from; defaults to `process.env`. */
  env?: Record<string, string | undefined>
  /** Component name attached to every record. */
  name?: string
  /** Where records go; defaults to a stderr writer. */
  sink?: LogSink
}

export interface Logger {
  debug(msg: string, fields?: LogFields): void
  info(msg: string, fields?: LogFields): void
  warn(msg: string, fields?: LogFields): void
  error(msg: string, fields?: LogFields): void
  /** A child logger that merges `fields` into every record. */
  child(fields: LogFields): Logger
}

const ORDER: Record<LogLevel, number> = { debug: 0, info: 1, warn: 2, error: 3 }

function isLevel(v: string | undefined): v is LogLevel {
  return v === 'debug' || v === 'info' || v === 'warn' || v === 'error'
}

function resolveLevel(opts: LoggerOptions): LogLevel {
  if (opts.level) return opts.level
  if (opts.envVar) {
    const env = opts.env ?? process.env
    const raw = env[opts.envVar]?.toLowerCase()
    if (isLevel(raw)) return raw
  }
  return 'info'
}

const stderrSink: LogSink = (record) => {
  process.stderr.write(record.line + '\n')
}

function render(
  level: LogLevel,
  ts: number,
  name: string | undefined,
  msg: string,
  fields?: LogFields,
): string {
  const prefix = name ? `${level} ${ts} [${name}] ${msg}` : `${level} ${ts} ${msg}`
  if (!fields || Object.keys(fields).length === 0) return prefix
  let json: string
  try {
    json = JSON.stringify(fields)
  } catch {
    json = '{"_error":"unserializable-fields"}'
  }
  return `${prefix} ${json}`
}

/**
 * Build a leveled logger. The threshold is `opts.level`, else the `envVar`
 * value, else `info`. Records at or above the threshold are rendered and passed
 * to the sink; everything else is dropped.
 */
export function createLogger(opts: LoggerOptions = {}): Logger {
  const threshold = ORDER[resolveLevel(opts)]
  const sink = opts.sink ?? stderrSink
  const name = opts.name

  function make(bound?: LogFields): Logger {
    function emit(level: LogLevel, msg: string, fields?: LogFields): void {
      if (ORDER[level] < threshold) return
      const merged = bound && fields ? { ...bound, ...fields } : (fields ?? bound)
      const ts = Date.now()
      const record: LogRecord = {
        level,
        ts,
        ...(name ? { name } : {}),
        msg,
        ...(merged ? { fields: merged } : {}),
        line: render(level, ts, name, msg, merged),
      }
      sink(record)
    }
    return {
      debug: (msg, fields) => emit('debug', msg, fields),
      info: (msg, fields) => emit('info', msg, fields),
      warn: (msg, fields) => emit('warn', msg, fields),
      error: (msg, fields) => emit('error', msg, fields),
      child: (fields) => make(bound ? { ...bound, ...fields } : fields),
    }
  }

  return make()
}

/** A no-op logger for call sites that have no logger injected. */
export function noopLogger(): Logger {
  const self: Logger = {
    debug: () => {},
    info: () => {},
    warn: () => {},
    error: () => {},
    child: () => self,
  }
  return self
}
