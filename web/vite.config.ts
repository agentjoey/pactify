/// <reference types="vitest/config" />
import { defineConfig } from "vitest/config";
import { fileURLToPath, URL } from "node:url";
import react from "@vitejs/plugin-react";
import tailwindcss from "@tailwindcss/vite";

export default defineConfig({
  plugins: [react(), tailwindcss()],
  build: {
    outDir: "../internal/serve/dist",
    emptyOutDir: true,
    rollupOptions: {
      output: {
        manualChunks(id) {
          // Vendor split: keep the heavy dependencies out of the main entry chunk
          // so the initial dashboard load stays under Vercel's 500 kB warning.
          if (
            id.includes("node_modules/react/") ||
            id.includes("node_modules/react-dom/")
          ) {
            return "react-vendor";
          }
          if (id.includes("node_modules/@xyflow/")) {
            return "xyflow";
          }
          if (
            id.includes("/cloud/crypto/") ||
            id.includes("/cloud/relay-client/") ||
            id.includes("/node_modules/@noble/") ||
            id.includes("/node_modules/@scure/") ||
            id.includes("/node_modules/socket.io-client/")
          ) {
            return "crypto-relay";
          }
        },
      },
    },
  },
  // Monorepo local linking (path B): the dashboard bundles shared cloud/ TS
  // packages straight from source — no npm publish, no build-dist round-trip.
  // Keeps cloud/ (its own pnpm workspace + fly-deployed relay) untouched; only
  // Vite/tsc resolve these aliases. A full pnpm-workspace unification would also
  // move the relay Docker to a repo-root build context (deploy-touching) — left
  // as a follow-up. Mirror every alias in tsconfig.app.json `paths`.
  resolve: {
    alias: {
      "@pactify-apps/pact-project": fileURLToPath(
        new URL("../cloud/pact-project/src/index.ts", import.meta.url),
      ),
      "@pactify-apps/crypto": fileURLToPath(
        new URL("../cloud/crypto/src/index.ts", import.meta.url),
      ),
      "@pactify-apps/wire": fileURLToPath(
        new URL("../cloud/wire/src/index.ts", import.meta.url),
      ),
      "@pactify-apps/relay-client": fileURLToPath(
        new URL("../cloud/relay-client/src/index.ts", import.meta.url),
      ),
    },
  },
  // Dev server proxies the API + SSE stream (both live under /api) to the running
  // `pactify serve` (launchd default :17082, override with PACTIFY_SERVE_URL), so
  // `vite` hot-reload hits the real backend WITHOUT the
  // build → go:embed dist → rebuild binary → restart launchd round-trip. Only the
  // embedded production build (npm run build) needs that chain.
  server: {
    proxy: {
      "/api": {
        target: process.env.PACTIFY_SERVE_URL || "http://127.0.0.1:17082",
        changeOrigin: true,
      },
    },
  },
  test: {
    environment: "jsdom",
    globals: true,
    setupFiles: ["./src/setupTests.ts"],
    // e2e/ holds the Playwright acceptance suite, which has its own runner
    // (playwright.config.ts). Vitest's default glob would otherwise try to
    // execute those .spec.ts files and choke on Playwright's test API, so scope
    // the unit suite to src/ (default node_modules/dist excludes still apply).
    include: ["src/**/*.{test,spec}.{ts,tsx}"],
  },
});
