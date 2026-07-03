import { z } from 'zod'
import { AgentKind } from './rpc.js'
import { RunState } from './operational.js'

/**
 * Wire-local mirror of @pactify/core's `BackendCapabilities`. wire must NOT
 * depend on core, so this is a zod schema with all capability flags optional
 * plus an optional model list. Keep field names in sync with core.
 */
export const BackendCapabilitiesLite = z
  .object({
    diff: z.boolean(),
    approvals: z.boolean(),
    sessionResume: z.boolean(),
    takeover: z.boolean(),
    mcp: z.boolean(),
    imageInput: z.boolean(),
    usage: z.boolean(),
  })
  .partial()
  .extend({
    models: z.array(z.string()).optional(),
  })
export type BackendCapabilitiesLite = z.infer<typeof BackendCapabilitiesLite>

/**
 * Cleartext fleet-board summary of a run, folded from the operational header.
 * This is what the relay indexes and pushes to drive tiles and the inbox.
 */
export const RunSummary = z.object({
  id: z.string(),
  agentKind: AgentKind,
  model: z.string().optional(),
  state: RunState,
  pendingApprovals: z.number().int(),
  tokensIn: z.number().int(),
  tokensOut: z.number().int(),
  costMicros: z.number().int().optional(),
  machineId: z.string(),
  seq: z.number().int(),
  updatedAt: z.number(),
  capabilities: BackendCapabilitiesLite.optional(),
  /** Current git branch of the run's workdir (cleartext operational metadata). */
  branch: z.string().optional(),
  /** Epoch ms the run started, stamped at spawn (cleartext operational metadata). */
  startedAt: z.number().int().optional(),
  /** Absolute, resolved workdir the run was spawned in (cleartext operational metadata). */
  workdir: z.string().optional(),
  /**
   * Absolute path to the git repo root of the run's workdir (`git rev-parse
   * --show-toplevel`), so a run is attributed to its PROJECT rather than a
   * per-subdir workdir. Cleartext operational metadata exactly like `workdir`/
   * `branch`; omitted when the workdir is not a git repo.
   */
  repoRoot: z.string().optional(),
  /** Human-readable run title, renamable by the user (cleartext operational metadata). */
  title: z.string().optional(),
})
export type RunSummary = z.infer<typeof RunSummary>
