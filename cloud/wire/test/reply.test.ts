import { describe, it, expect } from 'vitest'
import {
  FileListReply,
  ControlsPayload,
  McpPayload,
  DiscoveredReply,
  DirListReply,
  CommandListReply,
} from '../src/reply'
import { AgentEvent } from '../src/events'

describe('FileListReply', () => {
  it('parses a file-list reply', () => {
    const r = FileListReply.parse({
      kind: 'file-list',
      requestId: 'req1',
      files: ['src/a.ts', 'src/b.ts'],
    })
    expect(r.files).toHaveLength(2)
  })

  it('flows through the AgentEvent union', () => {
    const e = AgentEvent.parse({ kind: 'file-list', requestId: 'req1', files: [] })
    if (e.kind === 'file-list') expect(e.requestId).toBe('req1')
    else throw new Error('expected file-list')
  })

  it('rejects a file-list missing requestId', () => {
    expect(() => FileListReply.parse({ kind: 'file-list', files: [] })).toThrow()
  })
})

describe('ControlsPayload', () => {
  it('parses controls with optional current selections', () => {
    const c = ControlsPayload.parse({
      kind: 'controls',
      models: ['opus', 'sonnet'],
      modes: ['default', 'plan'],
      currentModel: 'opus',
    })
    expect(c.currentModel).toBe('opus')
    expect(c.currentMode).toBeUndefined()
  })

  it('flows through the AgentEvent union', () => {
    const e = AgentEvent.parse({ kind: 'controls', models: [], modes: [] })
    expect(e.kind).toBe('controls')
  })
})

describe('McpPayload', () => {
  it('parses an mcp payload with server statuses', () => {
    const m = McpPayload.parse({
      kind: 'mcp',
      servers: [
        { name: 'github', status: 'configured' },
        { name: 'slack', status: 'disabled' },
      ],
    })
    expect(m.servers[0].status).toBe('configured')
  })

  it('rejects an unknown server status', () => {
    expect(() =>
      McpPayload.parse({ kind: 'mcp', servers: [{ name: 'x', status: 'on' }] }),
    ).toThrow()
  })

  it('flows through the AgentEvent union', () => {
    const e = AgentEvent.parse({ kind: 'mcp', servers: [] })
    expect(e.kind).toBe('mcp')
  })
})

describe('DiscoveredReply', () => {
  it('parses a discovered reply with sessions', () => {
    const d = DiscoveredReply.parse({
      kind: 'discovered',
      requestId: 'req1',
      sessions: [
        { id: 's1', agentKind: 'claude', title: 'fixing tests', workdir: '/repo' },
        { id: 's2', agentKind: 'claude' },
      ],
    })
    expect(d.sessions).toHaveLength(2)
    expect(d.sessions[0].title).toBe('fixing tests')
    expect(d.sessions[1].title).toBeUndefined()
  })

  it('parses a discovered reply with no sessions', () => {
    const d = DiscoveredReply.parse({ kind: 'discovered', requestId: 'req1', sessions: [] })
    expect(d.sessions).toHaveLength(0)
  })

  it('rejects a discovered reply missing requestId', () => {
    expect(() => DiscoveredReply.parse({ kind: 'discovered', sessions: [] })).toThrow()
  })

  it('rejects a session with an unknown agentKind', () => {
    expect(() =>
      DiscoveredReply.parse({
        kind: 'discovered',
        requestId: 'req1',
        sessions: [{ id: 's1', agentKind: 'cursor' }],
      }),
    ).toThrow()
  })

  it('flows through the AgentEvent union', () => {
    const e = AgentEvent.parse({ kind: 'discovered', requestId: 'req1', sessions: [] })
    if (e.kind === 'discovered') expect(e.requestId).toBe('req1')
    else throw new Error('expected discovered')
  })
})

describe('DirListReply', () => {
  it('parses a dir-list reply with entries + parent', () => {
    const d = DirListReply.parse({
      kind: 'dir-list',
      requestId: 'req1',
      path: '/repo/src',
      parent: '/repo',
      entries: [
        { name: 'lib', path: '/repo/src/lib' },
        { name: 'routes', path: '/repo/src/routes' },
      ],
    })
    expect(d.path).toBe('/repo/src')
    expect(d.parent).toBe('/repo')
    expect(d.entries).toHaveLength(2)
    expect(d.entries[0]).toEqual({ name: 'lib', path: '/repo/src/lib' })
  })

  it('parses a dir-list reply at the filesystem root (no parent)', () => {
    const d = DirListReply.parse({
      kind: 'dir-list',
      requestId: 'req1',
      path: '/',
      entries: [{ name: 'etc', path: '/etc' }],
    })
    expect(d.parent).toBeUndefined()
  })

  it('parses a dir-list reply with no entries (empty / unreadable dir)', () => {
    const d = DirListReply.parse({
      kind: 'dir-list',
      requestId: 'req1',
      path: '/repo',
      entries: [],
    })
    expect(d.entries).toHaveLength(0)
  })

  it('rejects a dir-list missing requestId', () => {
    expect(() => DirListReply.parse({ kind: 'dir-list', path: '/repo', entries: [] })).toThrow()
  })

  it('flows through the AgentEvent union (round-trip)', () => {
    const input = {
      kind: 'dir-list',
      requestId: 'req1',
      path: '/repo',
      parent: '/',
      entries: [{ name: 'src', path: '/repo/src' }],
    }
    const e = AgentEvent.parse(JSON.parse(JSON.stringify(input)))
    if (e.kind === 'dir-list') expect(e).toEqual(input)
    else throw new Error('expected dir-list')
  })
})

describe('CommandListReply', () => {
  it('parses a command-list reply with optional descriptions', () => {
    const r = CommandListReply.parse({
      kind: 'command-list',
      requestId: 'req1',
      commands: [{ name: 'commit', description: 'make a commit' }, { name: 'test' }],
    })
    expect(r.commands).toHaveLength(2)
    expect(r.commands[0].description).toBe('make a commit')
    expect(r.commands[1].description).toBeUndefined()
  })

  it('rejects a command-list missing requestId', () => {
    expect(() => CommandListReply.parse({ kind: 'command-list', commands: [] })).toThrow()
  })

  it('rejects a command with an empty name', () => {
    expect(() =>
      CommandListReply.parse({
        kind: 'command-list',
        requestId: 'req1',
        commands: [{ name: '' }],
      }),
    ).toThrow()
  })

  it('flows through the AgentEvent union', () => {
    const e = AgentEvent.parse({ kind: 'command-list', requestId: 'req1', commands: [] })
    if (e.kind === 'command-list') expect(e.requestId).toBe('req1')
    else throw new Error('expected command-list')
  })
})
