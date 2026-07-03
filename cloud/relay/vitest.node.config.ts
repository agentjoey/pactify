import { defineConfig } from 'vitest/config'

// Standalone Vitest config for Node integration tests: the prod Postgres
// factory opens a `pg` Pool and the bootstrap listens on a real socket.io /
// engine.io / ws server in-process. Node resolve conditions so those stacks
// load via Node's native require — exactly as in production.
export default defineConfig({
  resolve: {
    conditions: ['node', 'import', 'module', 'default'],
  },
  test: {
    name: 'node',
    environment: 'node',
    include: ['test/**/*.node.{test,spec}.{js,ts}'],
  },
})
