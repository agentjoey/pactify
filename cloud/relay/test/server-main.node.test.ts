import { describe, it, expect } from 'vitest'
import { createPgliteDb } from '../src/db.js'
import { startServer } from '../src/server-main.js'

describe('startServer', () => {
  it('listens on the configured port (0) with an injected PGlite db, then closes', async () => {
    // Inject PGlite so the bootstrap never touches Neon. port 0 → OS-assigned.
    const db = await createPgliteDb()
    const server = await startServer({ databaseUrl: 'unused', secret: 's', port: 0 }, db)
    expect(server.url).toMatch(/^http:\/\/0\.0\.0\.0:\d+$/)
    expect(new URL(server.url).port).not.toBe('0')
    await server.close()
  })
})
