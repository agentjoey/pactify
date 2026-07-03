import type { Machine as MachineRow, Prisma, PrismaClient } from '@prisma/client'
import { MachineInfo } from '@pactify/wire'

/** Default machine TTL: 7 days. Offline machines staler than this are swept. */
export const DEFAULT_MACHINE_TTL_MS = 7 * 24 * 60 * 60 * 1000

/**
 * Map a stored `Machine` row to the cleartext {@link MachineInfo} pushed to
 * clients. `agentKinds` and `workdirs` are JSON arrays in the DB; the relay
 * does not interpret their contents, only forwards them.
 */
export function toMachineInfo(row: MachineRow): MachineInfo {
  const agentKinds = Array.isArray(row.agentKinds)
    ? (row.agentKinds as unknown[]).filter((v): v is string => typeof v === 'string')
    : []
  const workdirs = Array.isArray(row.workdirs)
    ? (row.workdirs as unknown[]).filter((v): v is string => typeof v === 'string')
    : undefined
  return MachineInfo.parse({
    machineId: row.id,
    host: row.host ?? undefined,
    agentKinds,
    workdirs,
    online: row.online,
    lastSeenAt: Number(row.lastSeenAt),
  })
}

/** Board hygiene knobs for {@link listMachines}. */
export interface ListMachinesOptions {
  /**
   * If > 0, offline machines whose `lastSeenAt` is older than `now - ttlMs` are
   * excluded from the rail so ghosts drop off even before a sweep deletes them.
   * Online machines are always kept. Omitted/`<= 0` keeps every machine.
   */
  ttlMs?: number
  /** Clock injection for tests; defaults to `Date.now`. */
  now?: () => number
}

/**
 * All machines belonging to an account, ordered by id for stable lists. With
 * `opts.ttlMs > 0`, stale *offline* machines (whose `lastSeenAt` is older than
 * `now - ttlMs`) are filtered out (board hygiene) so ghost machines drop off
 * the rail even before a {@link sweepExpiredMachines} run deletes them. Online
 * and recent machines are always kept.
 */
export async function listMachines(
  db: PrismaClient,
  accountId: string,
  opts: ListMachinesOptions = {},
): Promise<MachineInfo[]> {
  const where: Prisma.MachineWhereInput = { accountId }
  const ttlMs = opts.ttlMs ?? 0
  if (ttlMs > 0) {
    const cutoffMs = (opts.now ?? Date.now)() - ttlMs
    // Exclude offline machines that went stale past the ttl; keep everything else.
    where.NOT = { online: false, lastSeenAt: { lt: BigInt(cutoffMs) } }
  }
  const rows = await db.machine.findMany({ where, orderBy: { id: 'asc' } })
  return rows.map(toMachineInfo)
}

/** Tuning knobs for a machine sweep. */
export interface MachineRetentionOptions {
  /** Offline machines whose `lastSeenAt` is older than this are deleted. `<= 0` disables. */
  ttlMs: number
}

/**
 * Delete *offline* machines whose `lastSeenAt` is older than `now - ttlMs`,
 * returning the number deleted. Online machines (and recently-seen offline
 * ones) are kept. A `ttlMs <= 0` is a no-op (returns 0).
 *
 * FK-safe: `Run.machineId` references a Machine with no `onDelete: Cascade`, so
 * deleting a machine still referenced by a run would FK-fail (and would orphan
 * run queries). Stale machines that still own runs are therefore left in place
 * — their old runs are reaped separately by the D5a run-retention sweep, after
 * which a later machine sweep can collect the now-unreferenced machine.
 *
 * `db` and `now` are injected so the sweep is deterministic and testable.
 */
export async function sweepExpiredMachines(
  db: PrismaClient,
  now: Date,
  opts: MachineRetentionOptions,
): Promise<number> {
  if (opts.ttlMs <= 0) return 0
  const cutoffMs = now.getTime() - opts.ttlMs
  const candidates = await db.machine.findMany({
    where: { online: false, lastSeenAt: { lt: BigInt(cutoffMs) } },
    select: { id: true },
  })
  if (candidates.length === 0) return 0

  // Only delete machines no run references (FK-safe). Filter to ids with zero runs.
  const ids = candidates.map((m) => m.id)
  const referenced = await db.run.findMany({
    where: { machineId: { in: ids } },
    select: { machineId: true },
    distinct: ['machineId'],
  })
  const referencedIds = new Set(referenced.map((r) => r.machineId))
  const deletable = ids.filter((id) => !referencedIds.has(id))
  if (deletable.length === 0) return 0

  const { count } = await db.machine.deleteMany({ where: { id: { in: deletable } } })
  return count
}
