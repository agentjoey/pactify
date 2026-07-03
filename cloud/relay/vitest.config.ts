import { defineConfig } from 'vitest/config'

// Default unit/integration tests. Tests that open a real `pg` Pool or a
// socket.io/engine.io server need Node's native resolve conditions and run
// separately via vitest.node.config.ts, so exclude *.node.test.ts here.
export default defineConfig({
  test: {
    environment: 'node',
    exclude: ['**/node_modules/**', '**/dist/**', 'test/**/*.node.test.ts'],
  },
})
