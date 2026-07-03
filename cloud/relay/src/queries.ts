import type { Prisma, PrismaClient, Run, RunEvent } from '@prisma/client'
import type { AgentKind, RunState, RunSummary } from '@pactify-apps/wire'
import { TERMINAL_STATES } from './retention.js'

/** Board hygiene knobs for {@link listRuns}. */
export interface ListRunsOptions {
  /**
   * If > 0, terminal runs (`done`/`error`) whose `lastActiveAt` is older than
   * `now - ttlMs` are excluded from the board so ghosts don't show even before
   * a retention sweep. Non-terminal runs are always kept. Omitted/`<= 0` keeps
   * every run.
   */
  ttlMs?: number
  /** Clock injection for tests; defaults to `Date.now`. */
  now?: () => number
}

/**
 * Fold a stored {@link Run} row into the cleartext {@link RunSummary} the relay
 * pushes to drive board tiles and the inbox. `model` is intentionally
 * `undefined` — the run's model is only stored encrypted (`modelEnc`), so there
 * is no cleartext model column yet. `updatedAt` is the run's `lastActiveAt` as
 * epoch ms.
 */
export function toRunSummary(run: Run): RunSummary {
  return {
    id: run.id,
    agentKind: run.agentKind as AgentKind,
    model: undefined,
    state: run.state as RunState,
    pendingApprovals: run.pendingApprovals,
    tokensIn: run.tokensIn,
    tokensOut: run.tokensOut,
    costMicros: run.costMicros ?? undefined,
    machineId: run.machineId,
    seq: run.seq,
    updatedAt: run.lastActiveAt.getTime(),
    branch: run.branch ?? undefined,
    workdir: run.workdir ?? undefined,
    repoRoot: run.repoRoot ?? undefined,
    startedAt: run.startedAtMs != null ? Number(run.startedAtMs) : undefined,
    title: run.title ?? undefined,
  }
}

/**
 * Fleet board: an account's runs as {@link RunSummary}, most-recently-active
 * first. With `opts.ttlMs > 0`, stale terminal runs are filtered out (board
 * hygiene) so ghosts drop off even before a retention sweep deletes them.
 */
export async function listRuns(
  db: PrismaClient,
  accountId: string,
  opts: ListRunsOptions = {},
): Promise<RunSummary[]> {
  const where: Prisma.RunWhereInput = { accountId }
  const ttlMs = opts.ttlMs ?? 0
  if (ttlMs > 0) {
    const cutoff = new Date((opts.now ?? Date.now)() - ttlMs)
    // Exclude terminal runs that went stale past the ttl; keep everything else.
    where.NOT = { state: { in: [...TERMINAL_STATES] }, lastActiveAt: { lt: cutoff } }
  }
  const runs = await db.run.findMany({ where, orderBy: [{ lastActiveAt: 'desc' }, { id: 'desc' }] })
  return runs.map(toRunSummary)
}

/** Filters for {@link searchRuns}. All optional; omitted fields don't constrain. */
export interface SearchRunsOptions {
  /** Case-insensitive substring matched over runId / branch / machineId / title. */
  q?: string
  /** Exact run state (e.g. `done`, `idle`, `thinking`). */
  state?: string
  /** Exact agent kind (e.g. `claude`, `kimi`). */
  agentKind?: string
  /** Exact machine id. */
  machineId?: string
  /**
   * Recency cursor as epoch ms: only runs whose `lastActiveAt` is strictly
   * older than this are returned. Feed back {@link SearchRunsResult.nextCursor}
   * for the next page.
   */
  before?: number
  /** Page size. Defaults to {@link DEFAULT_HISTORY_LIMIT}, capped at {@link MAX_HISTORY_LIMIT}. */
  limit?: number
}

/** A page of history results plus the next-page cursor (epoch ms), if more remain. */
export interface SearchRunsResult {
  runs: RunSummary[]
  /** `lastActiveAt` (epoch ms) of the last row; pass as `before` to fetch the next page. */
  nextCursor?: number
}

/** Default history page size when none is requested. */
export const DEFAULT_HISTORY_LIMIT = 50
/** Hard cap on a history page, so a client can't request an unbounded scan. */
export const MAX_HISTORY_LIMIT = 50

/**
 * Durable run history for an account: every run (including terminal/old ones),
 * NOT subject to the board's retention exclusion. Account-scoped via the D5c
 * `Run(accountId, *)` indexes, most-recently-active first, with optional
 * substring/exact filters and cursor pagination.
 *
 * The substring `q` is matched case-insensitively over `id`, `branch`,
 * `machineId`, and `title` (the run's human label). `before` is an exclusive
 * `lastActiveAt` cursor (epoch ms). The
 * page is `min(limit ?? DEFAULT, MAX)`; if a full page comes back, `nextCursor`
 * is the last row's `lastActiveAt` so the caller can page on.
 */
export async function searchRuns(
  db: PrismaClient,
  accountId: string,
  opts: SearchRunsOptions = {},
): Promise<SearchRunsResult> {
  const requested = opts.limit ?? DEFAULT_HISTORY_LIMIT
  const limit =
    Number.isFinite(requested) && requested > 0
      ? Math.min(Math.floor(requested), MAX_HISTORY_LIMIT)
      : DEFAULT_HISTORY_LIMIT

  const where: Prisma.RunWhereInput = { accountId }
  if (opts.state !== undefined) where.state = opts.state
  if (opts.agentKind !== undefined) where.agentKind = opts.agentKind
  if (opts.machineId !== undefined) where.machineId = opts.machineId
  if (opts.before !== undefined) where.lastActiveAt = { lt: new Date(opts.before) }
  if (opts.q) {
    const contains = opts.q
    where.OR = [
      { id: { contains, mode: 'insensitive' } },
      { branch: { contains, mode: 'insensitive' } },
      { machineId: { contains, mode: 'insensitive' } },
      // The title is the run's human label (board/console/history all show it),
      // so it must be searchable too — otherwise a user can't find a run by the
      // name they gave it and see everywhere (display-source inconsistency).
      { title: { contains, mode: 'insensitive' } },
    ]
  }

  // Fetch one extra row to detect whether another page exists.
  const rows = await db.run.findMany({
    where,
    orderBy: [{ lastActiveAt: 'desc' }, { id: 'desc' }],
    take: limit + 1,
  })
  const hasMore = rows.length > limit
  const page = hasMore ? rows.slice(0, limit) : rows
  const runs = page.map(toRunSummary)
  const last = page[page.length - 1]
  // The `id`-tiebroken orderBy above makes ordering deterministic. NOTE: the
  // cursor is still a bare `lastActiveAt` ms, so runs sharing the exact boundary
  // ms can be skipped across pages. Fully fixing that needs a composite
  // (lastActiveAt, id) cursor, which changes the history API contract — a
  // coordinated change with linx-web (tracked in backlog).
  const nextCursor = hasMore && last ? last.lastActiveAt.getTime() : undefined
  return { runs, nextCursor }
}

/** Approval inbox: an account's runs awaiting approval, most-recently-active first. */
export async function listPendingApprovals(
  db: PrismaClient,
  accountId: string,
): Promise<RunSummary[]> {
  const runs = await db.run.findMany({
    where: { accountId, pendingApprovals: { gt: 0 } },
    orderBy: [{ lastActiveAt: 'desc' }, { id: 'desc' }],
  })
  return runs.map(toRunSummary)
}

/** Client catch-up: a run's events with seq > afterSeq, ascending. */
export async function getRunEventsAfter(
  db: PrismaClient,
  runId: string,
  afterSeq: number,
): Promise<RunEvent[]> {
  return db.runEvent.findMany({
    where: { runId, seq: { gt: afterSeq } },
    orderBy: { seq: 'asc' },
  })
}
