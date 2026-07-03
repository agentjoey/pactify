import { z } from 'zod'
import { AgentKind } from './rpc.js'

/**
 * E2E reply event bodies. The relay is zero-knowledge, so interactive query
 * replies (file listing, controls, mcp) are couriered as normal encrypted
 * AgentEvents through the acct room and matched back to their request by
 * `requestId`. These kinds are added to the AgentEvent union in events.ts so
 * they flow through the existing encrypted pipeline.
 */

/** Reply to a `list-files` RPC: workdir-relative paths matching the query. */
export const FileListReply = z.object({
  kind: z.literal('file-list'),
  requestId: z.string().min(1),
  files: z.array(z.string()),
})
export type FileListReply = z.infer<typeof FileListReply>

/** Available models/modes for a run, emitted by linxd on spawn. */
export const ControlsPayload = z.object({
  kind: z.literal('controls'),
  models: z.array(z.string()),
  modes: z.array(z.string()),
  currentModel: z.string().optional(),
  currentMode: z.string().optional(),
})
export type ControlsPayload = z.infer<typeof ControlsPayload>

/** Configured MCP servers for a run, emitted best-effort by linxd on spawn. */
export const McpPayload = z.object({
  kind: z.literal('mcp'),
  servers: z.array(
    z.object({
      name: z.string().min(1),
      status: z.enum(['configured', 'disabled']),
    }),
  ),
})
export type McpPayload = z.infer<typeof McpPayload>

/** One attachable host session surfaced by a `discover` RPC. */
export const DiscoveredSessionInfo = z.object({
  id: z.string().min(1),
  agentKind: AgentKind,
  title: z.string().optional(),
  workdir: z.string().optional(),
})
export type DiscoveredSessionInfo = z.infer<typeof DiscoveredSessionInfo>

/**
 * Reply to a `discover` RPC: the machine's already-running, attachable agent
 * sessions. Like `file-list`, it is couriered as an encrypted AgentEvent and
 * matched back to its request by `requestId`.
 */
export const DiscoveredReply = z.object({
  kind: z.literal('discovered'),
  requestId: z.string().min(1),
  sessions: z.array(DiscoveredSessionInfo),
})
export type DiscoveredReply = z.infer<typeof DiscoveredReply>

/** One sub-directory surfaced by a `list-dirs` RPC: its name + absolute path. */
export const DirEntry = z.object({
  name: z.string().min(1),
  path: z.string().min(1),
})
export type DirEntry = z.infer<typeof DirEntry>

/**
 * Reply to a `list-dirs` RPC: the sub-directories of `path` (directories only —
 * never files), `path` being the absolute directory listed and `parent` its
 * parent for up-navigation (absent at the filesystem root). Directory names are
 * user content, so — like `file-list`/`discovered` — this is couriered as an
 * encrypted AgentEvent and matched back to its request by `requestId`.
 */
export const DirListReply = z.object({
  kind: z.literal('dir-list'),
  requestId: z.string().min(1),
  path: z.string().min(1),
  parent: z.string().optional(),
  entries: z.array(DirEntry),
})
export type DirListReply = z.infer<typeof DirListReply>

/**
 * Reply to a `list-commands` RPC: the slash-commands the backend exposes for the
 * run, each an optionally-described name. Like `file-list`, it is couriered as an
 * encrypted AgentEvent and matched back to its request by `requestId`.
 */
export const CommandListReply = z.object({
  kind: z.literal('command-list'),
  requestId: z.string().min(1),
  commands: z.array(z.object({ name: z.string().min(1), description: z.string().optional() })),
})
export type CommandListReply = z.infer<typeof CommandListReply>
