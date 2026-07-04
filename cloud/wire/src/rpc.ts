import { z } from 'zod'

/** Agent kinds linxd can drive. */
export const AgentKind = z.enum(['opencode', 'claude', 'codex', 'kimi', 'gemini'])
export type AgentKind = z.infer<typeof AgentKind>

export const SpawnRequest = z.object({
  type: z.literal('spawn'),
  machineId: z.string().min(1),
  agentKind: AgentKind,
  workdir: z.string().min(1),
  model: z.string().optional(),
  task: z.string().optional(),
  /**
   * Optional human-readable session name, set at creation. Persisted as the
   * run's operational `title` (the same {@link RenameRequest} surfaces post-spawn),
   * so the board/console can name the session without a rename round-trip.
   */
  title: z.string().max(200).optional(),
})
/** An image attachment carried inline with a message (base64 body + mime). */
export const InlineImage = z.object({
  data: z.string().min(1).max(12_000_000),
  mimeType: z.string().min(1).max(256),
})
export type InlineImage = z.infer<typeof InlineImage>

export const SendMessageRequest = z.object({
  type: z.literal('send-message'),
  runId: z.string().min(1),
  text: z.string().max(256_000),
  images: z.array(InlineImage).max(32).optional(),
})
export const ResolveApprovalRequest = z.object({
  type: z.literal('resolve-approval'),
  runId: z.string().min(1),
  approvalId: z.string().min(1),
  decision: z.enum(['approve', 'deny', 'always']),
})
export const AbortRequest = z.object({
  type: z.literal('abort'),
  runId: z.string().min(1),
})
export const TakeoverRequest = z.object({
  type: z.literal('takeover'),
  runId: z.string().min(1),
  mode: z.enum(['local', 'remote']),
})
export const ListFilesRequest = z.object({
  type: z.literal('list-files'),
  runId: z.string().min(1),
  requestId: z.string().min(1),
  query: z.string(),
})
export const SetModelRequest = z.object({
  type: z.literal('set-model'),
  runId: z.string().min(1),
  modelId: z.string().min(1),
})
export const SetModeRequest = z.object({
  type: z.literal('set-mode'),
  runId: z.string().min(1),
  modeId: z.string().min(1),
})
/**
 * Ask a machine to enumerate its already-running, attachable agent sessions.
 * The reply is couriered as an encrypted `discovered` AgentEvent matched by
 * `requestId` (see {@link DiscoveredReply}).
 */
export const DiscoverRequest = z.object({
  type: z.literal('discover'),
  machineId: z.string().min(1),
  requestId: z.string().min(1),
})
/**
 * Ask a machine to enumerate the sub-directories of `path` on its filesystem
 * (defaulting to the machine's primary workdir when omitted). Run-less and
 * machineId-routed like {@link DiscoverRequest}; the reply is couriered as an
 * encrypted `dir-list` AgentEvent matched by `requestId` (see DirListReply).
 * Used by the new-session workdir browser — directory navigation only, never
 * reads file contents.
 */
export const ListDirsRequest = z.object({
  type: z.literal('list-dirs'),
  requestId: z.string().min(1),
  machineId: z.string().min(1),
  path: z.string().optional(),
})
/** Take over an existing host session, spawning an attached run for it. */
export const AttachRequest = z.object({
  type: z.literal('attach'),
  machineId: z.string().min(1),
  agentKind: AgentKind,
  sessionId: z.string().min(1),
})
/**
 * Ask a run to enumerate the slash-commands its backend exposes. The reply is
 * couriered as an encrypted `command-list` AgentEvent matched by `requestId`
 * (see {@link CommandListReply}).
 */
export const ListCommandsRequest = z.object({
  type: z.literal('list-commands'),
  runId: z.string().min(1),
  requestId: z.string().min(1),
})
export type ListCommandsRequest = z.infer<typeof ListCommandsRequest>
/** Run one of the backend's slash-commands by name. */
export const RunCommandRequest = z.object({
  type: z.literal('run-command'),
  runId: z.string().min(1),
  name: z.string().min(1),
})
export type RunCommandRequest = z.infer<typeof RunCommandRequest>
/** Rename a run (sets its operational `title`). */
export const RenameRequest = z.object({
  type: z.literal('rename'),
  runId: z.string().min(1),
  title: z.string().min(1).max(200),
})
export type RenameRequest = z.infer<typeof RenameRequest>

// ── pactify pact-verb control messages ──────────────────────────────────────
// A remote control plane (the hosted dashboard, or another machine) drives pact
// coordination on a target machine's LOCAL engine. Additive to the union: linxd
// ignores `pact.*` types (its handler has no case for them), pactify serve
// handles them (see internal/remoteexec). The relay routes by `machineId` and
// scopes delivery by the socket's authenticated account — identical to the linx
// machine RPCs above, so no relay routing change is needed. Reply on RpcResponse.

/** Assign a pact task on the target machine's project. */
export const PactAssignRequest = z.object({
  type: z.literal('pact.assign'),
  machineId: z.string().min(1),
  project: z.string().min(1),
  task: z.string().min(1),
  feature: z.string().min(1),
  branch: z.string().min(1),
  owner: z.string().min(1),
  reviewer: z.string().min(1),
  spec: z.string(),
  deps: z.array(z.string()).optional(),
})
export type PactAssignRequest = z.infer<typeof PactAssignRequest>

/** Accept a pact task (reviewer). */
export const PactAcceptRequest = z.object({
  type: z.literal('pact.accept'),
  machineId: z.string().min(1),
  project: z.string().min(1),
  task: z.string().min(1),
})
export type PactAcceptRequest = z.infer<typeof PactAcceptRequest>

/** Request changes on a pact task (reviewer). */
export const PactChangesRequest = z.object({
  type: z.literal('pact.changes'),
  machineId: z.string().min(1),
  project: z.string().min(1),
  task: z.string().min(1),
  reason: z.string(),
})
export type PactChangesRequest = z.infer<typeof PactChangesRequest>

/** Merge a pact feature (all tasks accepted). */
export const PactMergeRequest = z.object({
  type: z.literal('pact.merge'),
  machineId: z.string().min(1),
  project: z.string().min(1),
  feature: z.string().min(1),
})
export type PactMergeRequest = z.infer<typeof PactMergeRequest>

/** Checkpoint a pact task with evidence (owner). */
export const PactCheckpointRequest = z.object({
  type: z.literal('pact.checkpoint'),
  machineId: z.string().min(1),
  project: z.string().min(1),
  task: z.string().min(1),
  evidence: z.string(),
})
export type PactCheckpointRequest = z.infer<typeof PactCheckpointRequest>

/** client → relay → linxd (or pactify serve) control messages. */
export const RpcRequest = z.discriminatedUnion('type', [
  SpawnRequest,
  SendMessageRequest,
  ResolveApprovalRequest,
  AbortRequest,
  TakeoverRequest,
  ListFilesRequest,
  SetModelRequest,
  SetModeRequest,
  DiscoverRequest,
  ListDirsRequest,
  AttachRequest,
  ListCommandsRequest,
  RunCommandRequest,
  RenameRequest,
  PactAssignRequest,
  PactAcceptRequest,
  PactChangesRequest,
  PactMergeRequest,
  PactCheckpointRequest,
])
export type RpcRequest = z.infer<typeof RpcRequest>

export const RpcResponse = z.object({
  ok: z.boolean(),
  runId: z.string().optional(),
  error: z.string().optional(),
})
export type RpcResponse = z.infer<typeof RpcResponse>
