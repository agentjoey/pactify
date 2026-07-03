import { z } from 'zod'
import { AgentKind } from './rpc.js'

/**
 * Cleartext-operational machine metadata (C model). host/agentKinds/workdirs
 * are visible to the relay so the web can render the machine picker and
 * presence.
 */
export const MachineInfo = z.object({
  machineId: z.string(),
  host: z.string().optional(),
  agentKinds: z.array(AgentKind),
  workdirs: z.array(z.string()).optional(),
  online: z.boolean(),
  lastSeenAt: z.number(),
})
export type MachineInfo = z.infer<typeof MachineInfo>
