/**
 * Run/event retention (D5a).
 *
 * Relay metadata + ciphertext bodies are otherwise kept forever, so stale
 * terminal runs pile up and pollute the board (the `mac-studio` ghost-run
 * incident). This module expires old *terminal* runs (and their events) on a
 * periodic sweep and lets the board query hide them even before a sweep runs.
 *
 * No schema change: it keys off the existing `state` + `lastActiveAt` columns.
 */

import type { PrismaClient, Prisma } from '@prisma/client'
import type { Logger } from './log.js'
import { sweepExpiredMachines, DEFAULT_MACHINE_TTL_MS } from './machines.js'

/** Run states the relay considers terminal — eligible for expiry. */
export const TERMINAL_STATES = ['done', 'error'] as const

/** Default run TTL: 7 days. Terminal runs older than this are swept. */
export const DEFAULT_RUN_TTL_MS = 7 * 24 * 60 * 60 * 1000

/** Default sweep cadence: every hour. */
export const DEFAULT_SWEEP_INTERVAL_MS = 60 * 60 * 1000

/** Tuning knobs for a sweep. */
export interface RetentionOptions {
  /** Terminal runs whose `lastActiveAt` is older than this are deleted. `<= 0` disables. */
  ttlMs: number
}

/**
 * Delete terminal-state runs (`done`/`error`) whose `lastActiveAt` is older than
 * `now - ttlMs`, plus their events, in a single transaction (events first to
 * respect the RunEvent -> Run FK, which has no `onDelete: Cascade`). Returns the
 * number of runs deleted.
 *
 * `db` and `now` are injected so the sweep is deterministic and testable. A
 * `ttlMs <= 0` is a no-op (returns 0) — retention is off.
 */
export async function sweepExpired(
  db: PrismaClient,
  now: Date,
  opts: RetentionOptions,
): Promise<number> {
  if (opts.ttlMs <= 0) return 0
  const cutoff = new Date(now.getTime() - opts.ttlMs)
  const where: Prisma.RunWhereInput = {
    state: { in: [...TERMINAL_STATES] },
    lastActiveAt: { lt: cutoff },
  }

  // Batch the delete so a large backlog of expired runs can't load every id into
  // memory or bloat one transaction. Each batch is its own transaction; the next
  // findMany naturally skips the just-deleted rows.
  const BATCH = 1000
  let total = 0
  for (;;) {
    const removed = await db.$transaction(async (tx) => {
      const expired = await tx.run.findMany({ where, select: { id: true }, take: BATCH })
      if (expired.length === 0) return 0
      const ids = expired.map((r) => r.id)
      await tx.runEvent.deleteMany({ where: { runId: { in: ids } } })
      const { count } = await tx.run.deleteMany({ where: { id: { in: ids } } })
      return count
    })
    total += removed
    if (removed < BATCH) break
  }
  return total
}

/** A started sweeper; call `stop()` to clear the interval. */
export interface RetentionSweeper {
  stop(): void
}

export interface StartRetentionOptions {
  /** Run TTL in ms; `<= 0` disables run retention. */
  ttlMs?: number
  /** Machine TTL in ms; `<= 0` disables the machine sweep. */
  machineTtlMs?: number
  /** Sweep cadence in ms. */
  intervalMs?: number
  /** Clock injection for tests; defaults to `Date.now`. */
  now?: () => number
  /** Logger for the structured "retention sweep" / "machine sweep" info lines. */
  logger?: Logger
}

/**
 * Start a periodic retention sweep on the relay. Each tick runs the run sweep
 * ({@link sweepExpired}) and the machine sweep ({@link sweepExpiredMachines}),
 * logging a structured info line per sweep that deleted anything. Run retention
 * is off when `ttlMs <= 0`; the machine sweep is off when `machineTtlMs <= 0`.
 * Returns a no-op sweeper only when *both* are disabled. The interval is
 * `unref`'d so it never keeps the process alive.
 */
export function startRetention(
  db: PrismaClient,
  opts: StartRetentionOptions = {},
): RetentionSweeper {
  const ttlMs = opts.ttlMs ?? DEFAULT_RUN_TTL_MS
  const machineTtlMs = opts.machineTtlMs ?? DEFAULT_MACHINE_TTL_MS
  if (ttlMs <= 0 && machineTtlMs <= 0) return { stop() {} }
  const intervalMs = opts.intervalMs ?? DEFAULT_SWEEP_INTERVAL_MS
  const clock = opts.now ?? Date.now
  const log = opts.logger

  const tick = async (): Promise<void> => {
    const now = new Date(clock())
    if (ttlMs > 0) {
      try {
        const deleted = await sweepExpired(db, now, { ttlMs })
        if (deleted > 0) log?.info('retention sweep', { deleted, ttlMs })
      } catch (err) {
        log?.error('retention sweep failed', { error: String(err) })
      }
    }
    if (machineTtlMs > 0) {
      try {
        const deleted = await sweepExpiredMachines(db, now, { ttlMs: machineTtlMs })
        if (deleted > 0) log?.info('machine sweep', { deleted, ttlMs: machineTtlMs })
      } catch (err) {
        log?.error('machine sweep failed', { error: String(err) })
      }
    }
  }

  const handle = setInterval(() => void tick(), intervalMs)
  handle.unref?.()
  return {
    stop() {
      clearInterval(handle)
    },
  }
}
