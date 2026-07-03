import { sveltekit } from '@sveltejs/kit/vite'
import { defineConfig } from 'vite'

// The relay base URL is injected at build/runtime; default to the local relay.
process.env.PUBLIC_PACTIFY_RELAY_URL ??= 'http://localhost:4310'

export default defineConfig({
  plugins: [sveltekit()],
  test: {
    include: ['src/**/*.{test,spec}.ts'],
    environment: 'node',
  },
})
