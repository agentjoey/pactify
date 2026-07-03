import type { PrismaClient, Run, RunEvent } from '@prisma/client'
import type { WireMessage } from '@pactify-apps/wire'

/**
 * Ingest one {@link WireMessage} into the zero-knowledge store:
 *
 *  - Upsert the Run by `header.runId`, projecting the cleartext operational
 *    header into queryable columns (state, counters, seq, lastActiveAt).
 *  - Append the encrypted body as a {@link RunEvent}; `bodyEnc` is the opaque
 *    ciphertext envelope (`JSON.stringify(msg.body)`) — the server never reads
 *    or stores any plaintext.
 *
 * `accountId` and `agentKind` are resolved by the caller (the auth layer). The
 * `Account` and `Machine` rows are auto-provisioned here (no-op if they already
 * exist) so a first-time linxd connect never FK-fails on the Run upsert.
 */
export async function ingestWireMessage(
  db: PrismaClient,
  accountId: string,
  agentKind: string,
  msg: WireMessage,
): Promise<void> {
  const { header } = msg
  const bodyEnc = JSON.stringify(msg.body)

  // Auto-provision the tenant rows before the Run upsert. Both are no-ops when
  // the row already exists; placeholders for the required columns (publicKey,
  // metadataEnc) are only used on first-create and overwritten by the real
  // auth/machine flows. `accountId`-keyed publicKey keeps the unique constraint
  // satisfied without colliding across accounts.
  await db.account.upsert({
    where: { id: accountId },
    create: { id: accountId, publicKey: `provisional:${accountId}` },
    update: {},
  })
  await db.machine.upsert({
    where: { id: header.machineId },
    create: { id: header.machineId, accountId, metadataEnc: '' },
    update: {},
  })

  await db.run.upsert({
    where: { id: header.runId },
    create: {
      id: header.runId,
      accountId,
      machineId: header.machineId,
      agentKind,
      state: header.state,
      pendingApprovals: header.pendingApprovals,
      tokensIn: header.tokensIn,
      tokensOut: header.tokensOut,
      costMicros: header.costMicros ?? null,
      seq: header.seq,
      branch: header.branch ?? null,
      workdir: header.workdir ?? null,
      repoRoot: header.repoRoot ?? null,
      // Session title rides the header from spawn; persisted once on creation.
      // It is NOT updated below so a post-spawn rename (own endpoint) is never
      // clobbered by subsequent header ingests.
      title: header.title ?? null,
      startedAtMs: header.startedAt !== undefined ? BigInt(header.startedAt) : null,
    },
    update: {
      state: header.state,
      pendingApprovals: header.pendingApprovals,
      tokensIn: header.tokensIn,
      tokensOut: header.tokensOut,
      costMicros: header.costMicros ?? null,
      seq: header.seq,
      branch: header.branch ?? null,
      workdir: header.workdir ?? null,
      repoRoot: header.repoRoot ?? null,
      startedAtMs: header.startedAt !== undefined ? BigInt(header.startedAt) : null,
      lastActiveAt: new Date(),
    },
  })

  await db.runEvent.create({
    data: {
      runId: header.runId,
      seq: header.seq,
      state: header.state,
      eventKind: header.eventKind,
      ts: BigInt(header.ts),
      bodyEnc,
    },
  })
}

/** Read one Run by id, or `null` if it does not exist. */
export async function getRun(db: PrismaClient, runId: string): Promise<Run | null> {
  return db.run.findUnique({ where: { id: runId } })
}

/**
 * Non-terminal, *active* run states — the ones that drive an animated progress
 * indicator on the board. A run sitting in one of these has no in-memory linxd
 * handle behind it after a restart/crash, so it is a "zombie": the spinner runs
 * forever though nothing is executing. `idle` is non-terminal too but already
 * quiescent (nothing to reconcile); `done`/`error` are terminal (see
 * {@link TERMINAL_STATES} in retention).
 */
const RECONCILABLE_STATES = ['thinking', 'blocked', 'awaiting-approval'] as const

/**
 * Reconnect reconciliation: flip an account+machine's stale active runs to
 * `idle`.
 *
 * linxd keeps run handles in memory; a restart/crash loses them, but the relay
 * still persists those runs in a non-terminal active state
 * ({@link RECONCILABLE_STATES}). When that machine reconnects we know nothing is
 * driving them, so we mark them `idle` (stale) and return the updated rows so
 * the caller can broadcast a `run-updated` for each and clear the board
 * spinner.
 *
 * Account- AND machine-scoped: only this machine's own runs under this account
 * are touched — never another account's or another machine's. Terminal
 * (`done`/`error`) and already-`idle` runs are left untouched. A genuinely-live
 * run is self-correcting: its next ingested event restores the real state, and
 * resume-on-rpc re-drives it on the next message. `lastActiveAt` is left intact
 * so reconciliation never re-sorts the board.
 *
 * Returns the updated {@link Run} rows (empty when nothing was stale).
 */
export async function reconcileMachineRuns(
  db: PrismaClient,
  accountId: string,
  machineId: string,
): Promise<Run[]> {
  const stale = await db.run.findMany({
    where: { accountId, machineId, state: { in: [...RECONCILABLE_STATES] } },
    select: { id: true },
  })
  if (stale.length === 0) return []
  const ids = stale.map((r) => r.id)
  // Flip state only (not lastActiveAt) so the board keeps its ordering.
  await db.run.updateMany({ where: { id: { in: ids } }, data: { state: 'idle' } })
  return db.run.findMany({ where: { id: { in: ids } } })
}

/** Read a Run's events in append (seq) order. */
export async function getRunEvents(db: PrismaClient, runId: string): Promise<RunEvent[]> {
  return db.runEvent.findMany({ where: { runId }, orderBy: { seq: 'asc' } })
}

/**
 * Account-scoped rename of a Run's operational `title`.
 *
 * Returns `true` if the run existed under `accountId` and was renamed, `false`
 * otherwise (unknown id, or a run owned by another account). Title is cleartext
 * operational metadata; it is not encrypted.
 */
export async function renameRun(
  db: PrismaClient,
  accountId: string,
  runId: string,
  title: string,
): Promise<boolean> {
  const result = await db.run.updateMany({
    where: { id: runId, accountId },
    data: { title },
  })
  return result.count > 0
}

/**
 * Account-scoped delete of a Run and its events in a single transaction.
 *
 * Returns `true` if the run existed under `accountId` and was deleted, `false`
 * otherwise (unknown id, or a run owned by another account — the scoping makes
 * a foreign-account delete indistinguishable from a missing run, so the HTTP
 * layer can surface a uniform 404). Events are removed first to respect the
 * `RunEvent -> Run` FK, which has no `onDelete: Cascade`.
 */
export async function deleteRun(
  db: PrismaClient,
  accountId: string,
  runId: string,
): Promise<boolean> {
  return db.$transaction(async (tx) => {
    const run = await tx.run.findFirst({ where: { id: runId, accountId }, select: { id: true } })
    if (!run) return false
    await tx.runEvent.deleteMany({ where: { runId } })
    await tx.run.delete({ where: { id: runId } })
    return true
  })
}

/**
 * Account-scoped delete of a Machine and everything it owns, in one transaction.
 *
 * Cascade order is FK-safe — `RunEvent -> Run -> Machine` — the same order the
 * ghost-cleanup / retention sweeps respect, since none of these relations
 * declares `onDelete: Cascade`: every event of every run on the machine is
 * removed first, then the runs, then the machine row itself.
 *
 * Returns `true` if the machine existed under `accountId` and was deleted,
 * `false` otherwise (unknown id, or a machine owned by another account — the
 * scoping makes a foreign-account delete indistinguishable from a missing
 * machine, so the HTTP layer can surface a uniform 404).
 *
 * This is a deliberate manual operation: it cascades regardless of the
 * machine's `online`/presence state or whether its runs are live (non-terminal),
 * mirroring {@link deleteRun}, which removes a run irrespective of its state.
 * The caller is the account owner asking to forget the machine.
 */
export async function deleteMachine(
  db: PrismaClient,
  accountId: string,
  machineId: string,
): Promise<boolean> {
  return db.$transaction(async (tx) => {
    const machine = await tx.machine.findFirst({
      where: { id: machineId, accountId },
      select: { id: true },
    })
    if (!machine) return false
    const runs = await tx.run.findMany({ where: { machineId }, select: { id: true } })
    const runIds = runs.map((r) => r.id)
    // FK-safe cascade RunEvent -> Run -> Machine (no onDelete: Cascade on any hop).
    if (runIds.length > 0) {
      await tx.runEvent.deleteMany({ where: { runId: { in: runIds } } })
      await tx.run.deleteMany({ where: { id: { in: runIds } } })
    }
    await tx.machine.delete({ where: { id: machineId } })
    return true
  })
}
