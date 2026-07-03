import { z } from 'zod'
import type { PrismaClient, Project, PactEvent } from '@prisma/client'

// Wire contract for POST /v1/pact/ingest (docs/specs/agentworks-wire.md — pactify
// overlay). Cleartext operational header + opaque encrypted body. Bounds are
// generous DoS caps (a conforming pactify machine stays far under).
export const PactIngestRequest = z
  .object({
    projectId: z.string().min(1).max(256),
    name: z.string().min(1).max(256),
    feature: z.string().max(256).optional(),
    eventType: z.string().min(1).max(64),
    task: z.string().max(256).optional(),
    seq: z.number().int().nonnegative(),
    ts: z.number().int().nonnegative(),
    bodyEnc: z.string().min(1).max(2_000_000),
  })
  .strict()
export type PactIngestRequest = z.infer<typeof PactIngestRequest>

/**
 * U2 Mission Control data layer: ingest/read pact protocol events uploaded from
 * pactify machines. Same zero-knowledge shape as the run store — cleartext
 * operational columns (eventType/task/feature/seq/ts + project name) drive the
 * cross-machine board; `bodyEnc` is the opaque E2E ciphertext of the full pact
 * event (spec/evidence). Additive: linx never touches these tables.
 *
 * `accountId` is resolved by the caller (the auth layer). `projectId` is expected
 * to be account-derived (e.g. `${accountId}:${nameSlug}`) so it is inherently
 * account-scoped; the updates below re-assert `accountId` as defense in depth.
 */
export interface PactEventInput {
  projectId: string
  /** Cleartext project/repo label. */
  name: string
  /** Current feature id (cleartext operational), or null. */
  feature?: string | null
  /** Cleartext pact verb: assign/checkpoint/accept/merge/... */
  eventType: string
  /** Cleartext task id (operational slug), or null. */
  task?: string | null
  /** Monotonic per-project sequence. */
  seq: number
  /** Epoch ms. */
  ts: number
  /** Opaque E2E ciphertext of the full pact event JSON. */
  bodyEnc: string
}

export async function ingestPactEvent(
  db: PrismaClient,
  accountId: string,
  input: PactEventInput,
): Promise<void> {
  await db.$transaction(async (tx) => {
    // Establish the project row (create-or-noop). The Account row must already
    // exist (auth provisions it); a bad accountId FK-fails, which is correct.
    await tx.project.upsert({
      where: { id: input.projectId },
      create: {
        id: input.projectId,
        accountId,
        name: input.name,
        feature: input.feature ?? null,
        seq: input.seq,
      },
      update: {},
    })
    // Advance the project summary only for a non-stale event (seq at least as new)
    // AND only within this account — an out-of-order event must not roll the
    // summary back, and a projectId collision must not cross accounts.
    await tx.project.updateMany({
      where: { id: input.projectId, accountId, seq: { lte: input.seq } },
      data: {
        name: input.name,
        feature: input.feature ?? null,
        seq: input.seq,
        lastEventAt: new Date(),
      },
    })
    // Idempotent append: a duplicate/retried (projectId, seq) is a no-op instead
    // of a unique-constraint crash (mirrors the run ingest).
    await tx.pactEvent.upsert({
      where: { projectId_seq: { projectId: input.projectId, seq: input.seq } },
      create: {
        projectId: input.projectId,
        accountId,
        seq: input.seq,
        eventType: input.eventType,
        task: input.task ?? null,
        feature: input.feature ?? null,
        ts: BigInt(input.ts),
        bodyEnc: input.bodyEnc,
      },
      update: {},
    })
  })
}

/** Read one Project by id, or `null` if it does not exist. */
export async function getProject(db: PrismaClient, projectId: string): Promise<Project | null> {
  return db.project.findUnique({ where: { id: projectId } })
}

/** An account's projects, most-recently-active first (board listing). */
export async function listProjects(db: PrismaClient, accountId: string): Promise<Project[]> {
  return db.project.findMany({
    where: { accountId },
    orderBy: [{ lastEventAt: 'desc' }, { id: 'desc' }],
  })
}

export interface GetProjectEventsOptions {
  /** Return only events with seq strictly greater than this (catch-up/replay). */
  afterSeq?: number
  /** Cap the number of events returned. */
  limit?: number
}

/**
 * A project's events in seq order — the full replay stream the board projects
 * into a read-only view (the pactify frontend derive functions fold it).
 */
export async function getProjectEvents(
  db: PrismaClient,
  projectId: string,
  opts: GetProjectEventsOptions = {},
): Promise<PactEvent[]> {
  const where: { projectId: string; seq?: { gt: number } } = { projectId }
  if (opts.afterSeq !== undefined) where.seq = { gt: opts.afterSeq }
  return db.pactEvent.findMany({
    where,
    orderBy: { seq: 'asc' },
    ...(opts.limit !== undefined ? { take: opts.limit } : {}),
  })
}
