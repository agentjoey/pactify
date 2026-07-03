import { z } from 'zod'

/** Coarse run state — the only state the relay sees in cleartext. */
export const RunState = z.enum([
  'idle',
  'thinking',
  'blocked',
  'awaiting-approval',
  'done',
  'error',
])
export type RunState = z.infer<typeof RunState>

/** What kind of payload the accompanying encrypted body carries. */
export const EventKind = z.enum([
  'snapshot',
  'delta',
  'message',
  'thinking',
  'approval-request',
  'approval-resolved',
  'run-started',
  'run-ended',
])
export type EventKind = z.infer<typeof EventKind>

/**
 * Cleartext operational header (C hybrid model). Machine-readable enums,
 * counters and ids only — NO free text. The relay indexes this to drive the
 * fleet board, approval inbox and push.
 */
export const OperationalHeader = z.object({
  v: z.literal(1),
  machineId: z.string().min(1).max(256),
  runId: z.string().min(1).max(256),
  seq: z.number().int().nonnegative(),
  ts: z.number().int().nonnegative(),
  state: RunState,
  eventKind: EventKind,
  pendingApprovals: z.number().int().nonnegative(),
  tokensIn: z.number().int().nonnegative(),
  tokensOut: z.number().int().nonnegative(),
  costMicros: z.number().int().nonnegative().optional(),
  /**
   * Current git branch of the run's workdir. Low-sensitivity operational
   * metadata (like machineId/workdir), so it rides cleartext here — NOT the
   * encrypted body. Omitted when the workdir is not a git repo / is detached.
   */
  branch: z.string().max(512).optional(),
  /**
   * Epoch ms the run started (its first publish time), stamped once at spawn.
   * Cleartext operational metadata so the web can tick a running timer. Stable
   * across every header for the run; omitted for runs spawned before this field.
   */
  startedAt: z.number().int().optional(),
  /**
   * Absolute, resolved working directory the run was spawned in. Low-sensitivity
   * operational metadata (like machineId/branch), so it rides cleartext here —
   * NOT the encrypted body. Reported exactly once (already resolved at spawn);
   * omitted for runs that carry no workdir (e.g. attach).
   */
  workdir: z.string().max(4096).optional(),
  /**
   * Absolute path to the git repo root of the run's workdir (`git rev-parse
   * --show-toplevel`), resolved once at spawn alongside `branch`/`workdir`. Rides
   * cleartext here (low-sensitivity operational metadata, NOT the encrypted body)
   * so the relay/web can group a run by its PROJECT rather than a per-subdir
   * workdir. Omitted when the workdir is not a git repo / git is missing.
   */
  repoRoot: z.string().max(4096).optional(),
  /**
   * Human-readable session title, set at spawn from the optional spawn `title`.
   * Low-sensitivity operational metadata (like machineId/branch/workdir), so it
   * rides cleartext here — NOT the encrypted body. The relay persists it onto the
   * Run on creation; omitted for runs spawned without a title. Post-spawn renames
   * go through the dedicated rename endpoint, not this header.
   */
  title: z.string().max(512).optional(),
  /**
   * Marks a transient query reply (e.g. `dir-list`, `discovered`, `file-list`)
   * couriered via a one-off `requestId` placed in `runId`. The relay forwards it
   * to the account room for client-side `requestId` matching but MUST NOT persist
   * it as a run/event — otherwise each run-less reply upserts a phantom Run.
   */
  ephemeral: z.boolean().optional(),
})
  .strict()
export type OperationalHeader = z.infer<typeof OperationalHeader>
