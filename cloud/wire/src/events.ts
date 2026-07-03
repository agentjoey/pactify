import { z } from 'zod'
import { RunState } from './operational.js'
import {
  FileListReply,
  ControlsPayload,
  McpPayload,
  DiscoveredReply,
  DirListReply,
  CommandListReply,
} from './reply.js'

/**
 * The normalized agent event — the plaintext body that linxd encrypts into a
 * WireMessage. Every AgentBackend (opencode/claude/codex/ACP) normalizes its
 * native events into this union.
 */
export const AgentMessageEvent = z.object({
  kind: z.literal('message'),
  role: z.enum(['user', 'assistant']),
  text: z.string(),
})
export const AgentDeltaEvent = z.object({
  kind: z.literal('delta'),
  text: z.string(),
  /** Stable id of the assistant part this delta belongs to, so the client can
   * accumulate streaming text per part. */
  partId: z.string().optional(),
})
export const ThinkingEvent = z.object({
  kind: z.literal('thinking'),
  text: z.string(),
  partial: z.boolean().optional(),
})
export const ToolCallEvent = z.object({
  kind: z.literal('tool-call'),
  toolCallId: z.string().min(1),
  name: z.string().min(1),
  args: z.unknown(),
})
export const ToolResultEvent = z.object({
  kind: z.literal('tool-result'),
  toolCallId: z.string().min(1),
  ok: z.boolean(),
  output: z.string().optional(),
})
export const FileDiffEvent = z.object({
  kind: z.literal('diff'),
  path: z.string().min(1),
  patch: z.string(),
})
export const ApprovalRequestEvent = z.object({
  kind: z.literal('approval-request'),
  approvalId: z.string().min(1),
  tool: z.string().min(1),
  detail: z.string(),
})
/**
 * The durable record of an approval's resolution. The decision is otherwise an
 * RPC-only side effect (`resolve-approval` → the backend), so a transcript
 * re-render from persisted events had no way to know an approval was already
 * approved/denied. linxd publishes this through the same E2E-encrypted pipeline
 * as every other run event so a reload replays the resolved state.
 */
export const ApprovalResolvedEvent = z.object({
  kind: z.literal('approval-resolved'),
  approvalId: z.string().min(1),
  decision: z.enum(['approve', 'deny', 'always']),
})
export const StateChangeEvent = z.object({
  kind: z.literal('state-change'),
  state: RunState,
})
export const UsageEvent = z.object({
  kind: z.literal('usage'),
  tokensIn: z.number().int().nonnegative(),
  tokensOut: z.number().int().nonnegative(),
  costMicros: z.number().int().nonnegative().optional(),
  model: z.string().optional(),
  durationMs: z.number().int().nonnegative().optional(),
  cacheReadTokens: z.number().int().nonnegative().optional(),
})
/** The run's current todo/plan list, re-emitted in full whenever it changes. */
export const TodoPayload = z.object({
  kind: z.literal('todo'),
  items: z.array(
    z.object({
      content: z.string().min(1),
      status: z.enum(['pending', 'in_progress', 'completed']),
    }),
  ),
})
export type TodoPayload = z.infer<typeof TodoPayload>

export const AgentEvent = z.discriminatedUnion('kind', [
  AgentMessageEvent,
  AgentDeltaEvent,
  ThinkingEvent,
  ToolCallEvent,
  ToolResultEvent,
  FileDiffEvent,
  ApprovalRequestEvent,
  ApprovalResolvedEvent,
  StateChangeEvent,
  UsageEvent,
  TodoPayload,
  FileListReply,
  ControlsPayload,
  McpPayload,
  DiscoveredReply,
  DirListReply,
  CommandListReply,
])
export type AgentEvent = z.infer<typeof AgentEvent>
