import { describe, it, expect } from 'vitest'
import { MachineInfo } from '../src/machine'

describe('MachineInfo', () => {
  const valid = {
    machineId: 'm1',
    host: 'laptop.local',
    agentKinds: ['claude', 'opencode'],
    workdirs: ['/repo', '/other'],
    online: true,
    lastSeenAt: 1719000000000,
  }

  it('parses a valid machine and round-trips', () => {
    expect(MachineInfo.parse(valid)).toEqual(valid)
  })

  it('parses a minimal machine (host/workdirs omitted)', () => {
    const minimal = {
      machineId: 'm2',
      agentKinds: ['kimi'],
      online: false,
      lastSeenAt: 1719000000001,
    }
    expect(MachineInfo.parse(minimal)).toEqual(minimal)
  })

  it('rejects a missing required field (online)', () => {
    const { online, ...rest } = valid
    void online
    expect(MachineInfo.safeParse(rest).success).toBe(false)
  })

  it('rejects an unknown agentKind', () => {
    expect(MachineInfo.safeParse({ ...valid, agentKinds: ['cursor'] }).success).toBe(false)
  })

  // agy is a first-class headless kind; a machine with it installed must be able
  // to advertise it (before this, MachineInfo.parse threw on the whole machine).
  it('accepts a machine advertising antigravity (agy)', () => {
    expect(MachineInfo.safeParse({ ...valid, agentKinds: ['antigravity'] }).success).toBe(true)
  })

  it('rejects a non-boolean online', () => {
    expect(MachineInfo.safeParse({ ...valid, online: 'yes' }).success).toBe(false)
  })
})
